#!/usr/bin/env bash
#
# deploy-acm-mce.sh - Deploy Red Hat ACM or MCE on an OpenShift cluster
#
# Applies OLM manifests (Namespace, OperatorGroup, Subscription), waits for
# the operator CSV to succeed, then creates the product CR.
#
# Usage:
#   ./hack/deploy-acm-mce.sh --acm   # deploy Advanced Cluster Management
#   ./hack/deploy-acm-mce.sh --mce   # deploy MultiCluster Engine
#
# Environment variables:
#   ACM_CHANNEL - override ACM subscription channel (default: auto-detect)
#   MCE_CHANNEL - override MCE subscription channel (default: auto-detect)
#   OCP_RELEASE_IMAGE - override ClusterImageSet release image (default: auto-detect from cluster)
#   STORAGE_CLASS     - override StorageClass for AgentServiceConfig PVCs (default: cluster default)
#   CSV_TIMEOUT       - seconds to wait for CSV to reach Succeeded (default: 300)
#   DEPLOY_TIMEOUT    - seconds to wait for product CR to reach ready state (default: 1200)
#
# Exit codes:
#   0 - Deployment completed successfully
#   1 - Error (missing prereqs, timeout, bad usage)

set -euo pipefail

# Colors for output (disabled if not a terminal)
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

CSV_TIMEOUT="${CSV_TIMEOUT:-300}"
DEPLOY_TIMEOUT="${DEPLOY_TIMEOUT:-1200}"
STORAGE_CLASS="${STORAGE_CLASS:-}"

die() { echo -e "${RED}ERROR: $*${NC}" >&2; exit 1; }
info() { echo -e "${GREEN}$*${NC}"; }
warn() { echo -e "${YELLOW}$*${NC}"; }

apply_manifest() {
    if ! oc apply -f - <<< "$1"; then
        die "Failed to apply manifest"
    fi
}

get_default_channel() {
    local package_name="$1"
    local channel
    channel=$(oc get packagemanifest "${package_name}" -o jsonpath='{.status.defaultChannel}' 2>/dev/null || echo "")
    if [[ -z "${channel}" ]]; then
        die "PackageManifest '${package_name}' not found. Is the redhat-operators CatalogSource available?"
    fi
    echo "${channel}"
}

wait_for_csv() {
    local namespace="$1"
    local subscription_name="$2"
    local elapsed=0
    local interval=10

    info "Waiting for subscription '${subscription_name}' CSV in namespace '${namespace}' (timeout: ${CSV_TIMEOUT}s)..."

    while [[ ${elapsed} -lt ${CSV_TIMEOUT} ]]; do
        local csv_name
        csv_name=$(oc get subscription -n "${namespace}" "${subscription_name}" -o jsonpath='{.status.currentCSV}' 2>/dev/null || true)

        if [[ -n "${csv_name}" ]]; then
            local phase
            phase=$(oc get csv -n "${namespace}" "${csv_name}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
            if [[ "${phase}" == "Succeeded" ]]; then
                info "CSV '${csv_name}' reached Succeeded phase"
                return 0
            fi
            if [[ "${phase}" == "Failed" ]]; then
                local reason
                reason=$(oc get csv -n "${namespace}" "${csv_name}" -o jsonpath='{.status.message}' 2>/dev/null || echo "unknown")
                die "CSV '${csv_name}' failed: ${reason}"
            fi
            warn "  CSV '${csv_name}' phase: ${phase:-Unknown} (${elapsed}s elapsed)"
        else
            warn "  No CSV resolved yet for subscription '${subscription_name}' (${elapsed}s elapsed)"
        fi

        sleep "${interval}"
        elapsed=$((elapsed + interval))
    done

    die "Timed out after ${CSV_TIMEOUT}s waiting for subscription '${subscription_name}' CSV to reach Succeeded"
}

wait_for_resource() {
    local resource_type="$1"
    local resource_name="$2"
    local ns_flag="$3"
    local jsonpath="$4"
    local expected="$5"
    local timeout="$6"
    local elapsed=0
    local interval=30

    info "Waiting for ${resource_type}/${resource_name} to reach '${expected}' (timeout: ${timeout}s)..."

    while [[ ${elapsed} -lt ${timeout} ]]; do
        local current
        # shellcheck disable=SC2086
        current=$(oc get "${resource_type}" "${resource_name}" ${ns_flag} -o jsonpath="${jsonpath}" 2>/dev/null || echo "")

        if [[ "${current}" == "${expected}" ]]; then
            info "${resource_type}/${resource_name} reached '${expected}'"
            return 0
        fi

        warn "  ${resource_type}/${resource_name}: ${current:-<not set>} (${elapsed}s elapsed)"
        sleep "${interval}"
        elapsed=$((elapsed + interval))
    done

    die "Timed out after ${timeout}s waiting for ${resource_type}/${resource_name} to reach '${expected}'"
}

build_pvc_spec() {
    local size="$1"
    local spec="    accessModes:
      - ReadWriteOnce"
    if [[ -n "${STORAGE_CLASS}" ]]; then
        spec="${spec}
    storageClassName: ${STORAGE_CLASS}"
    fi
    spec="${spec}
    resources:
      requests:
        storage: ${size}"
    echo "${spec}"
}

validate_storage_class() {
    if [[ -n "${STORAGE_CLASS}" ]]; then
        if ! oc get storageclass "${STORAGE_CLASS}" &>/dev/null; then
            die "StorageClass '${STORAGE_CLASS}' not found. Available: $(oc get storageclass -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)"
        fi
        info "Using StorageClass: ${STORAGE_CLASS}"
    else
        local default_sc
        default_sc=$(oc get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}' 2>/dev/null)
        if [[ -z "${default_sc}" ]]; then
            die "No default StorageClass found. Set STORAGE_CLASS to specify one. Available: $(oc get storageclass -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)"
        fi
        info "Using default StorageClass: ${default_sc}"
    fi
}

detect_rhcos_iso() {
    info "Detecting RHCOS ISO from coreos-bootimages ConfigMap..."

    # Determine cluster architecture (node label uses "amd64", RHCOS uses "x86_64")
    local node_arch
    node_arch=$(oc get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || echo "")
    [[ -z "${node_arch}" ]] && die "Could not determine cluster node architecture"

    case "${node_arch}" in
        amd64)   CLUSTER_ARCH="x86_64" ;;
        arm64)   CLUSTER_ARCH="aarch64" ;;
        s390x)   CLUSTER_ARCH="s390x" ;;
        ppc64le) CLUSTER_ARCH="ppc64le" ;;
        *)       die "Unsupported architecture: ${node_arch}" ;;
    esac

    local stream_json
    stream_json=$(oc get configmap coreos-bootimages -n openshift-machine-config-operator -o jsonpath='{.data.stream}' 2>/dev/null || echo "")
    [[ -z "${stream_json}" ]] && die "ConfigMap 'coreos-bootimages' not found in openshift-machine-config-operator namespace"

    local jq_arch_path=".architectures.\"${CLUSTER_ARCH}\".artifacts.metal"
    RHCOS_VERSION=$(echo "${stream_json}" | jq -r "${jq_arch_path}.release")
    RHCOS_ISO_URL=$(echo "${stream_json}" | jq -r "${jq_arch_path}.formats.iso.disk.location")

    [[ -z "${RHCOS_VERSION}" || "${RHCOS_VERSION}" == "null" ]] && die "Could not parse RHCOS version for architecture '${CLUSTER_ARCH}' from coreos-bootimages"
    [[ -z "${RHCOS_ISO_URL}" || "${RHCOS_ISO_URL}" == "null" ]] && die "Could not parse RHCOS ISO URL for architecture '${CLUSTER_ARCH}' from coreos-bootimages"

    # Extract OCP version from ClusterVersion (used by setup_agent_service too)
    OCP_VERSION=$(oc get clusterversion version -o jsonpath='{.status.desired.version}' 2>/dev/null || echo "")
    [[ -z "${OCP_VERSION}" ]] && die "Could not detect OCP version from ClusterVersion"
    OCP_MINOR=$(echo "${OCP_VERSION}" | grep -oE '^[0-9]+\.[0-9]+')

    info "  Architecture: ${CLUSTER_ARCH}"
    info "  RHCOS version: ${RHCOS_VERSION}"
    info "  ISO URL: ${RHCOS_ISO_URL}"
    info "  OCP minor: ${OCP_MINOR}"
}

setup_agent_service() {
    validate_storage_class
    detect_rhcos_iso

    # OCP_VERSION is already set by detect_rhcos_iso; always use it for naming
    local release_image="${OCP_RELEASE_IMAGE:-quay.io/openshift-release-dev/ocp-release:${OCP_VERSION}-${CLUSTER_ARCH}}"

    info "Using release image: ${release_image}"

    local pvc_db pvc_fs pvc_img
    pvc_db=$(build_pvc_spec 10Gi)
    pvc_fs=$(build_pvc_spec 20Gi)
    pvc_img=$(build_pvc_spec 20Gi)

    info "Creating AgentServiceConfig (pinned to ${CLUSTER_ARCH} RHCOS ${RHCOS_VERSION})..."
    apply_manifest "
apiVersion: agent-install.openshift.io/v1beta1
kind: AgentServiceConfig
metadata:
  name: agent
spec:
  databaseStorage:
${pvc_db}
  filesystemStorage:
${pvc_fs}
  imageStorage:
${pvc_img}
  osImages:
    - openshiftVersion: '${OCP_MINOR}'
      version: '${RHCOS_VERSION}'
      url: '${RHCOS_ISO_URL}'
      cpuArchitecture: '${CLUSTER_ARCH}'
"

    local arch_suffix="${CLUSTER_ARCH//_/-}"
    info "Creating ClusterImageSet for OCP ${OCP_VERSION}..."
    apply_manifest "
apiVersion: hive.openshift.io/v1
kind: ClusterImageSet
metadata:
  name: img${OCP_VERSION}-${arch_suffix}
spec:
  releaseImage: ${release_image}
"
}

deploy_acm() {
    local namespace="open-cluster-management"
    local channel="${ACM_CHANNEL:-}"

    info "Deploying Advanced Cluster Management..."

    if [[ -z "${channel}" ]]; then
        info "Detecting default channel from PackageManifest..."
        channel=$(get_default_channel "advanced-cluster-management")
    fi
    info "Using channel: ${channel}"

    info "Creating namespace '${namespace}'..."
    apply_manifest "
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
"

    info "Creating OperatorGroup..."
    apply_manifest "
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: acm-operator-group
  namespace: ${namespace}
spec:
  targetNamespaces:
    - ${namespace}
"

    info "Creating Subscription..."
    apply_manifest "
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: advanced-cluster-management
  namespace: ${namespace}
spec:
  channel: ${channel}
  installPlanApproval: Automatic
  name: advanced-cluster-management
  source: redhat-operators
  sourceNamespace: openshift-marketplace
"

    wait_for_csv "${namespace}" "advanced-cluster-management"

    info "Creating MultiClusterHub CR..."
    apply_manifest "
apiVersion: operator.open-cluster-management.io/v1
kind: MultiClusterHub
metadata:
  name: multiclusterhub
  namespace: ${namespace}
spec: {}
"

    wait_for_resource "multiclusterhub" "multiclusterhub" "-n ${namespace}" '{.status.phase}' "Running" "${DEPLOY_TIMEOUT}"

    # ACM auto-creates an MCE — enable assisted-service on it
    local mce_name
    mce_name=$(oc get multiclusterengine -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [[ -n "${mce_name}" ]]; then
        info "Enabling assisted-service on ACM-managed MultiClusterEngine '${mce_name}'..."
        if ! oc patch multiclusterengine "${mce_name}" --type=merge -p '{"spec":{"overrides":{"components":[{"name":"assisted-service","enabled":true}]}}}'; then
            die "Failed to patch MultiClusterEngine '${mce_name}' to enable assisted-service"
        fi
        wait_for_resource "multiclusterengine" "${mce_name}" "" '{.status.phase}' "Available" "${DEPLOY_TIMEOUT}"
    else
        warn "No MultiClusterEngine found after ACM deploy — assisted-service must be enabled manually"
    fi

    setup_agent_service

    info ""
    info "========================================"
    info "  ACM deployed successfully"
    info "========================================"
}

deploy_mce() {
    local namespace="multicluster-engine"
    local channel="${MCE_CHANNEL:-}"

    info "Deploying MultiCluster Engine..."

    if [[ -z "${channel}" ]]; then
        info "Detecting default channel from PackageManifest..."
        channel=$(get_default_channel "multicluster-engine")
    fi
    info "Using channel: ${channel}"

    info "Creating namespace '${namespace}'..."
    apply_manifest "
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
"

    info "Creating OperatorGroup..."
    apply_manifest "
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: mce-operator-group
  namespace: ${namespace}
spec:
  targetNamespaces:
    - ${namespace}
"

    info "Creating Subscription..."
    apply_manifest "
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: multicluster-engine
  namespace: ${namespace}
spec:
  channel: ${channel}
  installPlanApproval: Automatic
  name: multicluster-engine
  source: redhat-operators
  sourceNamespace: openshift-marketplace
"

    wait_for_csv "${namespace}" "multicluster-engine"

    info "Creating MultiClusterEngine CR (with assisted-service enabled)..."
    apply_manifest "
apiVersion: multicluster.openshift.io/v1
kind: MultiClusterEngine
metadata:
  name: multiclusterengine
spec:
  overrides:
    components:
      - name: assisted-service
        enabled: true
"

    wait_for_resource "multiclusterengine" "multiclusterengine" "" '{.status.phase}' "Available" "${DEPLOY_TIMEOUT}"

    setup_agent_service

    info ""
    info "========================================"
    info "  MCE deployed successfully"
    info "========================================"
}

# --- Main ---

if ! command -v oc &>/dev/null; then
    die "'oc' CLI not found. Install it from https://console.redhat.com/openshift/downloads"
fi

if ! command -v jq &>/dev/null; then
    die "'jq' not found. Install it with 'dnf install jq' or equivalent."
fi

if ! oc cluster-info &>/dev/null; then
    die "Cannot reach OpenShift cluster. Log in with 'oc login' first."
fi

case "${1:-}" in
    --acm) deploy_acm ;;
    --mce) deploy_mce ;;
    *)
        echo "Usage: $0 --acm | --mce"
        echo ""
        echo "  --acm  Deploy Advanced Cluster Management"
        echo "  --mce  Deploy MultiCluster Engine"
        exit 1
        ;;
esac

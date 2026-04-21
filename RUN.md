# Running the ACM Cluster Service Provider

This document covers prerequisites, configuration, and operational details for deploying and running the ACM Cluster Service Provider.

For the full DCM stack deployment (recommended), see [dcm-project/api-gateway](https://github.com/dcm-project/api-gateway).

## Prerequisites

### ACM Hub cluster

The SP runs against an ACM Hub cluster and requires:

- **HyperShift operator** installed and functional
- **ClusterImageSet** resources available (used for version discovery)
- A **namespace** for managed clusters (configured via `SP_CLUSTER_NAMESPACE`)

### CRDs

The SP interacts with the following Custom Resource Definitions. All must be installed on the Hub cluster.

| CRD | API Group | Version | Required by |
|-----|-----------|---------|-------------|
| `HostedCluster` | `hypershift.openshift.io` | `v1beta1` | Core (all platforms) |
| `NodePool` | `hypershift.openshift.io` | `v1beta1` | Core (all platforms) |
| `ClusterImageSet` | `hive.openshift.io` | `v1` | Version discovery |

Platform-specific CRDs checked at health time:

| CRD | API Group | Version | Platform |
|-----|-----------|---------|----------|
| `VirtualMachineInstance` | `kubevirt.io` | `v1` | `kubevirt` |
| `Agent` | `agent-install.openshift.io` | `v1beta1` | `baremetal` |

### RBAC

The SP's service account needs permissions to:

| Resource | API Group | Verbs |
|----------|-----------|-------|
| `hostedclusters` | `hypershift.openshift.io` | `get`, `list`, `watch`, `create`, `delete` |
| `nodepools` | `hypershift.openshift.io` | `get`, `list`, `watch`, `create`, `delete` |
| `clusterimageset` | `hive.openshift.io` | `list` |
| `secrets` | `""` (core) | `get`, `create`, `update` |
| `virtualmachineinstances` | `kubevirt.io` | `list` (health check only, kubevirt platform) |
| `agents` | `agent-install.openshift.io` | `list` (health check only, baremetal platform) |

### External services

| Service | Purpose |
|---------|---------|
| [DCM Service Provider Manager](https://github.com/dcm-project/service-provider-manager) | Registration target. The SP registers itself and its capabilities on startup. |
| [NATS](https://nats.io/) | Message bus for publishing cluster status change events as CloudEvents. |

## Configuration

All configuration is read from environment variables at startup. The SP uses [caarlos0/env](https://github.com/caarlos0/env) for parsing.

### Server

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_SERVER_ADDRESS` | `string` | `:8080` | HTTP server bind address. |
| `SP_SERVER_SHUTDOWN_TIMEOUT` | `duration` | `15s` | Graceful shutdown timeout. |
| `SP_SERVER_REQUEST_TIMEOUT` | `duration` | `30s` | Per-request timeout. |
| `SP_SERVER_READ_TIMEOUT` | `duration` | `15s` | HTTP read timeout. |
| `SP_SERVER_WRITE_TIMEOUT` | `duration` | `15s` | HTTP write timeout. |
| `SP_SERVER_IDLE_TIMEOUT` | `duration` | `60s` | Idle connection timeout. |

### Registration

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `DCM_REGISTRATION_URL` | `string` | -- | **yes** | URL of the DCM Service Provider Manager (e.g., `http://spm:8080`). The SP calls `POST /providers` on this URL to register. |
| `SP_NAME` | `string` | `acm-cluster-sp` | no | Provider name used in registration and as NATS event source. |
| `SP_ENDPOINT` | `string` | -- | **yes** | The URL where the DCM platform can reach this SP (e.g., `http://acm-cluster-sp:8080`). Sent during registration so the platform knows how to call back. |
| `SP_DISPLAY_NAME` | `string` | `""` | no | Human-readable display name sent during registration. |
| `SP_REGION` | `string` | `""` | no | Region code included in registration metadata. |
| `SP_ZONE` | `string` | `""` | no | Zone identifier included in registration metadata. |
| `SP_REGISTRATION_INITIAL_BACKOFF` | `duration` | `1s` | no | Initial retry backoff for registration. Doubles on each retry. |
| `SP_REGISTRATION_MAX_BACKOFF` | `duration` | `5m` | no | Maximum retry backoff for registration. |
| `SP_VERSION_CHECK_INTERVAL` | `duration` | `5m` | no | How often to re-check `ClusterImageSet` resources and re-register if versions changed. |

### Cluster

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `SP_CLUSTER_NAMESPACE` | `string` | -- | **yes** | Kubernetes namespace where `HostedCluster` and `NodePool` resources are created. The pull secret is also created here. |
| `SP_PULL_SECRET` | `string` | -- | **yes** | Base64-encoded Docker/OCI pull secret content (`.dockerconfigjson` format). The SP decodes it at startup and creates a Kubernetes Secret named `{SP_NAME}-pull-secret` in the cluster namespace. Used by HyperShift to pull release images. |
| `SP_BASE_DOMAIN` | `string` | `""` | no | Default base DNS domain for clusters (e.g., `example.com`). Can be overridden per-cluster via `provider_hints.acm.base_domain`. |
| `SP_CONSOLE_URI_PATTERN` | `string` | `https://console-openshift-console.apps.{name}.{base_domain}` | no | Template for generating the OpenShift console URI. `{name}` and `{base_domain}` are substituted. |
| `SP_VERSION_MATRIX_PATH` | `string` | `""` | no | Path to a JSON file overriding the default OCP-to-K8s version compatibility matrix. If empty, uses the built-in matrix (4.14=1.27 through 4.18=1.31). |
| `SP_DEFAULT_INFRA_ENV` | `string` | `""` | no | Default `InfraEnv` name for baremetal clusters. If not set, every baremetal create request must include `provider_hints.acm.infra_env`. |
| `SP_AGENT_NAMESPACE` | `string` | `""` | no | Namespace where bare metal Agents are located. Set in the `HostedCluster` `AgentPlatformSpec`. |
| `SP_INFRA_ENV_LABEL_KEY` | `string` | `infraenvs.agent-install.openshift.io` | no | Label key used on `NodePool` agent label selectors to match agents to an `InfraEnv`. |

### Health

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_HEALTH_CHECK_TIMEOUT` | `duration` | `5s` | Timeout for each health probe (K8s API + CRD checks). |
| `SP_ENABLED_PLATFORMS` | `string` (comma-separated) | `kubevirt,baremetal` | Platforms to check during health probes. Only CRDs for enabled platforms are verified. |

### Monitoring

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `SP_NATS_URL` | `string` | -- | **yes** | NATS server URL (e.g., `nats://nats:4222`). Used to publish cluster status change events. The connection retries indefinitely on disconnect. |
| `SP_STATUS_DEBOUNCE_INTERVAL` | `duration` | `1s` | no | Debounce window for status change events. Rapid successive changes to the same cluster are coalesced. |
| `SP_STATUS_RESYNC_INTERVAL` | `duration` | `10m` | no | Full resync interval for the Kubernetes informer cache. |
| `SP_NATS_PUBLISH_RETRY_MAX` | `int` | `3` | no | Maximum publish retry attempts per event. |
| `SP_NATS_PUBLISH_RETRY_INTERVAL` | `duration` | `2s` | no | Initial retry interval between publish attempts. Doubles on each retry. |

### Kubernetes

The SP uses [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) to connect to the Kubernetes API. It respects standard kubeconfig resolution:

- In-cluster: automatic via service account token mount
- Out-of-cluster: `KUBECONFIG` env var or `~/.kube/config`


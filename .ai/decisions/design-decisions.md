# Design Decisions

This document records architectural and design decisions for the ACM Cluster Service Provider.

**Related Spec:** `.ai/specs/acm-cluster-sp.spec.md`

---

## DD-001: K8s Minor Version Format Enforced by SP

**Date:** 2026-02-13
**Status:** Accepted
**Spec References:** REQ-REG-080, REQ-REG-090, REQ-REG-091, REQ-ACM-030, REQ-ACM-031, REQ-ACM-032

### Problem

The SP needs to advertise available Kubernetes versions and validate version requests. The DCM API is platform-agnostic, but the SP uses OpenShift (OCP) under the hood, which has a different versioning scheme.

### Decision

The SP advertises available K8s minor versions in `kubernetesSupportedVersions` during registration. Two sources of truth govern version handling:

1. **Internal compatibility matrix** (OCP 4.x = K8s 1.(x+13)) — source of truth for OCP-to-K8s translation
2. **ACM ClusterImageSets** — source of truth for which OCP versions are available on the hub

The SP queries ClusterImageSets, translates OCP versions to K8s via the matrix, and advertises the resulting K8s versions. Callers MUST use exactly one of the advertised K8s minor versions — no OCP versions, no format variations (e.g., `"1.3"` is not `"1.30"`). Version validation at creation time translates the K8s version back to OCP via the matrix, then performs a live ClusterImageSet lookup.

When ClusterImageSets change, the SP re-registers to advertise the updated K8s version set.

### Rationale

The caller-facing API uses platform-agnostic K8s versions. The compatibility matrix is owned and maintained by the SP.

---

## DD-002: Health Status Values

**Date:** 2026-02-13
**Status:** Accepted
**Spec References:** REQ-HLT-050, REQ-HLT-060

### Problem

The SP health enhancement (`service-provider-health-check.md`) specifies `"pass"` as the healthy status value, but this is ambiguous and inconsistent with AEP conventions.

### Decision

The SP uses `"healthy"` / `"unhealthy"` instead of `"pass"` from the enhancement.

### Rationale

Aligns with AEP conventions and provides clearer semantics. The enhancement's `"pass"` value is not used.

---

## DD-003: Provider Name as Unique Identifier

**Date:** 2026-02-13
**Status:** Accepted
**Spec References:** REQ-MON-070, AC-MON-010

### Problem

The SP receives a provider ID from the DCM registry upon registration. The question is whether to persist and use this ID for CloudEvents subjects and correlation.

### Decision

`SP_NAME` is used for CloudEvents subjects and DCM correlation. The provider ID returned by the registry is not persisted by the SP. The administrator is responsible for ensuring name uniqueness across SP instances.

### Rationale

CloudEvents subjects use the provider name, not the registry-assigned ID. Persisting the ID adds complexity with no identified use case.

---

## DD-004: Control Plane Resources Mapped via Annotation Overrides

**Date:** 2026-03-24
**Status:** Accepted
**Spec References:** REQ-ACM-060, REQ-ACM-061, AC-ACM-060

### Problem

The DCM API exposes `nodes.control_plane.cpu` (integer) and `memory` (string, e.g. `"16GB"`) for callers to specify control plane resource sizing. HyperShift's `HostedClusterSpec` has **no struct field** for per-component resource requests. The SP needs a mechanism to apply these values to the hosted control plane.

### Options Considered

#### Option A: Per-component annotation overrides (CHOSEN)

Use HyperShift's `resource-request-override.hypershift.openshift.io/<deployment>.<container>` annotation prefix to set resource **requests** on targeted control plane components.

**Targeted components:**

- `kube-apiserver.kube-apiserver` — the API server, largest CPU/memory consumer
- `etcd.etcd` — the key-value store, second largest consumer

Each targeted component receives the **full** CPU and memory values specified by the caller.

**Pros:**

- Uses HyperShift's official, documented mechanism (`ResourceRequestOverrideAnnotationPrefix` constant in API package)
- Per-cluster granularity (no shared cluster-wide policy objects required)
- Simple implementation: set two annotations on the HostedCluster ObjectMeta
- Works identically for KubeVirt and BareMetal platforms (common behavior)

**Cons:**

- Total CP resource usage from overrides is `2x` the specified values (each of 2 components gets the full amount)
- Only covers 2 of ~8 CP components; others use HyperShift defaults
- Annotation key includes deployment/container names that could change between HyperShift versions

#### Option B: ClusterSizingConfiguration (t-shirt sizing)

Use the `ClusterSizingConfiguration` CRD (`scheduling/v1alpha1`) to define size-based resource policies.

**Rejected because:**

- Requires pre-existing cluster-wide policy objects on the management cluster
- Is an operator-level concern, not a per-cluster creation parameter
- Maps sizes based on node count ranges, which doesn't align with DCM's direct cpu/memory API
- Adds dependency on the scheduling API group

#### Option C: Treat control_plane.cpu/memory as informational

Accept the values but do not apply them, similar to `control_plane.count` (REQ-ACM-070) and `control_plane.storage` (REQ-ACM-080).

**Rejected because:**

- REQ-ACM-060 is a MUST requirement, not a SHOULD
- Unlike count (HA managed by HyperShift) and storage (etcd managed by HyperShift), CPU and memory sizing has direct impact on CP pod scheduling and is user-controllable via annotations

### Decision

**Option A** — Annotation-based resource request overrides targeting `kube-apiserver` and `etcd`.

### Resource Distribution

Each targeted component gets the **full** specified `cpu` and `memory` values. If a caller specifies `cpu=4, memory="16GB"`, the resulting annotations are:

```
resource-request-override.hypershift.openshift.io/kube-apiserver.kube-apiserver: cpu=4,memory=16G
resource-request-override.hypershift.openshift.io/etcd.etcd: cpu=4,memory=16G
```

**Implication:** Total CP resource requests for the two targeted components are `2 x cpu` and `2 x memory`. Non-targeted CP components (kube-controller-manager, kube-scheduler, oauth-openshift, etc.) use HyperShift's defaults.

### Format Conversion

- **CPU:** DCM `cpu` (integer cores) -> annotation value as integer (e.g., `4` -> `cpu=4`)
- **Memory:** DCM `"16GB"` -> K8s SI quantity by stripping trailing `"B"` (e.g., `"16GB"` -> `memory=16G`). Uses the existing `ParseDCMMemory` function which performs this conversion.

### Annotation Constant

The SP MUST use `hyperv1.ResourceRequestOverrideAnnotationPrefix` (`resource-request-override.hypershift.openshift.io`) from the HyperShift API package to construct annotation keys. This ensures the SP follows any prefix changes in future HyperShift releases.

### Scope

- **Applies to:** Both KubeVirt and BareMetal platforms (common behavior — the hosted control plane runs on the management cluster regardless of worker platform)
- **Implementation:** Shared code path (not duplicated per platform)
- **Zero-value behavior:** If `control_plane.cpu` is 0 or `control_plane.memory` is empty, no resource override annotations are added — HyperShift uses its own defaults

### Consequences

1. Operators should be aware that specifying CP resources results in `2x` the stated values across the two targeted components
2. Deployment/container names (`kube-apiserver`, `etcd`) are HyperShift implementation details — if they change across versions, the annotation keys must be updated
3. If HyperShift introduces a struct field for CP resources in the future, the SP should migrate from annotations to the struct field
4. Other CP components (kube-controller-manager, kube-scheduler, etc.) are not sized by this mechanism — they retain HyperShift defaults

---

## DD-005: Services — All 4 Control-Plane Services with Default Strategies

**Date:** 2026-04-01
**Updated:** 2026-04-02
**Status:** Accepted (revised — Option B chosen over original Option A)
**Spec References:** REQ-ACM-180, AC-ACM-180

### Problem

The HyperShift HostedCluster CRD marks `Spec.Services` (`[]ServicePublishingStrategyMapping`) as `+required` with no `omitempty` and no kubebuilder default. The SP was not setting this field, which is masked in tests because `controller-runtime/pkg/client/fake` does not enforce CRD validation.

### Options Considered

#### Option A: Set only APIServer with LoadBalancer

Set a single `ServicePublishingStrategyMapping` entry: `APIServer` with `LoadBalancer` strategy.

**Rejected because:**

- The HyperShift CRD expects all 4 control-plane services to be declared, even though the validation rules enforcing this are currently disabled (`-kubebuilder` prefix)
- Omitting OAuthServer, Konnectivity, and Ignition leaves the HostedCluster incomplete relative to ACM expectations
- If HyperShift re-enables the validation rules, the SP would break

#### Option B: Set all 4 services with default strategies (CHOSEN)

Set entries for all 4 control-plane services: `APIServer` with `LoadBalancer`, and `OAuthServer`, `Konnectivity`, `Ignition` with `Route`.

**Pros:**

- Matches what ACM/HyperShift expects for a fully-configured HostedCluster
- Adding 3 extra static entries costs nothing in complexity
- Eliminates the risk of breakage if HyperShift re-enables CRD validation rules requiring all 4 services

**Cons:**

- Slightly larger configuration surface (but all entries are static defaults with no user-facing knobs)

### Decision

The SP sets **4** `ServicePublishingStrategyMapping` entries on every HostedCluster:

| Service | Strategy |
|---------|----------|
| APIServer | LoadBalancer |
| OAuthServer | Route |
| Konnectivity | Route |
| Ignition | Route |

APIServer uses `LoadBalancer` because it is the primary external entry point and LoadBalancer is the most portable strategy. The remaining services use `Route` as the standard publishing mechanism for internal control-plane endpoints.

### Consequences

1. Clusters created by the SP will use LoadBalancer for the APIServer — operators must ensure their infrastructure supports LoadBalancer services
2. BareMetal/Agent deployments require LoadBalancer infrastructure (e.g., MetalLB) to be pre-configured on the management cluster
3. OAuthServer, Konnectivity, and Ignition are exposed via Route — an OpenShift ingress controller must be available on the management cluster

---

## DD-006: NodePool Management — InPlace Upgrade Type (v1 Opinionated)

**Date:** 2026-04-01
**Status:** Accepted
**Spec References:** REQ-ACM-200, AC-ACM-200

### Problem

The HyperShift NodePool CRD marks `Spec.Management.UpgradeType` as `+required`. The SP was not setting this field, which is masked in tests because `controller-runtime/pkg/client/fake` does not enforce CRD validation.

### Options Considered

#### Option A: InPlace for all platforms (CHOSEN)

Set `UpgradeType` to `InPlace` for both KubeVirt and BareMetal NodePools.

**Pros:**

- Simpler — avoids node churn of Replace upgrades
- Consistent across platforms
- Suitable for v1 where upgrade orchestration is not yet a concern

**Cons:**

- InPlace upgrades may cause brief workload disruption on the node being upgraded

#### Option B: Platform-differentiated (Replace for KubeVirt, InPlace for BareMetal)

**Rejected because:**

- Adds unnecessary per-platform logic for v1
- Replace for KubeVirt adds VM lifecycle complexity with no identified use case
- Per-platform differentiation is better deferred to a later version with proper upgrade strategy configuration

### Decision

For v1, the SP sets `Spec.Management.UpgradeType` to `InPlace` on every NodePool, for all platforms.

### Consequences

1. If per-platform upgrade strategies are needed in the future, this becomes a configuration-driven decision
2. Operators should be aware that InPlace upgrades will temporarily affect workloads running on the upgraded node

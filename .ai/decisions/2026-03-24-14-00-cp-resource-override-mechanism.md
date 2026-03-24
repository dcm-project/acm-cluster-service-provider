# Decision: Control Plane Resource Override Mechanism

**Date:** 2026-03-24
**Context:** REQ-ACM-060 — mapping `control_plane.cpu` and `memory` to HostedCluster resources
**Status:** Accepted
**Spec Reference:** DEC-004 in `.ai/specs/acm-cluster-sp.spec.md`

## Problem

The DCM API exposes `nodes.control_plane.cpu` (integer) and `nodes.control_plane.memory` (string, e.g. `"16GB"`) for callers to specify control plane resource sizing. HyperShift's `HostedClusterSpec` has **no struct field** for per-component resource requests. The SP needs a mechanism to apply these values to the hosted control plane.

## Options Considered

### Option A: Per-component annotation overrides (CHOSEN)

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

### Option B: ClusterSizingConfiguration (t-shirt sizing)

Use the `ClusterSizingConfiguration` CRD (`scheduling/v1alpha1`) to define size-based resource policies.

**Rejected because:**
- Requires pre-existing cluster-wide policy objects on the management cluster
- Is an operator-level concern, not a per-cluster creation parameter
- Maps sizes based on node count ranges, which doesn't align with DCM's direct cpu/memory API
- Adds dependency on the scheduling API group

### Option C: Treat control_plane.cpu/memory as informational (like count/storage)

Accept the values but do not apply them, similar to `control_plane.count` (REQ-ACM-070) and `control_plane.storage` (REQ-ACM-080).

**Rejected because:**
- REQ-ACM-060 is a MUST requirement, not a SHOULD
- Unlike count (HA managed by HyperShift) and storage (etcd managed by HyperShift), CPU and memory sizing has direct impact on CP pod scheduling and is user-controllable via annotations

## Decision

**Option A** — Annotation-based resource request overrides targeting `kube-apiserver` and `etcd`.

### Resource Distribution

Each targeted component gets the **full** specified `cpu` and `memory` values. The annotation is a per-component override — it sets the resource request for a single deployment/container.

If a caller specifies `cpu=4, memory="16GB"`, the resulting annotations are:

```
resource-request-override.hypershift.openshift.io/kube-apiserver.kube-apiserver: cpu=4,memory=16G
resource-request-override.hypershift.openshift.io/etcd.etcd: cpu=4,memory=16G
```

**Implication:** Total CP resource requests for the two targeted components are `2 x cpu` and `2 x memory`. Non-targeted CP components (kube-controller-manager, kube-scheduler, oauth-openshift, etc.) use HyperShift's defaults.

### Format Conversion

- **CPU:** DCM `cpu` (integer cores) → annotation value as integer (e.g., `4` → `cpu=4`)
- **Memory:** DCM `"16GB"` → K8s SI quantity by stripping trailing `"B"` (e.g., `"16GB"` → `memory=16G`). Uses the existing `ParseDCMMemory` function which performs this conversion.

### Annotation Constant

The SP MUST use `hyperv1.ResourceRequestOverrideAnnotationPrefix` (`resource-request-override.hypershift.openshift.io`) from the HyperShift API package to construct annotation keys. This ensures the SP follows any prefix changes in future HyperShift releases.

### Scope

- **Applies to:** Both KubeVirt and BareMetal platforms (common behavior — the hosted control plane runs on the management cluster regardless of worker platform)
- **Implementation:** Shared code path (not duplicated per platform)
- **Zero-value behavior:** If `control_plane.cpu` is 0 or `control_plane.memory` is empty, no resource override annotations are added — HyperShift uses its own defaults

## Consequences

1. Operators should be aware that specifying CP resources results in `2x` the stated values across the two targeted components
2. Deployment/container names (`kube-apiserver`, `etcd`) are HyperShift implementation details — if they change across versions, the annotation keys must be updated
3. If HyperShift introduces a struct field for CP resources in the future, the SP should migrate from annotations to the struct field
4. Other CP components (kube-controller-manager, kube-scheduler, etc.) are not sized by this mechanism — they retain HyperShift defaults

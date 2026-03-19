# Code Review: Topic 5a — KubeVirt Service + Common Behaviors

## Overview
- **Date:** 2026-03-19
- **Reviewer:** Claude Code
- **Scope:** All files introduced or modified in `feat/topic-5a-kubevirt` branch (commits d3d353a..f935ef6)
- **Verdict:** Needs changes

### Files Reviewed
- `internal/cluster/kubevirt/kubevirt.go` (shared helpers)
- `internal/cluster/kubevirt/create.go`
- `internal/cluster/kubevirt/get.go`
- `internal/cluster/kubevirt/list.go`
- `internal/cluster/kubevirt/delete.go`
- `internal/cluster/config.go`
- `internal/cluster/labels.go`
- `internal/cluster/convert.go`
- `internal/cluster/version.go`
- `internal/service/status/mapper.go`
- `internal/service/errors.go`
- `internal/service/interfaces.go`
- `internal/cluster/kubevirt/kubevirt_unit_test.go`
- `internal/cluster/kubevirt/helpers_test.go`
- `internal/service/status/mapper_unit_test.go`

### Test Results
All 48 specs pass. All pre-existing tests remain GREEN:
```
ok  github.com/dcm-project/acm-cluster-service-provider/internal/cluster/kubevirt
ok  github.com/dcm-project/acm-cluster-service-provider/internal/service/status
ok  github.com/dcm-project/acm-cluster-service-provider/internal/handler
ok  github.com/dcm-project/acm-cluster-service-provider/internal/health
ok  github.com/dcm-project/acm-cluster-service-provider/internal/registration
ok  github.com/dcm-project/acm-cluster-service-provider/internal/apiserver
```

---

## Behavior Correctness

- [x] Logic is correct for happy path
- [ ] Edge cases are handled
- [ ] Error conditions are handled appropriately

**Issues found:**

### MF-1: Version field set to release image URL instead of K8s minor version

- **File:** `internal/cluster/kubevirt/kubevirt.go:64`
- **Severity:** Must Fix
- **REQ:** REQ-API-165 (Cluster resource identity fields)

**Current code:**
```go
// kubevirt.go:52-65
func (s *Service) hostedClusterToCluster(ctx context.Context, hc *hyperv1.HostedCluster) v1alpha1.Cluster {
    instanceID := hc.Labels[cluster.LabelInstanceID]
    clusterStatus := status.MapConditionsToStatus(hc.Status.Conditions, hc.DeletionTimestamp)

    c := v1alpha1.Cluster{
        Id:          util.Ptr(instanceID),
        Path:        util.Ptr("clusters/" + instanceID),
        Status:      &clusterStatus,
        Metadata: v1alpha1.ClusterMetadata{
            Name: hc.Name,
        },
        ServiceType: v1alpha1.ClusterServiceTypeCluster,
        Version:     hc.Spec.Release.Image, // BUG: this is the full release image URL
    }
```

**What's wrong:** `hc.Spec.Release.Image` contains a value like `quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64`, but the API `version` field should contain the K8s minor version (e.g., `"1.30"`). The Create method correctly echoes back `req.Version` (K8s version), but Get/List return the wrong value via `hostedClusterToCluster`.

**Suggested fix:** Use `VersionResolver` or the compatibility matrix to reverse-map the release image back to a K8s minor version. Or extract the OCP minor version from the image tag and look it up in the matrix. A helper function like `releaseImageToK8sVersion(image string) string` should be added to `internal/cluster/version.go`, leveraging `extractOCPMinor` and the compatibility matrix reverse lookup.

```go
// In version.go, add:
func ReleaseImageToK8sVersion(image string, matrix registration.CompatibilityMatrix) string {
    ocpMinor := extractOCPMinor(image)
    for ocpVer, k8sVer := range matrix {
        if ocpVer == ocpMinor {
            return k8sVer
        }
    }
    return "" // fallback: unknown version
}

// In kubevirt.go, change line 64:
Version: cluster.ReleaseImageToK8sVersion(hc.Spec.Release.Image, registration.DefaultCompatibilityMatrix),
```

**Impact:** Consumers of the Get/List API receive a Docker image URL in the `version` field instead of a K8s version string like `"1.30"`. This is a contract violation — the version field is used for compatibility decisions and display.

---

### MF-2: UpdateTime not populated on Create or reads

- **File:** `internal/cluster/kubevirt/create.go:57-68` and `internal/cluster/kubevirt/kubevirt.go:52-80`
- **Severity:** Must Fix
- **REQ:** REQ-API-165 (Cluster resource — `update_time` field)

**Current code (create.go:56-68):**
```go
now := time.Now()
result := &v1alpha1.Cluster{
    Id:          util.Ptr(id),
    Path:        util.Ptr("clusters/" + id),
    Status:      util.Ptr(v1alpha1.ClusterStatusPENDING),
    CreateTime:  &now,
    // UpdateTime is missing here
    Metadata:    req.Metadata,
    Nodes:       req.Nodes,
    Version:     req.Version,
    ServiceType: req.ServiceType,
}
```

**Current code (kubevirt.go:56-79):**
```go
c := v1alpha1.Cluster{
    Id:          util.Ptr(instanceID),
    Path:        util.Ptr("clusters/" + instanceID),
    Status:      &clusterStatus,
    Metadata: v1alpha1.ClusterMetadata{
        Name: hc.Name,
    },
    ServiceType: v1alpha1.ClusterServiceTypeCluster,
    Version:     hc.Spec.Release.Image,
}

if !hc.CreationTimestamp.IsZero() {
    t := hc.CreationTimestamp.Time
    c.CreateTime = &t
}
// UpdateTime is never set
```

**What's wrong:** Per REQ-API-165, `update_time` should be populated. On Create, it should equal `create_time`. On reads (Get/List), it should reflect the last-modified timestamp. HyperShift HostedCluster has `metadata.managedFields` or we can use the last condition transition time as a reasonable proxy.

**Suggested fix:**

For Create (`create.go`):
```go
result := &v1alpha1.Cluster{
    ...
    CreateTime:  &now,
    UpdateTime:  &now, // Same as CreateTime on creation
    ...
}
```

For reads (`kubevirt.go`), use the most recent condition transition time as an approximation:
```go
// After setting CreateTime
if len(hc.Status.Conditions) > 0 {
    latest := hc.Status.Conditions[0].LastTransitionTime.Time
    for _, cond := range hc.Status.Conditions[1:] {
        if cond.LastTransitionTime.Time.After(latest) {
            latest = cond.LastTransitionTime.Time
        }
    }
    c.UpdateTime = &latest
}
```

**Impact:** API consumers have no way to know when a cluster was last modified. If the spec defines `update_time` as part of the resource, omitting it is a contract violation.

---

### MF-3: Invalid pageToken silently ignored instead of returning error

- **File:** `internal/cluster/kubevirt/list.go:36-43`
- **Severity:** Must Fix
- **REQ:** REQ-API-290 (Pagination — invalid token handling)

**Current code:**
```go
// list.go:36-43
if pageToken != "" {
    decoded, err := base64.StdEncoding.DecodeString(pageToken)
    if err == nil {
        if v, err := strconv.Atoi(string(decoded)); err == nil && v > 0 {
            offset = v
        }
    }
}
```

**What's wrong:** When `pageToken` is not valid base64 or does not decode to a positive integer, the code silently falls back to offset=0 and returns the first page. Per REQ-API-290, an invalid page token should return an InvalidArgument error. This masks client bugs and can cause confusion (client thinks they are on page N but receives page 1).

**Suggested fix:**
```go
if pageToken != "" {
    decoded, err := base64.StdEncoding.DecodeString(pageToken)
    if err != nil {
        return nil, service.NewInvalidArgumentError("invalid page_token")
    }
    v, err := strconv.Atoi(string(decoded))
    if err != nil || v < 0 {
        return nil, service.NewInvalidArgumentError("invalid page_token")
    }
    offset = v
}
```

**Impact:** Clients with corrupted or tampered page tokens silently get page 1 instead of an error, leading to data duplication or skipping in paginated results.

---

## Security

- [x] No injection vulnerabilities (SQL, command, XSS, etc.)
- [x] Auth/authz is enforced where needed (N/A for this layer)
- [x] No secrets or sensitive data in code

**No security issues found.** Kubeconfig data is base64-encoded (as expected by the API contract) and not logged.

---

## Complexity & Maintainability

- [x] Functions/methods are focused and not too long
- [x] Abstractions match the problem complexity
- [x] Naming is clear and consistent

**Issues found:**

### SF-1: Mutualization opportunity — extract shared helpers before Topic 5b

- **Files:** `internal/cluster/kubevirt/kubevirt.go`, `internal/cluster/kubevirt/create.go`
- **Severity:** Should Fix
- **REQ:** N/A (architecture quality)

**Current code:** The following helpers are in the `kubevirt` package but contain no KubeVirt-specific logic:

1. `kubevirt.go:35-49` — `findByInstanceID`: Looks up HC by `dcm-instance-id` label. BareMetal will need the same logic.
2. `kubevirt.go:52-80` — `hostedClusterToCluster`: Converts HC to v1alpha1.Cluster. Platform-independent except for `Version` field mapping.
3. `kubevirt.go:84-104` — `extractKubeconfig`: Reads kubeconfig from Secret. Fully platform-independent.
4. `kubevirt.go:107-115` — `buildConsoleURI`: Template substitution on config pattern. Platform-independent.
5. `kubevirt.go:118-127` — `buildAPIEndpoint`: Builds endpoint URL from HC status. Platform-independent.
6. `create.go:71-76` — `resolveBaseDomain`: Checks provider hints then config. Platform-independent.
7. `create.go:78-84` — `resolveReleaseImage`: Resolves version via CIS. Platform-independent.
8. `create.go:86-98` — `checkDuplicateID`: Checks label uniqueness. Platform-independent.

**What's wrong:** When Topic 5b (BareMetal) is implemented, all of these helpers will need to be duplicated or extracted. Extracting them now prevents duplication.

**Suggested fix:** Move these to `internal/cluster/shared.go` (or a `internal/cluster/common/` subpackage). Both KubeVirt and BareMetal services can embed or delegate to a shared base. The platform-specific parts are only:
- `buildHostedCluster` (platform spec differs)
- `buildNodePool` (platform spec differs)
- Create response construction (minor differences)

One approach: create a `cluster.Base` struct with the shared methods, and embed it in both `kubevirt.Service` and `baremetal.Service`.

**Impact:** Without this, Topic 5b will either duplicate ~80 lines of logic or require the same refactor under time pressure.

---

### SF-2: AlreadyExists error detail is not descriptive for name conflicts

- **File:** `internal/cluster/kubevirt/create.go:38-40`
- **Severity:** Should Fix

**Current code:**
```go
if k8serrors.IsAlreadyExists(err) {
    return nil, service.NewAlreadyExistsError("cluster already exists").WithDetail(req.Metadata.Name)
}
```

**What's wrong:** There are two AlreadyExists paths: duplicate `dcm-instance-id` (line 31) and K8s name collision (line 38-40). The K8s name collision message says "cluster already exists" with detail = the name, but doesn't clarify that it's a name conflict (not an ID conflict). Meanwhile the ID check (line 95) says "cluster with this ID already exists". A user receiving both would struggle to distinguish them.

**Suggested fix:**
```go
return nil, service.NewAlreadyExistsError(
    fmt.Sprintf("cluster with name %q already exists", req.Metadata.Name),
)
```

**Impact:** Debugging and UX — users and operators cannot easily tell whether they hit an ID conflict or a name conflict.

---

## Test Coverage

- [x] Key behaviors have tests
- [ ] Edge cases are tested
- [x] Tests are readable and maintainable

**Issues found:**

### C-1: No test asserts on Version field correctness in Get/List

- **File:** `internal/cluster/kubevirt/kubevirt_unit_test.go`
- **Severity:** Consider (related to MF-1)
- **TC:** TC-KV-UT-010 (Get — READY cluster returns full credentials)

**What's wrong:** TC-KV-UT-010 asserts on `ApiEndpoint`, `ConsoleUri`, `Kubeconfig`, `Status`, `Id`, `Path`, `ServiceType`, `CreateTime` — but NOT `Version`. This is why the release-image-as-version bug (MF-1) was not caught. When fixing MF-1, add a `Version` assertion to TC-KV-UT-010 and at least one List test.

---

### C-2: StatusMessage not populated for FAILED/UNAVAILABLE

- **File:** `internal/cluster/kubevirt/kubevirt.go:52-80`
- **Severity:** Consider
- **REQ:** REQ-ACM-160 mentions `status_message` as a SHOULD

**What's wrong:** When status is FAILED (Degraded=True) or UNAVAILABLE, the `StatusMessage` field in the response is always nil. The Degraded condition's `Message` field could be forwarded, giving operators actionable information.

**Suggested fix (if implementing):**
```go
// After computing clusterStatus
if clusterStatus == v1alpha1.ClusterStatusFAILED || clusterStatus == v1alpha1.ClusterStatusUNAVAILABLE {
    for _, cond := range hc.Status.Conditions {
        if cond.Type == "Degraded" && cond.Status == metav1.ConditionTrue && cond.Message != "" {
            c.StatusMessage = &cond.Message
            break
        }
    }
}
```

**Impact:** Low — this is a SHOULD requirement. But it's zero-cost to implement and provides operational value.

---

### C-3: VersionResolver created per-call in Create

- **File:** `internal/cluster/kubevirt/create.go:82`
- **Severity:** Consider

**Current code:**
```go
func (s *Service) resolveReleaseImage(ctx context.Context, req v1alpha1.Cluster) (string, error) {
    // ...
    resolver := cluster.NewVersionResolver(s.client)
    return resolver.Resolve(ctx, req.Version)
}
```

**What's wrong:** `NewVersionResolver` is called on every Create. The resolver holds only a `client.Client` and the matrix (a static map), so it could be created once in `New()` and stored on the `Service` struct.

**Suggested fix:** Move `VersionResolver` instantiation to `New()`:
```go
type Service struct {
    client   client.Client
    config   cluster.Config
    resolver *cluster.VersionResolver
}

func New(c client.Client, cfg cluster.Config) *Service {
    return &Service{
        client:   c,
        config:   cfg,
        resolver: cluster.NewVersionResolver(c),
    }
}
```

**Impact:** Negligible performance difference in practice. This is a style/hygiene item only.

---

## Pre-Existing Issues (Out of Topic 5a Scope)

### PRE-1: clientIDRe regex more restrictive than spec

- **File:** `internal/handler/validation.go:12`
- **Severity:** Must Fix (but belongs to Topic 4 scope)
- **REQ:** REQ-API-210

**Current code:**
```go
clientIDRe = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
```

**What's wrong:** The first character class is `[a-z]` (letters only), but REQ-API-210 specifies `^[a-z0-9]...` (letters or digits). IDs starting with a digit (e.g., `1my-cluster`) are rejected when they should be accepted.

**Suggested fix:**
```go
clientIDRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
```

**Impact:** Users cannot use IDs starting with digits. Should be addressed separately from Topic 5a.

---

## Recommendations

### Must Fix
1. **MF-1** — `kubevirt.go:64`: Version field returns release image URL instead of K8s minor version
2. **MF-2** — `create.go:57-68` + `kubevirt.go:52-80`: UpdateTime not populated
3. **MF-3** — `list.go:36-43`: Invalid pageToken silently ignored

### Should Fix
4. **SF-1** — Extract shared helpers to `internal/cluster/` before Topic 5b to avoid duplication
5. **SF-2** — `create.go:39`: AlreadyExists error message doesn't distinguish name vs ID conflicts

### Consider
6. **C-1** — Add Version assertion to TC-KV-UT-010 and List tests
7. **C-2** — Populate StatusMessage for FAILED/UNAVAILABLE from condition messages
8. **C-3** — Create VersionResolver once in `New()` instead of per-call

### Pre-Existing (separate fix)
9. **PRE-1** — `handler/validation.go:12`: clientIDRe rejects IDs starting with digits

---

## Next Steps

To address the findings above:
1. Run `/plan-impl` referencing this review file: `.ai/reviews/2026-03-19-12-08-topic-5a-kubevirt.md`
2. The plan should cover all "Must Fix" (MF-1, MF-2, MF-3) and "Should Fix" (SF-1, SF-2) items
3. "Consider" items (C-1, C-2, C-3) should be included if they don't add significant scope
4. PRE-1 should be tracked separately (it belongs to Topic 4 handler scope)
5. Use `/implement-plan` to execute the plan

### Mutualization Checklist for Topic 5b
When planning Topic 5b (BareMetal), the following from Topic 5a should be reused without duplication:
- `status.MapConditionsToStatus` — already platform-independent
- `cluster.DCMLabels`, `cluster.ParseDCMMemory`, `cluster.VersionResolver` — already in shared package
- `findByInstanceID`, `hostedClusterToCluster`, `extractKubeconfig`, `buildConsoleURI`, `buildAPIEndpoint` — extract from kubevirt package per SF-1
- `resolveBaseDomain`, `resolveReleaseImage`, `checkDuplicateID` — extract from kubevirt/create.go per SF-1
- Test helpers: `buildFakeClient`, `buildClusterImageSet`, `defaultClusterImageSets`, `newTestServiceWithInterceptor` — move to `internal/cluster/testutil/` or keep in a shared `helpers_test.go`

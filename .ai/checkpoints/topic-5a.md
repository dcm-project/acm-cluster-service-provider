# Checkpoint: Topic 5a — KubeVirt Service + Common Behaviors

## Status: GREEN PHASE COMPLETE — REVIEW PENDING FIXES
**Date:** 2026-03-19
**Branch:** `feat/topic-5a-kubevirt`

## Summary

GREEN phase for Topic 5a is complete. All 48 test cases pass against the implementation.

### Review (2026-03-19)
Review completed: `.ai/reviews/2026-03-19-12-08-topic-5a-kubevirt.md`

**3 Must Fix issues identified:**
1. **MF-1** — `kubevirt.go:64`: Version field returns release image URL instead of K8s minor version
2. **MF-2** — `create.go:57-68` + `kubevirt.go:52-80`: UpdateTime not populated
3. **MF-3** — `list.go:36-43`: Invalid pageToken silently ignored (should return error)

**2 Should Fix issues:**
4. **SF-1** — Extract shared helpers to `internal/cluster/` before Topic 5b (mutualization)
5. **SF-2** — AlreadyExists error doesn't distinguish name vs ID conflicts

**3 Consider items and 1 pre-existing issue (Topic 4 scope) also documented.**

Next: Run `/plan-impl` referencing the review file, then `/implement-plan`.

### Test Results

| Suite | Total Specs | Pass | Fail |
|-------|------------|------|------|
| Status Mapper (TC-STS-UT-001..012) | 12 | 12 | 0 |
| KubeVirt Service (TC-KV-UT-001..029 + TC-XC-ID-UT-001..002) | 36 | 36 | 0 |
| **Total** | **48** | **48** | **0** |

### Existing Tests
All existing tests (handler, health, registration, apiserver) remain GREEN.

## Implementation

### New Files (GREEN phase)
- `internal/cluster/labels.go` — DCM label constants and DCMLabels() helper
- `internal/cluster/convert.go` — ParseDCMMemory() for DCM→K8s format conversion
- `internal/cluster/version.go` — VersionResolver: K8s version → release image via CIS lookup
- `internal/cluster/kubevirt/create.go` — Service.Create() with HC+NP construction, validation, rollback
- `internal/cluster/kubevirt/get.go` — Service.Get() with label lookup and credential extraction
- `internal/cluster/kubevirt/list.go` — Service.List() with sorting and pagination
- `internal/cluster/kubevirt/delete.go` — Service.Delete() with label lookup

### Modified Files (GREEN phase)
- `internal/service/status/mapper.go` — Implemented condition precedence logic
- `internal/cluster/kubevirt/kubevirt.go` — Shared helpers (findByInstanceID, hostedClusterToCluster, extractKubeconfig, buildConsoleURI, buildAPIEndpoint); removed CRUD stubs
- `internal/cluster/kubevirt/helpers_test.go` — Added CIS seeding, REST mapper, interceptor support
- `internal/cluster/kubevirt/kubevirt_unit_test.go` — Updated setup for interceptor tests (016, 017, 023, 028)

## Commits (RED phase)

| Hash | Message |
|------|---------|
| 30487b1 | docs(test-plans): update CRD type strategy to typed HyperShift imports |
| aca5940 | feat(api): add UNAVAILABLE to ClusterStatus enum and HyperShift API dep |
| b6f424f | test(status): add RED phase status mapper tests (TC-STS-UT-001..012) |
| 6b93dc5 | feat(cluster): add cluster config and KubeVirt service stub |
| dd0e0bc | test(kubevirt): add test suite bootstrap and fixture helpers |
| 7921c39 | test(kubevirt): add RED phase KubeVirt service tests (TC-KV-UT + TC-XC-ID) |

## Key Decisions Made (GREEN phase)
- **Unstructured ClusterImageSet** — same approach as registration/version.go, with REST mapper for fake client
- **Imported compatibility matrix** from registration package (no duplication)
- **Split kubevirt into per-operation files** — create.go, get.go, list.go, delete.go (aligns with reference project)
- **Shared helpers** in kubevirt.go — findByInstanceID, hostedClusterToCluster (used by Get and List)
- **Credentials for READY + UNAVAILABLE** — both statuses populate api_endpoint, console_uri, kubeconfig
- **ParseDCMMemory errors ignored** — OpenAPI middleware validates format at boundary (caller-trust)

## Notes for Topic 5b
- Status mapper is shared — BareMetal tests should NOT duplicate TC-STS-UT-xxx
- Same cluster.Config struct — BareMetal may add SP_BAREMETAL_* env vars
- Shared utilities reusable: labels.go, convert.go, version.go
- Same test helper patterns — buildFakeClient, buildClusterImageSet, newTestServiceWithInterceptor
- BareMetal uses `hyperv1.AgentPlatform` and `AgentNodePoolPlatform`

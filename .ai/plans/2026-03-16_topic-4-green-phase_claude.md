# Plan: Topic 4 GREEN Phase — API Handler Implementation

## Original Prompt

> You are tasked to plan for implement the GREEN phase of the BDD for the topic #4 specified in @.ai/specs/
> The tests plans covering the requirements are in @.ai/test-plans/
> THe checkpoint file is in @.ai/checkpoints
> ** YOU MUST NEVER UPDATE THE TEST PLANS, THEY ARE LOCKED **
> Implement the behaviours to make the tests become GREEN; as per BDD
> If you have any questions, ask me, I will reply
> Make sure to follow the ~/.claude/CLAUDE.md guidelines
> You must use subagents to assist you in your task: implemeting, validating all tests implemented, review both code and test, critical assessments
> Only focus on topic #4
> Make sure to update the checkpoints file for the topic once your are done. I want only 1 file per topic
> Commit the changes once you are done.
> Use agent to self reflect on the proposed plan before submitting to me. Iterate until the agents assessing and reviewing your (updated) plans no longer have questions nor find issue with the plan you propose.
> Split the work into common concern (service, store, ...)
> Make sure to split the files and to introduce the nececessaries folder and subdirectories to keep the project well organised. Make sure it's compliant with go idioms
> Make sure that what we do is aligned with how it is done by https://github.com/dcm-project/k8s-container-service-provider. You can check the PRs and the codebase of this repo to see how it was built.
> Use the SKILLS defined globally
> Make no assumption
> mitigate risks before execution and ask if you still have follow up questions
> Think deep

## Project Context

- **Date:** 2026-03-16
- **Tool:** Claude Code
- **Model:** Claude Opus 4.6
- **Working Directory:** /home/gabriel/git/dcm/acm-cluster-service-provider
- **Language / Framework:** Go 1.25.5, Chi v5, oapi-codegen, Ginkgo v2 + Gomega
- **Related Spec:** .ai/specs/acm-cluster-sp.spec.md
- **Test Plan:** .ai/test-plans/acm-cluster-sp.unit-tests.md (LOCKED)
- **Checkpoint:** .ai/checkpoints/topic-4.md

### Git State

- **Branch:** feat/topic-4_api_handler
- **Base Branch:** main
- **Starting Commit:** 85b3e15 test(handler): implement RED phase for Topic 4 API handler (37 TCs)

### Build & Test Commands

| Action | Command |
|--------|---------|
| Build | `go build ./...` |
| Test (all) | `make test` (runs `ginkgo -r --race`) |
| Test (handler only) | `go test ./internal/handler/ -count=1 -v` |
| Test (handler, focused) | `go test ./internal/handler/ -count=1 -v --ginkgo.focus="TC-HDL-CRT-UT-001"` |
| Lint | `golangci-lint run ./...` |
| Full check | `make check` (fmt + vet + lint + test) |

## Analysis

### Current State

- **RED phase complete**: 37 handler unit tests all FAIL (expected — handler stubs return `nil, nil`)
- **Breakdown**: 17 CreateCluster + 4 GetCluster + 8 ListClusters + 5 DeleteCluster + 3 ErrorMapping
- **Existing GREEN tests**: health (3), registration (6), apiserver (4) — all PASS
- **Handler methods**: All are stubs returning `(nil, nil)`
- **MapDomainError**: Stub returning zero values `("", 0, "", "")`
- **Infrastructure exists**: `Handler` struct, `ClusterService` interface, `DomainError` types, mock helpers

### Target State

- **37 of 37 handler tests GREEN**
- **All existing tests still GREEN** (health, registration, apiserver)
- **golangci-lint: 0 issues**
- Handler methods implement full request lifecycle: validate → convert → delegate → map errors → respond
- Error mapping correctly translates domain errors to RFC 7807 HTTP responses
- Code organized by concern: `handler.go`, `errors.go`, `validation.go`, `convert.go`

### Codebase Exploration Findings

#### Reference Project Alignment (k8s-container-service-provider)

The reference project uses this handler structure:
- **`handler.go`** — Handler struct + constructor + all CRUD methods + health
- **`errors.go`** — Per-operation error mappers (`mapCreateError`, `mapGetError`, etc.)
- **`validation.go`** — Input validation functions (`validateContainerID`, `validateResources`)

Key patterns:
1. Handler validates inputs, calls store/service, maps errors to typed response objects
2. Per-operation error mappers return operation-specific response types (e.g., `CreateContainer409ApplicationProblemPlusJSONResponse`)
3. UUID generation for IDs: `uuid.New().String()`
4. RFC 7807 error responses with `Type`, `Title`, `Status`, `Detail` fields
5. Validation uses regex patterns matching OpenAPI spec constraints

#### Dual Type System

Both `api/v1alpha1/` and `internal/api/server/` contain identical struct definitions generated from the same OpenAPI spec. They are different Go types in different packages:
- `oapigen.Cluster` (handler/HTTP layer) vs `v1alpha1.Cluster` (service/domain layer)
- Conversion required between layers using JSON marshal/unmarshal (both have identical JSON tags)
- `oapigen.ClusterStatus("PENDING")` and `v1alpha1.ClusterStatus("PENDING")` are different types with same underlying value

#### Type Conversion Between Packages

When building error responses, `MapDomainError` returns `v1alpha1.ErrorType` but oapigen response types use `oapigen.ErrorType`. Since both have `string` as the underlying type, explicit conversion is required: `oapigen.ErrorType(errType)`. Similarly, all response type aliases like `CreateCluster400ApplicationProblemPlusJSONResponse` are `type ... Error` (oapigen.Error), so constructing them from an `oapigen.Error` is a simple type conversion: `CreateCluster400ApplicationProblemPlusJSONResponse(errObj)`. The `ListClustersParams.MaxPageSize` is `*int32` but `ClusterService.List` takes `pageSize int` — convert via `int(*req.Params.MaxPageSize)`.

#### Error Response Types (Generated)

Each operation has typed error responses in `oapigen`:
- Create: `400`, `401`, `403`, `409`, `422`, `500`
- Get: `401`, `403`, `404`, `500`
- List: `400`, `401`, `403`, `500`
- Delete: `401`, `403`, `404`, `500`

All error response types wrap `oapigen.Error` which has: `Type ErrorType`, `Title string`, `Status *int32`, `Detail *string`, `Instance *string`.

## File Inventory

| Action | File Path | Purpose / What Changes |
|--------|-----------|----------------------|
| MODIFY | `internal/handler/handler.go` | Implement CreateCluster, GetCluster, ListClusters, DeleteCluster (replace stubs) |
| MODIFY | `internal/handler/errors.go` | Implement MapDomainError + per-operation error response builders |
| CREATE | `internal/handler/validation.go` | Input validation: service_type, version, nodes, memory format, ID format |
| CREATE | `internal/handler/convert.go` | Type conversion between oapigen and v1alpha1 via JSON roundtrip |
| MODIFY | `.ai/checkpoints/topic-4.md` | Update checkpoint with GREEN phase completion |

## Key Code Context

### Handler Struct & Interface (internal/handler/handler.go)
```go
// handler.go:14-18
type Handler struct {
    clusterService service.ClusterService
    healthChecker  service.HealthChecker
}
```

### ClusterService Interface (internal/service/interfaces.go)
```go
// interfaces.go:11-16
type ClusterService interface {
    Create(ctx context.Context, id string, cluster v1alpha1.Cluster) (*v1alpha1.Cluster, error)
    Get(ctx context.Context, id string) (*v1alpha1.Cluster, error)
    List(ctx context.Context, pageSize int, pageToken string) (*v1alpha1.ClusterList, error)
    Delete(ctx context.Context, id string) error
}
```

### DomainError (internal/service/errors.go)
```go
// errors.go:10-15
type DomainError struct {
    Type    v1alpha1.ErrorType
    Message string
    Detail  string
    Cause   error
}
```

### MapDomainError Signature (internal/handler/errors.go)
```go
// errors.go:9-11 — current stub, to be replaced
func MapDomainError(_ error) (v1alpha1.ErrorType, int, string, string) {
    return "", 0, "", ""
}
```

### Generated Request/Response Types (internal/api/server/server.gen.go)
```go
// server.gen.go:715-718
type CreateClusterRequestObject struct {
    Params CreateClusterParams
    Body   *CreateClusterJSONRequestBody  // = Cluster
}

// server.gen.go:724
type CreateCluster201JSONResponse Cluster

// server.gen.go:733
type CreateCluster400ApplicationProblemPlusJSONResponse Error

// server.gen.go:855-857
type GetClusterRequestObject struct {
    ClusterId ClusterIdPath `json:"clusterId"`
}

// server.gen.go:662-664
type ListClustersRequestObject struct {
    Params ListClustersParams
}

// server.gen.go:803-805
type DeleteClusterRequestObject struct {
    ClusterId ClusterIdPath `json:"clusterId"`
}
```

### Test Mock Pattern (internal/handler/helpers_test.go)
```go
// helpers_test.go:16-19
type mockClusterService struct {
    CreateFunc func(ctx context.Context, id string, cluster v1alpha1.Cluster) (*v1alpha1.Cluster, error)
    GetFunc    func(ctx context.Context, id string) (*v1alpha1.Cluster, error)
    ListFunc   func(ctx context.Context, pageSize int, pageToken string) (*v1alpha1.ClusterList, error)
    DeleteFunc func(ctx context.Context, id string) error
}
```

### v1alpha1 Error Constants (api/v1alpha1/types.gen.go)
```go
// types.gen.go:96-103
const (
    ALREADYEXISTS       ErrorType = "ALREADY_EXISTS"
    INTERNAL            ErrorType = "INTERNAL"
    INVALIDARGUMENT     ErrorType = "INVALID_ARGUMENT"
    NOTFOUND            ErrorType = "NOT_FOUND"
    PERMISSIONDENIED    ErrorType = "PERMISSION_DENIED"
    UNAUTHENTICATED     ErrorType = "UNAUTHENTICATED"
    UNAVAILABLE         ErrorType = "UNAVAILABLE"
    UNPROCESSABLEENTITY ErrorType = "UNPROCESSABLE_ENTITY"
)
```

### Project Conventions to Follow

- External test packages (`package handler_test`)
- `util.Ptr(v)` for pointer creation
- `slog` for structured logging (not used in handler currently, but available)
- RFC 7807 `application/problem+json` for all errors
- Commit format: `<type>(<scope>): <subject>` with `-s` flag and Co-Authored-By
- Do not modify locked test files

## Design Decisions

| Decision | Choice | Alternatives Considered | Rationale |
|----------|--------|------------------------|-----------|
| Type conversion approach | JSON marshal/unmarshal roundtrip | Manual field-by-field copy, unsafe.Pointer | JSON roundtrip is safe, correct, handles nested types/pointers automatically. Both type packages have identical JSON tags. Performance is negligible for HTTP handlers |
| Error mapping architecture | Single `MapDomainError` returns components + per-operation helpers in handler methods | Per-operation error mapper methods (reference project pattern) | Tests already define `MapDomainError` as exported with `(ErrorType, int, string, string)` signature — cannot change. Per-operation helpers use these components to build typed response objects |
| Validation placement | Handler-level validation functions (before service call) | Rely solely on OpenAPI middleware | Tests call handler methods directly (no middleware). Handler must validate independently. Defense-in-depth aligned with reference project |
| max_page_size limit | 100 (not 1000) | 1000 (OpenAPI spec default) | Test TC-HDL-LST-UT-002 and TC-HDL-LST-UT-007 explicitly test that 101 returns 400. Test plan says "max_page_size limit corrected to 100 per REQ-API-270" |
| INTERNAL error detail | Generic message "an internal error occurred" | Pass through DomainError.Message | TC-ERR-UT-002 requires detail must not contain "k8s" or "kube-apiserver". Using generic message is safest |
| File organization | 4 files: handler.go, errors.go, validation.go, convert.go | Single handler.go, or one file per operation | Aligned with reference project (handler.go + errors.go + validation.go) plus convert.go for dual-type conversion unique to this project |

## Scope Boundaries

### In Scope

- Implement 4 handler methods: CreateCluster, GetCluster, ListClusters, DeleteCluster
- Implement MapDomainError with correct error type → HTTP status mapping
- Input validation (service_type, version, nodes, memory format, ID format, max_page_size)
- Type conversion between oapigen and v1alpha1
- Error response builders for each operation
- Update checkpoint file

### Out of Scope — Do NOT Change

- **Test files**: `handler_unit_test.go`, `handler_test.go`, `helpers_test.go` — LOCKED
- **Service interface**: `internal/service/interfaces.go` — already correct
- **Domain errors**: `internal/service/errors.go` — already correct
- **Generated code**: `internal/api/server/server.gen.go`, `api/v1alpha1/types.gen.go`
- **OpenAPI spec**: `api/v1alpha1/openapi.yaml`
- **Main.go**: `cmd/acm-cluster-service-provider/main.go` — currently passes `nil` for ClusterService, that's Topic 5
- **Health endpoint**: Already implemented, no changes needed
- **Other test suites**: health, registration, apiserver tests

## Relevant Requirements & Test Cases (Inlined)

### From Spec: .ai/specs/acm-cluster-sp.spec.md

- **REQ-API-010:** StrictServerInterface implementation with compile-time check
- **REQ-API-020:** All error responses use application/problem+json with Error schema (RFC 7807)
- **REQ-API-030:** Error type is one of ErrorType enum values
- **REQ-API-040:** Error status matches HTTP status code
- **REQ-API-050:** Internal errors do NOT leak implementation details
- **REQ-API-060:** Accept JSON body conforming to Cluster schema
- **REQ-API-070:** version and nodes.control_plane are required
- **REQ-API-080:** nodes.workers required with count >= 1; if omitted, return 400
- **REQ-API-090:** Validate service_type == "cluster"; return 400 if not
- **REQ-API-100:** ID handling: client-specified via ?id=, or server-generated UUID. Empty ?id= treated as absent
- **REQ-API-101:** id in request body is readOnly, MUST be ignored
- **REQ-API-110:** Successful creation returns 201 with full Cluster including id, path, status=PENDING
- **REQ-API-130:** Unsupported platform returns 422
- **REQ-API-140:** Version with no matching ClusterImageSet returns 422
- **REQ-API-150:** Malformed/invalid body returns 400
- **REQ-API-160:** Read-only fields in body are ignored
- **REQ-API-170:** Memory/storage must match `^[1-9][0-9]*(MB|GB|TB)$`
- **REQ-API-180:** GetCluster returns Cluster with given ID
- **REQ-API-200:** Non-existent cluster returns 404
- **REQ-API-220:** READY cluster includes api_endpoint, console_uri, kubeconfig
- **REQ-API-230:** Non-READY cluster has empty/absent credentials
- **REQ-API-231:** UNAVAILABLE cluster SHOULD have credentials if available
- **REQ-API-240:** ListClusters returns paginated ClusterList
- **REQ-API-250:** Cursor-based pagination with page_token and max_page_size
- **REQ-API-260:** max_page_size defaults to 50
- **REQ-API-270:** max_page_size > 100 returns 400
- **REQ-API-280:** max_page_size < 1 returns 400
- **REQ-API-290:** Invalid page_token returns 400
- **REQ-API-300:** No more results → next_page_token absent/empty
- **REQ-API-320:** Delete returns 204 No Content
- **REQ-API-340:** Delete non-existent cluster returns 404
- **REQ-API-360:** Delete already-deleted cluster returns 404
- **REQ-API-370:** Delete cluster in DELETING state is idempotent (204)

### From Test Plan: .ai/test-plans/acm-cluster-sp.unit-tests.md (37 TCs)

#### CreateCluster (17 TCs)

| TC ID | Title | Key Assertion |
|-------|-------|---------------|
| TC-HDL-CRT-UT-001 | Server-generated ID | Response has UUID-format id, status=PENDING, path set |
| TC-HDL-CRT-UT-002 | Client-specified ID | Response id matches "my-custom-id", path="/api/v1alpha1/clusters/my-custom-id" |
| TC-HDL-CRT-UT-003 | Read-only fields ignored | Body id/status/api_endpoint/path/kubeconfig/console_uri ignored, result has PENDING status |
| TC-HDL-CRT-UT-004 | Invalid service_type | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-005 | Missing workers | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-006 | Invalid memory format | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-007 | Duplicate ID from service | 409 with ALREADY_EXISTS |
| TC-HDL-CRT-UT-008 | Duplicate name from service | 409 with ALREADY_EXISTS, detail contains "test-cluster" |
| TC-HDL-CRT-UT-009 | Unsupported platform | 422 with UNPROCESSABLE_ENTITY |
| TC-HDL-CRT-UT-010 | Version not found | 422 with UNPROCESSABLE_ENTITY |
| TC-HDL-CRT-UT-011 | Missing required fields (version) | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-012 | Internal error no leak | 500 with INTERNAL, detail does NOT contain "k8s" or "kube-apiserver" |
| TC-HDL-CRT-UT-013 | Missing nodes entirely | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-014 | Workers count below minimum | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-015 | Invalid ?id= format | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-016 | Missing service_type | 400 with INVALID_ARGUMENT |
| TC-HDL-CRT-UT-019 | Empty ?id= treated as absent | UUID generated (same as TC-001) |

#### GetCluster (4 TCs)

| TC ID | Title | Key Assertion |
|-------|-------|---------------|
| TC-HDL-GET-UT-001 | READY cluster with credentials | 200 with id, status=READY, api_endpoint, kubeconfig, console_uri set |
| TC-HDL-GET-UT-002 | Non-READY cluster | 200 with status=PENDING, no credentials |
| TC-HDL-GET-UT-003 | Non-existent cluster | 404 with NOT_FOUND |
| TC-HDL-GET-UT-005 | UNAVAILABLE with credentials | 200 with status=UNAVAILABLE, credentials present |

#### ListClusters (8 TCs)

| TC ID | Title | Key Assertion |
|-------|-------|---------------|
| TC-HDL-LST-UT-001 | Default pagination (50) | 200 with 50 clusters, next_page_token set |
| TC-HDL-LST-UT-002 | max_page_size > 100 | 400 with INVALID_ARGUMENT |
| TC-HDL-LST-UT-003 | max_page_size < 1 | 400 with INVALID_ARGUMENT |
| TC-HDL-LST-UT-004 | Invalid page_token | 400 with INVALID_ARGUMENT (service returns error) |
| TC-HDL-LST-UT-005 | Last page (no token) | 200 with clusters, next_page_token nil |
| TC-HDL-LST-UT-006 | Empty collection | 200 with empty clusters array, next_page_token nil |
| TC-HDL-LST-UT-007 | Boundary values | 1=OK, 100=OK, 101=400 |
| TC-HDL-LST-UT-008 | Empty page_token | Treated as absent, 200 returned |

#### DeleteCluster (5 TCs)

| TC ID | Title | Key Assertion |
|-------|-------|---------------|
| TC-HDL-DEL-UT-001 | Successful deletion | 204 (DeleteCluster204Response) |
| TC-HDL-DEL-UT-002 | Non-existent cluster | 404 with NOT_FOUND |
| TC-HDL-DEL-UT-003 | Already-deleted cluster | 404 with NOT_FOUND |
| TC-HDL-DEL-UT-004 | Idempotent delete of DELETING | 204 (service returns nil) |
| TC-HDL-DEL-UT-005 | Get during deletion | GetCluster returns 200 with DELETING status |

#### Error Mapping (3 TCs)

| TC ID | Title | Key Assertion |
|-------|-------|---------------|
| TC-ERR-UT-001 | Error type → HTTP status | INVALID_ARGUMENT→400, NOT_FOUND→404, ALREADY_EXISTS→409, UNPROCESSABLE_ENTITY→422, INTERNAL→500, UNAVAILABLE→503 |
| TC-ERR-UT-002 | Internal errors no leak | Detail is non-empty but does not contain "k8s" or "kube-apiserver" |
| TC-ERR-UT-003 | Detail included when set | DomainError with Detail → detail field in return |

## Test Strategy (BDD - GREEN Phase)

### Tests Already Written (LOCKED — DO NOT MODIFY)
- `internal/handler/handler_unit_test.go` — 37 TCs all RED
- `internal/handler/handler_test.go` — Suite bootstrap
- `internal/handler/helpers_test.go` — Mocks and test helpers

### Strategy
Tests are locked. Implementation must make all 37 tests pass without modifying any test file. This is the GREEN phase of BDD — write the minimum implementation to make tests pass.

### Test Execution
```bash
# Run handler tests only
go test ./internal/handler/ -count=1 -v

# Run all tests (ensure no regressions)
make test

# Run single TC for debugging
go test ./internal/handler/ -count=1 -v --ginkgo.focus="TC-HDL-CRT-UT-001"
```

## Test-to-Implementation Mapping

| Test Case | What Makes It Pass |
|-----------|-------------------|
| TC-HDL-CRT-UT-001 | CreateCluster: when no ?id=, generate UUID via `uuid.New().String()`, call service.Create, convert result, return 201 |
| TC-HDL-CRT-UT-002 | CreateCluster: when ?id= is set, use it as-is, verify path="/api/v1alpha1/clusters/{id}" |
| TC-HDL-CRT-UT-003 | CreateCluster: strip read-only fields from body before passing to service, return service result |
| TC-HDL-CRT-UT-004 | CreateCluster: validate service_type == "cluster", return 400 if not |
| TC-HDL-CRT-UT-005 | CreateCluster: validate workers present (WorkerSpec non-zero), return 400 if missing |
| TC-HDL-CRT-UT-006 | CreateCluster: validate memory format matches `^[1-9][0-9]*(MB\|GB\|TB)$`, return 400 |
| TC-HDL-CRT-UT-007 | CreateCluster: service returns ALREADY_EXISTS → mapCreateError returns 409 |
| TC-HDL-CRT-UT-008 | CreateCluster: service returns ALREADY_EXISTS with Detail → 409 with detail containing cluster name |
| TC-HDL-CRT-UT-009 | CreateCluster: service returns UNPROCESSABLE_ENTITY → mapCreateError returns 422 |
| TC-HDL-CRT-UT-010 | CreateCluster: service returns UNPROCESSABLE_ENTITY → 422 |
| TC-HDL-CRT-UT-011 | CreateCluster: validate version != "", return 400 if empty |
| TC-HDL-CRT-UT-012 | CreateCluster: service returns INTERNAL with cause → 500, detail does NOT leak internals |
| TC-HDL-CRT-UT-013 | CreateCluster: validate nodes.control_plane non-zero (cpu > 0), return 400 |
| TC-HDL-CRT-UT-014 | CreateCluster: validate workers.count >= 1, return 400 |
| TC-HDL-CRT-UT-015 | CreateCluster: validate ?id= format matches `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`, return 400 |
| TC-HDL-CRT-UT-016 | CreateCluster: validate service_type != "" (same check as TC-004) |
| TC-HDL-CRT-UT-019 | CreateCluster: empty ?id= ("") → treat as absent → generate UUID |
| TC-HDL-GET-UT-001 | GetCluster: call service.Get, convert v1alpha1.Cluster to oapigen, return 200 |
| TC-HDL-GET-UT-002 | GetCluster: same as TC-001, service returns PENDING cluster (no credentials) |
| TC-HDL-GET-UT-003 | GetCluster: service returns NOT_FOUND → mapGetError returns 404 |
| TC-HDL-GET-UT-005 | GetCluster: service returns UNAVAILABLE cluster with credentials → 200 with credentials |
| TC-HDL-LST-UT-001 | ListClusters: default max_page_size=50 when nil, call service.List, convert result, return 200 |
| TC-HDL-LST-UT-002 | ListClusters: validate max_page_size <= 100, return 400 if exceeded |
| TC-HDL-LST-UT-003 | ListClusters: validate max_page_size >= 1, return 400 if below |
| TC-HDL-LST-UT-004 | ListClusters: service returns INVALID_ARGUMENT for bad token → mapListError returns 400 |
| TC-HDL-LST-UT-005 | ListClusters: service returns list without next_page_token → 200, nil token |
| TC-HDL-LST-UT-006 | ListClusters: service returns empty list → 200, empty clusters array, nil token |
| TC-HDL-LST-UT-007 | ListClusters: boundary tests — 1=OK, 100=OK, 101=400 |
| TC-HDL-LST-UT-008 | ListClusters: empty page_token ("") → treat as absent (""), pass to service |
| TC-HDL-DEL-UT-001 | DeleteCluster: call service.Delete, return 204 |
| TC-HDL-DEL-UT-002 | DeleteCluster: service returns NOT_FOUND → mapDeleteError returns 404 |
| TC-HDL-DEL-UT-003 | DeleteCluster: service returns NOT_FOUND → 404 (same mapping) |
| TC-HDL-DEL-UT-004 | DeleteCluster: service returns nil → 204 (idempotent) |
| TC-HDL-DEL-UT-005 | GetCluster: service returns DELETING status → 200 with DELETING (pass-through) |
| TC-ERR-UT-001 | MapDomainError: switch on DomainError.Type → return (type, status, title, detail) |
| TC-ERR-UT-002 | MapDomainError: INTERNAL type → generic detail "an internal error occurred" |
| TC-ERR-UT-003 | MapDomainError: DomainError.Detail set → return it as 4th value |

## Implementation Steps

### Step 1: Create type conversion utilities

- **Commit type:** `feat(handler)`
- **Files:** CREATE `internal/handler/convert.go`
- **Changes:**
  - `toServiceCluster(c oapigen.Cluster) (v1alpha1.Cluster, error)` — JSON marshal/unmarshal from oapigen to v1alpha1
  - `toAPICluster(c *v1alpha1.Cluster) (oapigen.Cluster, error)` — JSON marshal/unmarshal from v1alpha1 to oapigen
  - `toAPIClusterList(c *v1alpha1.ClusterList) (oapigen.ClusterList, error)` — same for ClusterList
- **Validates:** Prerequisite for handler methods; no tests directly target this but TC-HDL-CRT-UT-001, TC-HDL-GET-UT-001, TC-HDL-LST-UT-001 depend on correct conversion

### Step 2: Create input validation functions

- **Commit type:** `feat(handler)`
- **Files:** CREATE `internal/handler/validation.go`
- **Changes:**
  - `validateCreateRequest(req oapigen.CreateClusterRequestObject) error` — validates in this order:
    1. service_type == "cluster" (TC-004, TC-016) — checked first as cheapest validation
    2. version != "" (TC-011)
    3. nodes.workers.count >= 1 (TC-005, TC-013, TC-014) — catches both "missing workers" and "missing nodes entirely" since zero-valued ClusterNodes{} has Workers.Count=0
    4. nodes.control_plane.cpu >= 2 (TC-013) — catches zero-valued ControlPlane; order doesn't matter for TC-013 since workers check (step 3) would already catch it
    5. memory/storage format matches `^[1-9][0-9]*(MB|GB|TB)$` for workers and control_plane (TC-006) — only checked when the string is non-empty (empty string means field was zero-valued, already caught by cpu/count checks above)
  - `validateClientID(id string) error` — validates ?id= format matches `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$` (TC-015)
  - `validateMaxPageSize(size int32) error` — validates 1 <= size <= 100 (TC-002, TC-003, TC-007)
- **Validates:** TC-HDL-CRT-UT-004..006, 011, 013..016, TC-HDL-LST-UT-002..003, 007

### Step 3: Implement MapDomainError and per-operation error helpers

- **Commit type:** `feat(handler)`
- **Files:** MODIFY `internal/handler/errors.go`
- **Changes:**
  - Replace stub `MapDomainError` with real implementation:
    - Use `errors.As` to extract `*service.DomainError`
    - Map `DomainError.Type` to HTTP status code: INVALID_ARGUMENT→400, NOT_FOUND→404, ALREADY_EXISTS→409, UNPROCESSABLE_ENTITY→422, INTERNAL→500, UNAVAILABLE→503
    - For INTERNAL: return generic detail "an internal error occurred" (not Message/Cause)
    - For other types: return `DomainError.Detail` if non-empty, otherwise `DomainError.Message`
    - Unknown/non-DomainError: return INTERNAL/500 with generic detail
  - Add `createErrorResponse(errType oapigen.ErrorType, status int32, title, detail string) oapigen.Error` helper to build RFC 7807 Error struct
- **Validates:** TC-ERR-UT-001, TC-ERR-UT-002, TC-ERR-UT-003

### Step 4: Implement all handler methods

- **Commit type:** `feat(handler)`
- **Files:** MODIFY `internal/handler/handler.go`
- **Changes:**
  - **CreateCluster:**
    1. Resolve ID: if `req.Params.Id == nil || *req.Params.Id == ""` → `uuid.New().String()`, else use `*req.Params.Id`
    2. Validate ?id= format (if client-specified)
    3. Validate request body (service_type, version, nodes, memory format)
    4. Strip read-only fields from body by setting them to nil/zero: `body.Id = nil; body.Status = nil; body.ApiEndpoint = nil; body.Kubeconfig = nil; body.CreateTime = nil; body.UpdateTime = nil; body.Path = nil; body.StatusMessage = nil; body.ConsoleUri = nil`
    5. Convert oapigen.Cluster → v1alpha1.Cluster
    6. Call `h.clusterService.Create(ctx, id, cluster)`
    7. On error: call `MapDomainError(err)` to get `(errType, status, title, detail)`, build `oapigen.Error{Type: oapigen.ErrorType(errType), Title: title, Status: util.Ptr(int32(status)), Detail: util.Ptr(detail)}`, then switch on status: 400→`CreateCluster400ApplicationProblemPlusJSONResponse(errObj)`, 409→`CreateCluster409...(errObj)`, 422→`CreateCluster422...(errObj)`, default→`CreateCluster500...(errObj)`
    8. On success: convert v1alpha1.Cluster → oapigen.Cluster → CreateCluster201JSONResponse
  - **GetCluster:**
    1. Call `h.clusterService.Get(ctx, req.ClusterId)`
    2. On error: call `MapDomainError(err)`, build `oapigen.Error`, switch on status: 404→`GetCluster404ApplicationProblemPlusJSONResponse(errObj)`, default→`GetCluster500ApplicationProblemPlusJSONResponse(errObj)`
    3. On success: convert → GetCluster200JSONResponse
  - **ListClusters:**
    1. Resolve max_page_size: nil → 50, else validate 1..100 (convert `*int32` → `int` via `int(*req.Params.MaxPageSize)`)
    2. Resolve page_token: nil or "" → ""
    3. Call `h.clusterService.List(ctx, pageSize, pageToken)`
    4. On error: call `MapDomainError(err)`, build `oapigen.Error`, switch on status: 400→`ListClusters400ApplicationProblemPlusJSONResponse(errObj)`, default→`ListClusters500ApplicationProblemPlusJSONResponse(errObj)`
    5. On success: convert → ListClusters200JSONResponse
  - **DeleteCluster:**
    1. Call `h.clusterService.Delete(ctx, req.ClusterId)`
    2. On error: call `MapDomainError(err)`, build `oapigen.Error`, switch on status: 404→`DeleteCluster404ApplicationProblemPlusJSONResponse(errObj)`, default→`DeleteCluster500ApplicationProblemPlusJSONResponse(errObj)`
    3. On success: return DeleteCluster204Response{}
  - Add import for `github.com/google/uuid`
  - Run `go mod tidy` to promote `github.com/google/uuid` from indirect to direct dependency
- **Validates:** All 37 TCs

### Step 5: Run `make check`, verify all 37 tests GREEN + no regressions + lint clean

- **Commit type:** N/A (verification only)
- **Files:** None
- **Changes:** Run `make check` to verify:
  - 37 handler tests GREEN
  - All existing tests GREEN (health, registration, apiserver)
  - golangci-lint: 0 issues
- **Validates:** All TCs + regression safety

### Step 6: Update checkpoint and commit

- **Commit type:** `feat(handler)`
- **Files:** MODIFY `.ai/checkpoints/topic-4.md`
- **Changes:** Update with GREEN phase completion, test results, files modified
- **Validates:** N/A (documentation)

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Type conversion failure between oapigen ↔ v1alpha1 | Tests fail on happy paths | JSON roundtrip is provably correct for identical struct layouts. Test each conversion early |
| Validation logic doesn't match test expectations | Wrong tests pass/fail | Verify each validation test case individually with `--ginkgo.focus` |
| MapDomainError signature change | Won't compile | Signature is locked by tests — `(error) (v1alpha1.ErrorType, int, string, string)`. Only implementation changes |
| max_page_size boundary (100 not 1000) | Test TC-HDL-LST-UT-007 fails | Test plan explicitly says "corrected to 100 per REQ-API-270". TC-007 tests 101 → 400 |
| UUID dependency not direct | Build fails | `github.com/google/uuid` is already an indirect dep. Run `go mod tidy` after adding import |
| Regression in existing tests | Existing GREEN tests break | Run `make test` after implementation to catch regressions. Handler changes don't touch health/registration/apiserver code |

## Self-Reflection Checkpoint

After implementation, answer explicitly:
- Are there edge cases not handled? Validate empty strings, nil pointers, zero values
- Are there potential security issues? No SQL injection (no SQL). No command injection. Internal errors don't leak details (TC-ERR-UT-002)
- Does it follow project conventions? External test package, util.Ptr(), oapigen alias, commit format
- What could be wrong? Type conversion might lose time zone info on CreateTime/UpdateTime — verify JSON roundtrip preserves time.Time correctly

## Session State

### Last Updated
2026-03-16

### Current Session
Session 2 — GREEN phase implementation

### Phase Completion Log

| Phase | Completed | Validated By | Notes |
|-------|-----------|--------------|-------|
| Spec | 2026-02-13 | Human approved | REQ-API-xxx defined |
| Test Plan | 2026-03-16 | Human approved | 37 TCs, LOCKED |
| Test Impl (RED) | 2026-03-16 | 37 FAIL, 0 PASS | Commit 85b3e15 |
| Code Impl (GREEN) | 2026-03-16 | 37 PASS, 74 total PASS | Commit faa4ce5 |
| Review | 2026-03-16 | Subagent PASS | Zero Must Fix/Should Fix |

### Completed Steps
- [x] Phase 1: Gather context & draft plan
- [x] Phase 2 - Review iteration 1: 0 Must Fix, 5 Should Fix (all resolved), 8 Consider (deferred)
- [x] Phase 2 - Review iteration 2: 0 Must Fix, 0 Should Fix, 2 Consider (deferred). Plan passes review.
- [x] Phase 3: Present for approval
- [x] Step 1: Create type conversion utilities (convert.go)
- [x] Step 2: Create input validation functions (validation.go)
- [x] Step 3: Implement MapDomainError (errors.go)
- [x] Step 4: Implement all handler methods (handler.go)
- [x] Step 5: Run make check — 74/74 tests GREEN, 0 lint issues
- [x] Step 6: Update checkpoint and commit (faa4ce5)

### Key Decisions This Session
- JSON roundtrip for type conversion (safe, correct, no unsafe)
- max_page_size limit is 100 (per corrected test plan)
- Generic "an internal error occurred" for INTERNAL error details
- 4-file structure aligned with reference project
- `nilerr` lint suppressed with nolint directive for intentional error-to-response pattern

### Deferred Findings
- [Review R1] JSON roundtrip may alter `time.Time` precision (nanosecond → RFC 3339). No test checks time equality, so not blocking.
- [Review R1] `go.mod`/`go.sum` will be modified by `go mod tidy` (uuid promotion). Not listed in File Inventory since implicit.
- [Review R1] UNAVAILABLE status handled via string cast (not in enum). JSON roundtrip preserves this correctly.
- [Review R1] OpenAPI spec says max_page_size maximum=1000, but corrected requirement (REQ-API-270) and locked tests mandate 100. This is a known deviation documented in Design Decisions.

### Context to Preserve for Next Session
- Tests are LOCKED — do not modify
- main.go passes nil for ClusterService (Topic 5 concern)
- UNAVAILABLE status uses string cast `v1alpha1.ClusterStatus("UNAVAILABLE")` (not in generated enum)

### Resume Prompt for Next Session
```
"Read .ai/plans/2026-03-16_topic-4-green-phase_claude.md and continue from Step 1.
Tests are LOCKED. Implement handler methods to make all 37 tests GREEN."
```

## Rollback Plan

```bash
git checkout feat/topic-4_api_handler
git reset --hard 85b3e15
```

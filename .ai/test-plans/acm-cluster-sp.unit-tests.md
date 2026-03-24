# Test Plan: ACM Cluster Service Provider -- Unit Tests

## Overview

- **Related Spec:** .ai/specs/acm-cluster-sp.spec.md
- **Related Requirements:** REQ-REG-xxx, REQ-HTTP-xxx, REQ-HLT-xxx, REQ-API-xxx, REQ-ACM-xxx, REQ-KV-xxx, REQ-BM-xxx, REQ-MON-xxx, REQ-XC-xxx
- **Created:** 2026-02-17
- **Last Updated:** 2026-03-24 (REQ-ACM-060/061: CP resource override annotations — refined TC-KV-UT-003, added TC-BM-UT-014)
- **Scope:** This file covers **unit tests only** (131 unit test cases + 4 reclassified as integration/middleware). Integration tests are in `acm-cluster-sp.integration-tests.md`.

## Design Principles

1. **Status mapping is SHARED** -- a single `statusFromConditions()` function tested once, used by both KubeVirt and BareMetal. Status mapping tests live in a shared `status` component, not duplicated per platform.
2. **Test behaviors, not implementation details** -- CRD type strategy resolved: tests use typed HyperShift Go structs from `github.com/openshift/hypershift/api` (separate lightweight API module) with the `client.Client` fake.
3. **Zero gaps** -- every REQ-xxx appears in at least one TC mapping.
4. **Zero redundancy** -- no behavioral scenario tested twice at the same layer.
5. **Layer isolation** -- handler tests mock `ClusterService`/`HealthChecker`; service tests use fake K8s client; integration tests use `envtest` + `httptest` with no mocks.

## Test Framework
- **Framework:** Ginkgo v2 + Gomega
- **Rationale:** Aligned with K8s Container SP per cross-SP alignment review

## Decisions Log

Decisions made during test plan creation that affect test design:

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Zero-value resources (`"0GB"`) | Reject — OpenAPI regex `^[1-9][0-9]*(MB\|GB\|TB)$` rejects at middleware level | Prevents invalid zero-value quantities from reaching business logic |
| Version matching strategy | K8s minor version match + compatibility matrix translation | `version="1.30"` must match a K8s minor version in the compatibility matrix exactly; SP translates to OCP for ClusterImageSet lookup |
| Partial create failure (HC ok, NP fails) | Rollback HostedCluster | Delete orphaned HostedCluster before returning error; ensures atomicity |
| `base_domain` default | Shared across both platforms (HyperShift HostedCluster requires `dns.baseDomain` regardless of platform type). Optional at startup; validated at request time | Request `provider_hints.acm.base_domain` overrides config default |
| Status change semantics | DCM-mapped status only | Events published only when DCM status changes, not on every K8s condition update |
| NATS publish failure | Retry with configurable interval + max | `SP_NATS_PUBLISH_RETRY_INTERVAL=2s`, `SP_NATS_PUBLISH_RETRY_MAX=3`. Log and drop on exhaustion |
| `console_uri` source | Construct from configurable pattern | `SP_CONSOLE_URI_PATTERN` template (default: `https://console-openshift-console.apps.{name}.{base_domain}`) when READY. Pattern is configurable since it may change across HyperShift versions |
| Health critical deps | K8s API + HyperShift CRD | REQ-HLT-070/080 upgraded to MUST. Platform checks remain SHOULD (non-critical) |
| Empty `page_token` | Treat as absent | Empty string returns first page, not a 400 error |
| `max_page_size` upper limit | 100 (per REQ-API-270) | Spec says 1-100; original test plan had 1-1000 which contradicted the spec |
| Middleware-validated TCs reclassified | TC-HDL-GET-UT-004, TC-HDL-DEL-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018 moved to integration scope | OpenAPI spec patterns (`ClusterIdPath`, memory/storage regex) are enforced by the validation middleware before the handler. The generated `StrictServerInterface` has no 400 response types for GetCluster/DeleteCluster, confirming these validations cannot be returned by the handler |
| `metadata.name` validation | OpenAPI pattern | Already defined in OpenAPI spec, enforced by validation middleware. No handler code needed |
| OpenAPI middleware validation | Middleware handles pattern/enum/min/max/required | `RequestErrorHandlerFunc` rejects invalid requests before handler code. Tests like TC-HDL-CRT-UT-004, TC-HDL-CRT-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018 verify middleware behavior as part of the API contract |
| Utility function testing | Tested transitively, IDs kept for traceability | Pure utility functions (format conversion) are tested through consuming service/handler tests, not standalone. TC-STS-UT-xxx, TC-ERR-UT-xxx IDs remain for requirement coverage mapping |

---

## Test Architecture

### Layer Diagram

```
                      /\
                     /  \
                    / IT  \         ~7 integration tests (envtest + httptest)
                   /________\       Prove wiring, routes, full round-trip
                  /          \
                 / Service    \     ~25 service tests (fake K8s client)
                /   Tests      \    Prove CRD construction, K8s interactions
               /________________\
              /                  \
             /  Handler Tests     \  ~27 handler tests (mocked services)
            /                      \ Prove validation, HTTP codes, error format
           /________________________\
          /                          \
         /   Shared Component Tests   \ ~11 table-driven tests (pure functions)
        /                              \ Prove status mapping, errors
       /________________________________\
```

### Test File Layout (Recommended)

> **Note:** The file layout below is **recommended**, not prescriptive. Test cases are identified by their TC-xxx IDs regardless of file location.

```
internal/
  service/
    interfaces.go                          # ClusterService, HealthChecker, StatusPublisher
    errors.go                              # Domain errors (typed, maps to ErrorType enum)
    status/
      mapper.go                            # MapConditionsToStatus() -- shared pure function
      mapper_test.go                       # TC-STS-UT-xxx: implemented within kubevirt_test.go and baremetal_test.go
  handler/
    handler.go                             # StrictServerInterface implementation
    handler_test.go                        # TC-HDL-UT-xxx: mock ClusterService + HealthChecker
  cluster/
    kubevirt/
      kubevirt.go                          # KubeVirt ClusterService implementation
      kubevirt_test.go                     # TC-KV-UT-xxx: fake K8s client
    baremetal/
      baremetal.go                         # BareMetal ClusterService implementation
      baremetal_test.go                    # TC-BM-UT-xxx: fake K8s client
  health/
    health.go                              # HealthChecker implementation
    health_test.go                         # TC-HLT-UT-xxx: fake K8s client
  monitor/
    monitor.go                             # Status Monitor (informer + StatusPublisher)
    monitor_test.go                        # TC-MON-UT-xxx: client-go fake + mock publisher
  registration/
    registration.go                        # DCM Registration logic
    registration_test.go                   # TC-REG-UT-xxx: httptest + fake K8s
  config/
    config.go                              # Environment variable loading
    config_test.go                         # TC-CFG-UT-xxx: env var tests
  testutil/
    builders.go                            # Test fixture builders (Cluster, HostedCluster)
    fakes.go                               # Reusable mock/fake implementations
    assertions.go                          # RFC 7807 assertion helpers
test/
  integration/
    suite_test.go                          # envtest setup, shared TestMain
    cluster_lifecycle_test.go              # TC-INT-xxx: full HTTP -> K8s round-trips
    health_integration_test.go             # TC-INT-xxx: health endpoint integration
```

### Layer Responsibilities

| Layer | Mocks/Fakes | Tests What | Does NOT Test |
|-------|-------------|------------|---------------|
| Shared: Status | None (pure function) | Condition precedence, all 7 DCM statuses. Implemented within service test files (kubevirt_test.go, baremetal_test.go) | K8s client calls |
| Shared: Errors | None (pure function) | Error type -> HTTP status mapping, RFC 7807 | Business logic |
| Handler | Mock ClusterService, Mock HealthChecker | Input validation, HTTP codes, read-only fields, pagination, delegation | K8s CRD construction, status mapping |
| KubeVirt Service | Fake K8s client (`controller-runtime/pkg/client/fake`) | CRD construction, labels, ClusterImageSet lookup, kubeconfig extraction, rollback | HTTP codes, validation already done by handler |
| BareMetal Service | Fake K8s client | Agent platform CRD, InfraEnv ref, agent labels | Status mapping (shared), format conversion (shared) |
| Health | Fake K8s client + error interceptor | Healthy/unhealthy determination, timeout, fixed fields | HTTP transport |
| Monitor | `client-go/fake` + Mock StatusPublisher | CloudEvent construction, debounce, filtering, deletion | NATS transport |
| Registration | `httptest.NewServer` + Fake K8s client | Payload, retry, idempotence, version discovery | Server startup |
| Config | `t.Setenv` | Required var validation, defaults, parsing | Business logic |
| Integration | `envtest` + `httptest` (NO mocks) | Full wiring, route mounting, HTTP round-trips | Individual edge cases (covered by unit tests) |

---

## Structural Requirements (Verified Without Behavioral Tests)

These requirements are validated by compilation, code review, or static analysis -- not by Given/When/Then tests.

| Requirement | How Verified |
|---|---|
| REQ-HTTP-010 (configurable bind address) | Server starts on configured `SP_SERVER_ADDRESS` |
| REQ-API-010 (compile-time interface check) | `var _ server.StrictServerInterface = (*Handler)(nil)` in handler.go |
| REQ-API-011 (Handler depends on ClusterService) | Constructor signature accepts `service.ClusterService`; `depguard` lint prevents K8s imports |
| REQ-API-012 (Handler depends on HealthChecker) | Constructor signature accepts `service.HealthChecker` |
| REQ-XC-K8S-010 (K8s client usage) | `go.mod` includes `sigs.k8s.io/controller-runtime` |
| REQ-XC-LOG-010 (structured logging) | Code review; linter for `slog`/`logr` usage |
| REQ-XC-LOG-020 (request log fields) | Code review; middleware integration verifies presence |
| REQ-XC-LOG-030 (K8s operation log fields) | Code review |
| REQ-API-175 (`metadata.name` format via OpenAPI) | OpenAPI spec `pattern` + validation middleware (REQ-HTTP-090) |
| REQ-XC-K8S-030 (context-based timeouts) | Code review (all K8s calls use `ctx`) |

---

## Test Cases

### 2.1 Registration (TC-REG-UT-xxx)

#### TC-REG-UT-001: Successful first-time registration with capabilities
- **Requirements:** REQ-REG-010, REQ-REG-020, REQ-REG-080, REQ-REG-090
- **Type:** Unit
- **Priority:** High
- **Given** a mock DCM Registry server that returns 201 with a generated provider ID
- **And** a fake K8s client with ClusterImageSet resources for OCP versions "4.16.0" and "4.17.3"
- **When** the registration component executes
- **Then** a `POST {DCM_REGISTRATION_URL}/providers` request is sent to the registry
- **And** the payload contains `service_type="cluster"`, `endpoint`, `operations` at top level; `display_name` is included when `SP_DISPLAY_NAME` is configured
- **And** ACM-specific metadata includes `supportedPlatforms=["kubevirt","baremetal"]`, `supportedProvisioningTypes=["hypershift"]`, `kubernetesSupportedVersions=["1.29","1.30"]`; `metadata.region` and `metadata.zone` included when configured

#### TC-REG-UT-009: Registry returns non-2xx server errors
- **Requirements:** REQ-REG-040, REQ-REG-050, REQ-REG-051
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock DCM Registry that returns HTTP 500 on every request
- **When** the registration component executes
- **Then** the SP retries with exponential backoff up to a maximum backoff interval
- **And** failures are logged with sufficient detail
- **And** the SP continues serving requests (does not exit)
- **Given** a mock DCM Registry that returns HTTP 400 (bad request)
- **When** the registration component executes
- **Then** the SP does NOT retry (client error indicates a payload problem, not transience)
- **And** the error is logged and the SP continues operating

#### TC-REG-UT-002: Idempotent re-registration
- **Requirements:** REQ-REG-060
- **Type:** Unit
- **Priority:** High
- **Given** a mock DCM Registry that returns 200 (existing provider) with the same provider ID
- **When** the registration component executes
- **Then** the SP proceeds normally (provider ID is not persisted per DEC-003)
- **And** no error is raised

#### TC-REG-UT-003: Registry unreachable with infinite retry
- **Requirements:** REQ-REG-040, REQ-REG-050, REQ-REG-051
- **Type:** Unit
- **Priority:** High
- **Given** a mock DCM Registry that always returns connection errors
- **When** the registration component executes
- **Then** retries are made indefinitely with exponential backoff until context cancellation
- **And** failures are logged with sufficient detail for diagnosis
- **And** the SP continues running and serving requests (does not exit)

#### TC-REG-UT-004: Non-retryable 4xx error stops retries immediately
- **Requirements:** REQ-REG-040, REQ-REG-051
- **Type:** Unit
- **Priority:** High
- **Given** a mock DCM Registry that returns HTTP 400
- **When** the registration component executes
- **Then** exactly 1 request is sent (no retries on 4xx)
- **And** a non-retryable error is logged
- **And** the SP continues serving requests normally

#### TC-REG-UT-010b: Calling Start() twice does not launch duplicate goroutines
- **Requirements:** REQ-REG-030
- **Type:** Unit
- **Priority:** Medium
- **Given** a Registrar instance
- **When** `Start()` is called twice
- **Then** only one background goroutine is launched (verified via request count)

#### TC-REG-UT-005: Version refresh triggers re-registration
- **Requirements:** REQ-REG-100, REQ-REG-110
- **Type:** Unit
- **Priority:** Medium
- **Given** the SP is registered with `kubernetesSupportedVersions=["1.29","1.30"]`
- **And** a version watcher is configured with a short check interval
- **When** a new ClusterImageSet for OCP "4.18.0" (K8s "1.31") appears in the fake K8s client
- **And** the check interval elapses
- **Then** the registration component re-registers with updated `kubernetesSupportedVersions=["1.29","1.30","1.31"]`

#### TC-REG-UT-007: No ClusterImageSets at startup
- **Requirements:** REQ-REG-090
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with no ClusterImageSet resources
- **And** a mock DCM Registry that accepts registration
- **When** the registration component executes
- **Then** the SP registers with `kubernetesSupportedVersions=[]` (empty list)
- **And** registration succeeds without error

#### TC-REG-UT-008: ClusterImageSet deletion triggers re-registration
- **Requirements:** REQ-REG-100, REQ-REG-110
- **Type:** Unit
- **Priority:** Medium
- **Given** the SP is registered with `kubernetesSupportedVersions=["1.29","1.30","1.31"]`
- **And** the ClusterImageSet for OCP "4.18.0" (K8s "1.31") is deleted from the fake K8s client
- **When** the version check interval elapses
- **Then** the SP re-registers with `kubernetesSupportedVersions=["1.29","1.30"]`

#### TC-REG-UT-010: Version check detects changes but re-registration fails
- **Requirements:** REQ-REG-110, REQ-REG-040
- **Type:** Unit
- **Priority:** Medium
- **Given** the SP is registered with `kubernetesSupportedVersions=["1.29","1.30"]`
- **And** a new ClusterImageSet for OCP "4.18.0" (K8s "1.31") appears in the fake K8s client
- **And** the mock DCM Registry returns HTTP 500 on the re-registration POST
- **When** the version check interval elapses
- **Then** the SP retries re-registration
- **And** logs the error
- **And** continues operating with the stale versions (does not terminate)

#### TC-REG-UT-011: Registration uses DCM client library
- **Requirements:** REQ-REG-070
- **Type:** Unit
- **Priority:** Medium
- **Given** the registration component is initialized
- **When** it communicates with the DCM SP Registry
- **Then** it uses the official DCM SP API client library
- **And** does not make raw HTTP calls to the registry

#### TC-REG-UT-012: ClusterImageSet without matrix mapping is not advertised
- **Requirements:** REQ-REG-091
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with ClusterImageSet resources for OCP "4.16.0", "4.17.0", and "4.19.0"
- **And** the compatibility matrix only covers OCP 4.14 through 4.18
- **When** the registration component executes
- **Then** `kubernetesSupportedVersions` includes "1.29" (from 4.16) and "1.30" (from 4.17)
- **And** `kubernetesSupportedVersions` does NOT include any version for OCP "4.19" (no matrix entry)

#### TC-REG-UT-013: Matrix entry without matching ClusterImageSet is not advertised
- **Requirements:** REQ-REG-091
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with ClusterImageSet resources for OCP "4.16.0" only
- **And** the compatibility matrix covers OCP 4.14 through 4.18 (K8s 1.27-1.31)
- **When** the registration component executes
- **Then** `kubernetesSupportedVersions=["1.29"]` (only 4.16 has a ClusterImageSet)
- **And** K8s versions "1.27", "1.28", "1.30", "1.31" are NOT advertised (no ClusterImageSet for their OCP equivalents)

---

### 2.2 HTTP Server (TC-HTTP-UT-xxx)

#### TC-HTTP-UT-006: Configurable bind address
- **Requirements:** REQ-HTTP-010
- **Type:** Unit
- **Priority:** Low
- **Given** `SP_SERVER_ADDRESS` is set to `127.0.0.1:9090`
- **When** the server starts
- **Then** it listens on `127.0.0.1:9090`

#### TC-HTTP-UT-012: Server sets http.Server timeouts from config
- **Requirements:** REQ-HTTP-050
- **Type:** Unit
- **Priority:** Medium
- **Given** `SP_SERVER_READ_TIMEOUT`, `SP_SERVER_WRITE_TIMEOUT`, `SP_SERVER_IDLE_TIMEOUT` are configured
- **When** the server is created via `New()`
- **Then** the underlying `http.Server` has `ReadTimeout`, `WriteTimeout`, `IdleTimeout` set to the configured values

---

### 2.3 Health Service (TC-HLT-UT-xxx)

#### TC-HLT-UT-001: Healthy response with all checks passing
- **Requirements:** REQ-HLT-010, REQ-HLT-020, REQ-HLT-030, REQ-HLT-040, REQ-HLT-050, REQ-HLT-120
- **Type:** Unit
- **Priority:** High
- **Given** a `HealthChecker` implementation with a fake K8s client where API connectivity succeeds and the HostedCluster CRD exists
- **When** `Check(ctx)` is called
- **Then** the returned `Health` has `status="healthy"`, `type="acm-cluster-service-provider.dcm.io/health"`, `path="health"`
- **And** `version` field is present and non-empty
- **And** `uptime` field is present and >= 0

#### TC-HLT-UT-002: Unhealthy when K8s API unreachable (critical dependency)
- **Requirements:** REQ-HLT-060, REQ-HLT-070
- **Type:** Unit
- **Priority:** High
- **Given** a `HealthChecker` implementation where K8s API connectivity check fails (connection refused)
- **When** `Check(ctx)` is called
- **Then** the returned `Health` has `status="unhealthy"`
- **And** `type` and `path` are still correctly set

#### TC-HLT-UT-003: Unhealthy when HyperShift CRD missing (critical dependency)
- **Requirements:** REQ-HLT-060, REQ-HLT-080
- **Type:** Unit
- **Priority:** High
- **Given** a `HealthChecker` where K8s API is reachable but HostedCluster CRD does not exist
- **When** `Check(ctx)` is called
- **Then** the returned `Health` has `status="unhealthy"`

#### TC-HLT-UT-004: Timeout produces unhealthy
- **Requirements:** REQ-HLT-110
- **Type:** Unit
- **Priority:** Medium
- **Given** a `HealthChecker` where dependency checks take >5s (simulated)
- **When** `Check(ctx)` is called with a 5s context deadline
- **Then** the response is returned within 5 seconds
- **And** `status="unhealthy"`

#### TC-HLT-UT-005: KubeVirt infrastructure check
- **Requirements:** REQ-HLT-090
- **Type:** Unit
- **Priority:** Low
- **Given** KubeVirt platform is enabled
- **And** KubeVirt infrastructure is not accessible (fake client returns error for KubeVirt CRDs)
- **When** `Check(ctx)` is called
- **Then** `status="unhealthy"`

#### TC-HLT-UT-006: BareMetal infrastructure check
- **Requirements:** REQ-HLT-100
- **Type:** Unit
- **Priority:** Low
- **Given** BareMetal platform is enabled
- **And** Agent/CIM resources are unavailable
- **When** `Check(ctx)` is called
- **Then** `status="unhealthy"`

#### TC-HLT-UT-007: Uptime increases over time
- **Requirements:** REQ-HLT-020
- **Type:** Unit
- **Priority:** Medium
- **Given** the SP has been running for some time
- **When** `Check(ctx)` is called at time T1
- **And** `Check(ctx)` is called again at time T2 > T1
- **Then** the `uptime` value at T2 is greater than the `uptime` value at T1

---

### 2.4 Handler Layer -- Create Cluster (TC-HDL-CRT-UT-xxx)

These test the handler (`StrictServerInterface` implementation) with a mock `ClusterService`. The handler is responsible for input validation, read-only field stripping, id generation, and HTTP response code selection.

#### TC-HDL-CRT-UT-001: Create with server-generated ID
- **Requirements:** REQ-API-060, REQ-API-100, REQ-API-110
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Create` that succeeds and returns the cluster
- **When** `CreateCluster` is called with a valid body (service_type="cluster", version, nodes with control_plane and workers, metadata.name) and no `?id=` param
- **Then** the handler generates a UUID for `id`
- **And** delegates to `ClusterService.Create(ctx, generatedId, cluster)`
- **And** returns 201 with `id`, `path="clusters/<id>"`, `status="PENDING"`, `create_time`, `update_time`

#### TC-HDL-CRT-UT-002: Create with client-specified ID
- **Requirements:** REQ-API-100, REQ-API-104
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Create` that succeeds
- **When** `CreateCluster` is called with `?id=my-custom-id` and valid body
- **Then** the handler passes `id="my-custom-id"` to `ClusterService.Create`
- **And** returns 201 with `id="my-custom-id"`, `path="clusters/my-custom-id"`

#### TC-HDL-CRT-UT-003: Read-only fields in body are ignored
- **Requirements:** REQ-API-101, REQ-API-104, REQ-API-160
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Create` that succeeds
- **When** `CreateCluster` is called with body containing `id="custom"`, `status="READY"`, `api_endpoint="https://fake"`, `create_time`, `update_time`, `path="clusters/fake"`, `status_message="test"`, `console_uri="https://fake"`, `kubeconfig="abc"`
- **Then** the handler ignores all read-only body fields
- **And** returns 201 with server-generated `id`, `status="PENDING"`, no `api_endpoint`, server-set `create_time` and `path`

#### TC-HDL-CRT-UT-004: Invalid service_type
- **Requirements:** REQ-API-090
- **Type:** Unit
- **Priority:** High
- **Given** no mock interaction needed (validation fails before delegation)
- **When** `CreateCluster` is called with `service_type="compute"`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-005: Missing workers
- **Requirements:** REQ-API-080
- **Type:** Unit
- **Priority:** High
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with `nodes.control_plane` present but no `nodes.workers`
- **Then** returns 400 with `type="INVALID_ARGUMENT"` and detail mentioning workers are required

#### TC-HDL-CRT-UT-006: Invalid memory format
- **Requirements:** REQ-API-170
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with `nodes.control_plane.memory="16Gi"` (K8s format, not DCM)
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-007: Duplicate ID error from service
- **Requirements:** REQ-API-102
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Create` that returns an `AlreadyExists` domain error for the given ID
- **When** `CreateCluster` is called with `?id=abc123`
- **Then** returns 409 with `type="ALREADY_EXISTS"`

#### TC-HDL-CRT-UT-008: Duplicate metadata.name error from service
- **Requirements:** REQ-API-103
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Create` that returns an `AlreadyExists` domain error with name conflict detail
- **When** `CreateCluster` is called with `metadata.name="my-cluster"`
- **Then** returns 409 with `type="ALREADY_EXISTS"` and detail indicating the name conflict

#### TC-HDL-CRT-UT-009: Unsupported platform error from service
- **Requirements:** REQ-API-130
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.Create` that returns an `UnprocessableEntity` domain error
- **When** `CreateCluster` is called with `platform="aws"`
- **Then** returns 422 with `type="UNPROCESSABLE_ENTITY"`

#### TC-HDL-CRT-UT-010: Version not found error from service
- **Requirements:** REQ-API-140
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.Create` that returns an `UnprocessableEntity` domain error for version
- **When** `CreateCluster` is called with `version="9.99"`
- **Then** returns 422 with `type="UNPROCESSABLE_ENTITY"`

#### TC-HDL-CRT-UT-011: Missing required fields (version, control_plane)
- **Requirements:** REQ-API-070, REQ-API-150
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with body missing `version`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`
- **When** `CreateCluster` is called with `nodes.workers` but no `nodes.control_plane`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-019: Empty `?id=` query parameter treated as absent
- **Requirements:** REQ-API-100
- **Type:** Unit
- **Priority:** Low
- **Given** a mock `ClusterService.Create` that succeeds
- **When** `POST /api/v1alpha1/clusters?id=` is sent with a valid body (empty string `?id=` parameter)
- **Then** response is 201
- **And** body contains a server-generated `id` (valid UUID), not an empty string

#### TC-HDL-CRT-UT-012: Internal error does not leak details
- **Requirements:** REQ-API-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.Create` that returns a wrapped K8s API error containing "etcd leader changed: 10.0.0.5"
- **When** `CreateCluster` is called with a valid body
- **Then** returns 500 with `type="INTERNAL"`
- **And** response body does NOT contain K8s-specific error text

#### TC-HDL-CRT-UT-013: Missing nodes object entirely
- **Requirements:** REQ-API-070, REQ-API-150
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with version, service_type, metadata but no `nodes` field
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-014: Workers count below minimum
- **Requirements:** REQ-API-080
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with `nodes.workers.count=0`
- **Then** returns 400 with `type="INVALID_ARGUMENT"` and detail indicating workers count must be >= 1

#### TC-HDL-CRT-UT-015: Invalid client-specified ?id= format
- **Requirements:** REQ-API-100, REQ-API-150
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with `?id=INVALID_ID!` (uppercase, special characters)
- **Then** returns 400 with `type="INVALID_ARGUMENT"`
- **When** `CreateCluster` is called with `?id=` followed by a 64-character string
- **Then** returns 400 with `type="INVALID_ARGUMENT"` (exceeds 63-char K8s limit)

#### TC-HDL-CRT-UT-016: Missing service_type field
- **Requirements:** REQ-API-090, REQ-API-150
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock interaction needed
- **When** `CreateCluster` is called with valid body but missing `service_type` field
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-017: Zero-value memory rejected by middleware
- **Requirements:** REQ-API-170
- **Type:** Integration (middleware)
- **Priority:** High
- **Reclassified:** Moved from unit to integration scope. The OpenAPI spec defines `pattern: '^[1-9][0-9]*(MB|GB|TB)$'` on memory fields, which rejects `"0GB"` at the validation middleware before the request reaches the handler. This TC must be tested through the full HTTP middleware stack.
- **Given** no mock interaction needed (middleware rejects before handler)
- **When** `POST /api/v1alpha1/clusters` is sent with `workers.memory="0GB"`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-CRT-UT-018: Zero-value storage rejected by middleware
- **Requirements:** REQ-API-170
- **Type:** Integration (middleware)
- **Priority:** Medium
- **Reclassified:** Moved from unit to integration scope. The OpenAPI spec defines `pattern: '^[1-9][0-9]*(MB|GB|TB)$'` on storage fields, which rejects `"0TB"` at the validation middleware before the request reaches the handler. This TC must be tested through the full HTTP middleware stack.
- **Given** no mock interaction needed (middleware rejects before handler)
- **When** `POST /api/v1alpha1/clusters` is sent with `control_plane.storage="0TB"`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

---

### 2.5 Handler Layer -- Get Cluster (TC-HDL-GET-UT-xxx)

#### TC-HDL-GET-UT-001: Get existing cluster (READY)
- **Requirements:** REQ-API-180, REQ-API-190, REQ-API-220
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Get` that returns a cluster with `status=READY`, populated `api_endpoint`, `console_uri`, `kubeconfig`
- **When** `GetCluster` is called for `id="my-cluster-id"`
- **Then** returns 200 with all fields populated

#### TC-HDL-GET-UT-002: Get existing cluster (non-READY, non-UNAVAILABLE)
- **Requirements:** REQ-API-190, REQ-API-230
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Get` returns cluster with `status=PROVISIONING`
- **When** `GetCluster` is called
- **Then** returns 200 with `api_endpoint`, `console_uri`, `kubeconfig` absent/empty

#### TC-HDL-GET-UT-005: Get existing cluster (UNAVAILABLE with credentials)
- **Requirements:** REQ-API-190, REQ-API-231
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Get` returns cluster with `status=UNAVAILABLE`, populated `api_endpoint`, `console_uri`, `kubeconfig`
- **When** `GetCluster` is called
- **Then** returns 200 with `api_endpoint`, `console_uri`, `kubeconfig` populated

#### TC-HDL-GET-UT-003: Get non-existent cluster
- **Requirements:** REQ-API-200
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Get` that returns a `NotFound` domain error
- **When** `GetCluster` is called for `id="nonexistent-id"`
- **Then** returns 404 with `type="NOT_FOUND"`

#### TC-HDL-GET-UT-004: clusterId format validation
- **Requirements:** REQ-API-210
- **Type:** Integration (middleware)
- **Priority:** Medium
- **Reclassified:** Moved from unit to integration scope. The OpenAPI spec defines a `pattern` on `ClusterIdPath` (`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`), enforced by the validation middleware before the request reaches the handler. The generated `StrictServerInterface` has no `GetCluster400` response type, confirming the handler cannot return 400. This TC must be tested through the full HTTP middleware stack.
- **Given** no mock needed (validation at middleware layer)
- **When** `GET /api/v1alpha1/clusters/INVALID_ID!` is sent (uppercase/special chars)
- **Then** returns 400 with `type="INVALID_ARGUMENT"`
- **When** `GET /api/v1alpha1/clusters/<64-char-id>` is sent
- **Then** returns 400 with `type="INVALID_ARGUMENT"`
- **When** `GET /api/v1alpha1/clusters/-starts-with-hyphen` is sent
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

---

### 2.6 Handler Layer -- List Clusters (TC-HDL-LST-UT-xxx)

#### TC-HDL-LST-UT-001: Default pagination
- **Requirements:** REQ-API-240, REQ-API-250, REQ-API-260, REQ-API-310
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.List` that returns 50 clusters and a `next_page_token`
- **When** `ListClusters` is called with no parameters
- **Then** returns 200 with 50 results and `next_page_token` populated
- **And** the mock was called with `pageSize=50` (default)

#### TC-HDL-LST-UT-002: max_page_size exceeds maximum
- **Requirements:** REQ-API-270
- **Type:** Unit
- **Priority:** High
- **Given** no mock needed
- **When** `ListClusters` is called with `max_page_size=200`
- **Then** returns 400 with `type="INVALID_ARGUMENT"` and detail mentioning allowed range 1-100

#### TC-HDL-LST-UT-003: max_page_size below minimum
- **Requirements:** REQ-API-280
- **Type:** Unit
- **Priority:** Medium
- **Given** no mock needed
- **When** `ListClusters` is called with `max_page_size=0`
- **Then** returns 400 with `type="INVALID_ARGUMENT"` and detail mentioning minimum value is 1
- **When** `ListClusters` is called with `max_page_size=-5`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-LST-UT-004: Invalid page_token
- **Requirements:** REQ-API-290
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.List` that returns an `InvalidArgument` domain error for bad token
- **When** `ListClusters` is called with `page_token="garbage-token"`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-LST-UT-005: Last page has no next_page_token
- **Requirements:** REQ-API-300
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.List` returns 10 results with no `next_page_token`
- **When** `ListClusters` is called
- **Then** returns 200 with `next_page_token` absent or empty

#### TC-HDL-LST-UT-006: Empty collection returns empty list
- **Requirements:** REQ-API-240, REQ-API-300
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.List` returns 0 results and no `next_page_token`
- **When** `ListClusters` is called with no parameters
- **Then** returns 200 with `clusters=[]` and `next_page_token` absent

#### TC-HDL-LST-UT-007: max_page_size boundary values
- **Requirements:** REQ-API-270, REQ-API-280
- **Type:** Unit
- **Priority:** Low
- **Given** a mock `ClusterService.List` returns results
- **When** `ListClusters` is called with `max_page_size=1`
- **Then** returns 200 (1 is valid minimum)
- **When** `ListClusters` is called with `max_page_size=100`
- **Then** returns 200 (100 is valid maximum)
- **When** `ListClusters` is called with `max_page_size=101`
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

#### TC-HDL-LST-UT-008: Empty page_token treated as absent
- **Requirements:** REQ-API-290
- **Type:** Unit
- **Priority:** Low
- **Given** a mock `ClusterService.List` returns results normally for the first page
- **When** `ListClusters` is called with `page_token=""`
- **Then** returns 200 with the first page of results (empty string is not treated as invalid token)

---

### 2.7 Handler Layer -- Delete Cluster (TC-HDL-DEL-UT-xxx)

#### TC-HDL-DEL-UT-001: Successful deletion
- **Requirements:** REQ-API-320, REQ-API-330, REQ-API-350
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Delete` that succeeds
- **When** `DeleteCluster` is called for `id="my-cluster-id"`
- **Then** returns 204 with empty body
- **And** the mock confirms the call was delegated

#### TC-HDL-DEL-UT-002: Delete non-existent cluster
- **Requirements:** REQ-API-340
- **Type:** Unit
- **Priority:** High
- **Given** a mock `ClusterService.Delete` that returns a `NotFound` domain error
- **When** `DeleteCluster` is called for `id="nonexistent-id"`
- **Then** returns 404 with `type="NOT_FOUND"`

#### TC-HDL-DEL-UT-003: Delete already-deleted cluster
- **Requirements:** REQ-API-360
- **Type:** Unit
- **Priority:** Low
- **Given** a mock `ClusterService.Delete` that returns `NotFound` (resource no longer exists)
- **When** `DeleteCluster` is called
- **Then** returns 404

#### TC-HDL-DEL-UT-004: Delete cluster in DELETING state is idempotent
- **Requirements:** REQ-API-370
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.Delete` that succeeds (idempotent for DELETING resources)
- **When** `DeleteCluster` is called for a cluster already being deleted
- **Then** returns 204

#### TC-HDL-DEL-UT-005: Get during deletion returns DELETING
- **Requirements:** REQ-API-370
- **Type:** Unit
- **Priority:** Medium
- **Given** a mock `ClusterService.Get` returns cluster with `status=DELETING`
- **When** `GetCluster` is called for that cluster
- **Then** returns 200 with `status="DELETING"`

#### TC-HDL-DEL-UT-006: clusterId format validation
- **Requirements:** REQ-API-210
- **Type:** Integration (middleware)
- **Priority:** Medium
- **Reclassified:** Moved from unit to integration scope. The OpenAPI spec defines a `pattern` on `ClusterIdPath` (`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`), enforced by the validation middleware before the request reaches the handler. The generated `StrictServerInterface` has no `DeleteCluster400` response type, confirming the handler cannot return 400. This TC must be tested through the full HTTP middleware stack.
- **Given** no mock needed (validation at middleware layer)
- **When** `DELETE /api/v1alpha1/clusters/INVALID_ID!` is sent (uppercase/special chars)
- **Then** returns 400 with `type="INVALID_ARGUMENT"`
- **When** `DELETE /api/v1alpha1/clusters/<64-char-id>` is sent
- **Then** returns 400 with `type="INVALID_ARGUMENT"`

---

### 2.8 Error Mapping (TC-ERR-UT-xxx)

Tests the shared domain-error-to-RFC-7807 mapping used by all handlers.

#### TC-ERR-UT-001: Error type to HTTP status mapping (table-driven)
- **Requirements:** REQ-API-020, REQ-API-030, REQ-API-040, REQ-XC-ERR-010, REQ-XC-ERR-020
- **Type:** Unit
- **Priority:** High
- **Given** a set of domain error types
- **When** each error is converted to an RFC 7807 response
- **Then** the mapping is: `INVALID_ARGUMENT` -> 400, `NOT_FOUND` -> 404, `ALREADY_EXISTS` -> 409, `UNPROCESSABLE_ENTITY` -> 422, `INTERNAL` -> 500, `UNAVAILABLE` -> 503
- **And** Content-Type is `application/problem+json`
- **And** `type`, `title`, `status` are all present in each response

#### TC-ERR-UT-002: Internal errors do not leak implementation details
- **Requirements:** REQ-API-050, REQ-XC-ERR-040
- **Type:** Unit
- **Priority:** High
- **Given** an internal domain error wrapping a K8s API error message "etcd leader changed: 10.0.0.5"
- **When** converted to an RFC 7807 response
- **Then** the `detail` field does NOT contain the original K8s error text
- **And** `type="INTERNAL"` and a generic `title` is used

#### TC-ERR-UT-003: Error response includes optional tracing fields
- **Requirements:** REQ-XC-ERR-030
- **Type:** Unit
- **Priority:** Low
- **Given** a domain error with detail and instance context
- **When** converted to an RFC 7807 response
- **Then** `detail` and `instance` fields are present in the response

---

### 2.9 Status Mapping -- SHARED Component (TC-STS-UT-xxx)

Tests the **shared** `MapConditionsToStatus()` function that maps HostedCluster conditions to DCM status. Used identically by KubeVirt and BareMetal services. This single test suite satisfies REQ-ACM-110 (condition-to-status mapping) and REQ-ACM-160 (status condition precedence) and all per-platform status acceptance criteria.

All tests in this section are **table-driven** within a single Go test function.

#### TC-STS-UT-001: PENDING -- Progressing=Unknown
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Progressing=Unknown`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `PENDING`

#### TC-STS-UT-002: PROVISIONING -- Progressing=True, Available=False
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Progressing=True`, `Available=False`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `PROVISIONING`

#### TC-STS-UT-003: READY -- Available=True, Progressing=False
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Available=True`, `Progressing=False`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `READY`

#### TC-STS-UT-004: FAILED -- Degraded=True
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Degraded=True`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `FAILED`

#### TC-STS-UT-005: DELETING -- deletionTimestamp set
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** `deletionTimestamp` set, any conditions present
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `DELETING`

#### TC-STS-UT-006: Precedence -- Degraded wins over Available
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Degraded=True` AND `Available=True`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `FAILED` (Degraded wins over Available)

#### TC-STS-UT-007: Precedence -- deletionTimestamp overrides all conditions
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Available=True`, `Progressing=False`, `Degraded=True` AND `deletionTimestamp` is set
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `DELETING` (deletionTimestamp has highest precedence)

#### TC-STS-UT-008: No conditions -- defaults to PENDING
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** Medium
- **Given** no conditions set (empty conditions list), no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `PENDING`

#### TC-STS-UT-009: PROVISIONING -- Progressing=True with no Available condition
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** Medium
- **Given** conditions: `Progressing=True`, no `Available` condition (absent, not False), no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `PROVISIONING`

#### TC-STS-UT-010: READY -- Available=True with no Progressing condition
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** Medium
- **Given** conditions: `Available=True`, no `Progressing` condition (absent), no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `READY`

#### TC-STS-UT-011: UNAVAILABLE -- Available=False, Progressing=False
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Available=False`, `Progressing=False`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `UNAVAILABLE`

#### TC-STS-UT-012: Precedence -- Degraded=True wins over Available=False, Progressing=False (FAILED over UNAVAILABLE)
- **Requirements:** REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** conditions: `Degraded=True`, `Available=False`, `Progressing=False`, no `deletionTimestamp`
- **When** `MapConditionsToStatus()` is called
- **Then** returned DCM status is `FAILED` (Degraded wins; not UNAVAILABLE)

---

### 2.10 KubeVirt Service (TC-KV-UT-xxx)

Tests the KubeVirt `ClusterService` implementation with a fake K8s `client.Client`. Status mapping is NOT retested here (covered by TC-STS-UT-xxx). Each test verifies CRD construction, K8s interactions, and data extraction.

#### TC-KV-UT-001: Create KubeVirt cluster -- HostedCluster + NodePool
- **Requirements:** REQ-ACM-010, REQ-KV-010, REQ-KV-020, REQ-ACM-020, REQ-ACM-090, REQ-KV-030, REQ-ACM-100, REQ-XC-LBL-010, REQ-XC-LBL-020
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a ClusterImageSet for OCP "4.15.2" (K8s "1.28") and `SP_CLUSTER_NAMESPACE="clusters"`
- **When** `Create(ctx, "test-id", cluster)` is called with `platform="kubevirt"`, `version="1.28"`, `metadata.name="my-cluster"`, `workers={count:2, cpu:4, memory:"16GB", storage:"120GB"}`
- **Then** a HostedCluster named "my-cluster" is created in namespace "clusters" with `platform.type=KubeVirt`
- **And** a NodePool is created in namespace "clusters" with `replicas=2`
- **And** NodePool worker VM template has resources matching cpu=4, memory=16GB equivalent, storage=120GB equivalent
- **And** both carry labels `managed-by=dcm`, `dcm-instance-id=test-id`, `dcm-service-type=cluster`

#### TC-KV-UT-002: control_plane.count and storage are ignored
- **Requirements:** REQ-ACM-070, REQ-ACM-080
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `control_plane.count=5, control_plane.storage="500GB"`
- **Then** the HostedCluster does not contain an explicit control plane replica count
- **And** etcd storage config is not set (HyperShift manages it)

#### TC-KV-UT-003: control_plane CPU and memory map to resource request override annotations
- **Requirements:** REQ-ACM-060, REQ-ACM-061
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `control_plane.cpu=4, control_plane.memory="16GB"`
- **Then** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/kube-apiserver.kube-apiserver` with value `cpu=4,memory=16G`
- **And** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/etcd.etcd` with value `cpu=4,memory=16G`
- **And** no other `resource-request-override.hypershift.openshift.io` annotations are present

#### TC-KV-UT-004: Version validated via compatibility matrix and ClusterImageSets (table-driven)
- **Requirements:** REQ-ACM-030, REQ-ACM-031, REQ-ACM-032, REQ-ACM-040
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with ClusterImageSets for OCP "4.15.2" and "4.17.0"
- **Sub-case 1:** `version="1.28"` → matrix maps to OCP 4.15 → ClusterImageSet for "4.15.2" exists → **success**
- **Sub-case 2:** `version="9.99"` → no K8s-to-OCP mapping in compatibility matrix → **error (422)** (REQ-ACM-031)
- **Sub-case 3:** `version="1.3"` → partial K8s version, no exact match → **error** ("1.30" requires full minor)
- **Sub-case 4:** `version="4.17.0"` → OCP format, no matrix entry for this format → **error (422)** (REQ-ACM-031)
- **Sub-case 5:** `version="1.30"` → matrix maps to OCP 4.17 → no ClusterImageSet for OCP 4.17 minor without patch → **success** (ClusterImageSet "4.17.0" exists)
- **Sub-case 6:** `version="1.31"` → matrix maps to OCP 4.18 → no ClusterImageSet for 4.18 → **error (422)** (REQ-ACM-032)

#### TC-KV-UT-005: Release image override bypasses ClusterImageSet lookup
- **Requirements:** REQ-ACM-050, REQ-ACM-051
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with NO matching ClusterImageSet for the requested version
- **When** `Create()` is called with `provider_hints.acm.release_image="quay.io/ocp-release:4.15.2-x86_64"`
- **Then** the HostedCluster uses the specified release image directly
- **And** no ClusterImageSet lookup error occurs

#### TC-KV-UT-006: Base domain from request and default config
- **Requirements:** REQ-API-380
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **And** `SP_BASE_DOMAIN="cluster.local"` is configured
- **When** `Create()` is called with `provider_hints.acm.base_domain="example.com"`
- **Then** the HostedCluster's base domain is set to `"example.com"` (request overrides default)
- **When** `Create()` is called WITHOUT `provider_hints.acm.base_domain`
- **Then** the HostedCluster's base domain is set to `"cluster.local"` (falls back to configured default)

#### TC-KV-UT-007: Default platform is KubeVirt
- **Requirements:** (implied by AC-KV-060)
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called WITHOUT `provider_hints` (no platform specified)
- **Then** the HostedCluster is created with `platform.type=KubeVirt`

#### TC-KV-UT-008: Memory/storage format conversion (DCM to K8s)
- **Requirements:** REQ-ACM-150
- **Type:** Unit
- **Priority:** High
- **Given** various DCM format values: `"16GB"`, `"512MB"`, `"2TB"`
- **When** each is converted and applied to the NodePool VM template
- **Then** the resulting K8s resource quantities are equivalent (e.g., "16G", "512M", "2T")

#### TC-KV-UT-009: Workers storage maps to root disk size
- **Requirements:** REQ-KV-040
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `workers.storage="120GB"`
- **Then** the NodePool VM template root disk size is set to the K8s equivalent of 120GB

#### TC-KV-UT-010: Get cluster -- READY with api_endpoint and kubeconfig
- **Requirements:** REQ-ACM-110, REQ-ACM-120, REQ-ACM-130, REQ-API-390
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with:
  - A HostedCluster with `dcm-instance-id=test-id`, `Available=True`, `Progressing=False`, `status.controlPlaneEndpoint.host="api.cluster.example.com"`, `port=6443`
  - A kubeconfig Secret named `<cluster-name>-admin-kubeconfig` with `kubeconfig` key (naming convention derived from HyperShift behavior, not DCM-defined)
- **When** `Get(ctx, "test-id")` is called
- **Then** the returned cluster has `status=READY`
- **And** `api_endpoint="https://api.cluster.example.com:6443"`
- **And** `console_uri="https://console-openshift-console.apps.<cluster-name>.<base_domain>"`
- **And** `kubeconfig` is the base64-encoded content from the Secret's `kubeconfig` key

#### TC-KV-UT-011: Get cluster -- PROVISIONING, no credentials
- **Requirements:** REQ-ACM-110
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a HostedCluster (`Progressing=True`, `Available=False`)
- **When** `Get(ctx, "test-id")` is called
- **Then** `api_endpoint`, `console_uri`, `kubeconfig` are empty/absent

#### TC-KV-UT-012: Get cluster -- not found
- **Requirements:** (service-layer enforcement for handler's REQ-API-200)
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with no matching HostedCluster (no `dcm-instance-id=nonexistent` label)
- **When** `Get(ctx, "nonexistent")` is called
- **Then** a `NotFound` domain error is returned

#### TC-KV-UT-013: Delete cluster
- **Requirements:** REQ-ACM-140
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a HostedCluster with `dcm-instance-id=test-id`
- **When** `Delete(ctx, "test-id")` is called
- **Then** the HostedCluster is deleted from the fake client

#### TC-KV-UT-014: Duplicate ID conflict
- **Requirements:** REQ-API-102 (service-layer enforcement)
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with an existing HostedCluster carrying label `dcm-instance-id=abc123`
- **When** `Create(ctx, "abc123", cluster)` is called
- **Then** an `AlreadyExists` domain error is returned

#### TC-KV-UT-015: Duplicate metadata.name conflict
- **Requirements:** REQ-API-103 (service-layer enforcement)
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with an existing HostedCluster named `my-cluster` in the target namespace
- **When** `Create(ctx, "new-id", cluster)` is called with `metadata.name="my-cluster"`
- **Then** an `AlreadyExists` domain error with name conflict detail is returned

#### TC-KV-UT-016: K8s API failure during create returns internal error
- **Requirements:** REQ-ACM-010 (error path)
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that returns an error on HostedCluster creation
- **When** `Create(ctx, "test-id", cluster)` is called
- **Then** an `Internal` domain error is returned
- **And** the original K8s error message is not exposed in the domain error detail

#### TC-KV-UT-017: NodePool creation failure triggers HostedCluster rollback
- **Requirements:** REQ-ACM-170
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client that succeeds on HostedCluster creation but fails on NodePool creation
- **When** `Create(ctx, "test-id", cluster)` is called
- **Then** the SP deletes the orphaned HostedCluster
- **And** an error is returned to the caller
- **And** no HostedCluster remains in the fake client

#### TC-KV-UT-028: Rollback failure during partial create (double failure)
- **Requirements:** REQ-ACM-170
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that succeeds on HostedCluster creation, fails on NodePool creation, AND fails on HostedCluster deletion (rollback)
- **When** `Create(ctx, "test-id", cluster)` is called
- **Then** an error is returned to the caller containing both the NodePool creation failure and the rollback failure
- **And** the error is logged with sufficient detail for diagnosis
- **Note:** Tests the double-failure scenario where cleanup during partial create also fails

#### TC-KV-UT-029: Empty platform string defaults to KubeVirt
- **Requirements:** REQ-KV-010
- **Type:** Unit
- **Priority:** Low
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `provider_hints.acm.platform=""`
- **Then** the HostedCluster is created with `platform.type=KubeVirt` (empty string treated as absent)

#### TC-KV-UT-030: Invalid page_token returns INVALID_ARGUMENT error
- **Requirements:** REQ-API-290 (service-layer enforcement)
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with at least one HostedCluster
- **When** `List(ctx, 50, "!!!invalid!!!")` is called with a non-base64 page_token
- **Then** an `InvalidArgument` domain error is returned with message "invalid page_token"

#### TC-KV-UT-018: Kubeconfig Secret missing for READY cluster
- **Requirements:** REQ-ACM-130 (edge case)
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a HostedCluster showing `Available=True` (READY) but no associated kubeconfig Secret
- **When** `Get(ctx, "test-id")` is called
- **Then** status is `READY` with `api_endpoint` populated
- **And** `kubeconfig` is empty (graceful degradation, not an error)

#### TC-KV-UT-019: Console URI construction
- **Requirements:** REQ-API-390
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a HostedCluster with `metadata.name="my-cluster"`, `base_domain="example.com"`, `Available=True`, `Progressing=False`
- **When** `Get(ctx, "test-id")` is called
- **Then** status is `READY`
- **And** `console_uri` is `"https://console-openshift-console.apps.my-cluster.example.com"`
- **Given** a HostedCluster with status `PROVISIONING`
- **When** `Get(ctx, "test-id")` is called
- **Then** `console_uri` is empty
- **Given** a HostedCluster with `metadata.name="my-cluster"`, `base_domain="example.com"`, `Available=False`, `Progressing=False`
- **When** `Get(ctx, "test-id")` is called
- **Then** status is `UNAVAILABLE`
- **And** `console_uri` is `"https://console-openshift-console.apps.my-cluster.example.com"` (SHOULD be populated)

#### TC-KV-UT-020: Missing base_domain (neither config nor request)
- **Requirements:** REQ-API-380
- **Type:** Unit
- **Priority:** High
- **Given** no `SP_BASE_DOMAIN` config is set
- **And** `provider_hints.acm.base_domain` is absent from the request
- **When** `Create(ctx, "test-id", cluster)` is called
- **Then** an error is returned indicating base domain is required

#### TC-KV-UT-021: K8s client Get() transient error
- **Requirements:** REQ-ACM-110, REQ-API-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that returns an error on `Get()` for HostedCluster
- **When** `Get(ctx, "test-id")` is called
- **Then** an `Internal` domain error is returned
- **And** no K8s error details are leaked

#### TC-KV-UT-022: K8s client Delete() error
- **Requirements:** REQ-ACM-140, REQ-API-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that returns an error on `Delete()` for HostedCluster
- **When** `Delete(ctx, "test-id")` is called
- **Then** an `Internal` domain error is returned

#### TC-KV-UT-023: K8s client List() error
- **Requirements:** REQ-API-310, REQ-API-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that returns an error on `List()` for HostedClusters
- **When** `List(ctx, "", 50)` is called
- **Then** an `Internal` domain error is returned

#### TC-KV-UT-024: Duplicate dcm-instance-id label on two HostedClusters
- **Requirements:** REQ-ACM-110
- **Type:** Unit
- **Priority:** Low
- **Given** a fake K8s client with two HostedClusters both carrying label `dcm-instance-id=test-id`
- **When** `Get(ctx, "test-id")` is called
- **Then** a deterministic result or error is returned

#### TC-KV-UT-025: Kubeconfig Secret exists but missing kubeconfig key
- **Requirements:** REQ-ACM-130
- **Type:** Unit
- **Priority:** Low
- **Given** a fake K8s client with a HostedCluster showing `Available=True` (READY) and a kubeconfig Secret that exists but does NOT contain a `kubeconfig` key
- **When** `Get(ctx, "test-id")` is called
- **Then** status is `READY` with empty `kubeconfig` (graceful degradation)

#### TC-KV-UT-026: List returns results ordered by metadata.name ascending
- **Requirements:** REQ-API-315
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with HostedClusters named "charlie", "alpha", "bravo" (unordered)
- **When** `List(ctx, "", 50)` is called
- **Then** results are returned in order: "alpha", "bravo", "charlie"

#### TC-KV-UT-027: Get cluster -- UNAVAILABLE with credentials
- **Requirements:** REQ-API-231, REQ-ACM-110
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with:
  - A HostedCluster with `dcm-instance-id=test-id`, `Available=False`, `Progressing=False`, `status.controlPlaneEndpoint.host="api.cluster.example.com"`, `port=6443`
  - A kubeconfig Secret named `<cluster-name>-admin-kubeconfig` with `kubeconfig` key (naming convention derived from HyperShift behavior, not DCM-defined)
- **When** `Get(ctx, "test-id")` is called
- **Then** the returned cluster has `status=UNAVAILABLE`
- **And** `api_endpoint="https://api.cluster.example.com:6443"` (populated if available)
- **And** `console_uri="https://console-openshift-console.apps.<cluster-name>.<base_domain>"` (populated if base_domain available)
- **And** `kubeconfig` is the base64-encoded content from the Secret

---

### 2.11 BareMetal Service (TC-BM-UT-xxx)

Tests the BareMetal `ClusterService` implementation. Status mapping is NOT retested (shared TC-STS-UT-xxx). CRD construction focuses on Agent-platform-specific fields.

#### TC-BM-UT-001: Create BareMetal cluster -- HostedCluster + NodePool with InfraEnv
- **Requirements:** REQ-ACM-010, REQ-BM-010, REQ-BM-020, REQ-ACM-020, REQ-BM-030, REQ-BM-040, REQ-ACM-090, REQ-XC-LBL-010
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `platform="baremetal"`, `infra_env="my-infra"`, `workers={count:3}`
- **Then** a HostedCluster is created with `platform.type=Agent`
- **And** a NodePool is created with `replicas=3`, `platform.type=Agent`, and InfraEnv reference set to `"my-infra"`
- **And** both carry DCM labels

#### TC-BM-UT-002: BareMetal with agent labels
- **Requirements:** REQ-BM-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `agent_labels={"location":"dc1","rack":"r1"}`
- **Then** the NodePool agent label selector includes `{"location":"dc1","rack":"r1"}`

#### TC-BM-UT-003: BareMetal without infra_env and no default
- **Requirements:** REQ-BM-030
- **Type:** Unit
- **Priority:** High
- **Given** no `SP_DEFAULT_INFRA_ENV` configured
- **When** `Create()` is called with `platform="baremetal"` and no `infra_env`
- **Then** an error is returned indicating `infra_env` is required

#### TC-BM-UT-004: BareMetal without infra_env uses SP_DEFAULT_INFRA_ENV
- **Requirements:** REQ-BM-030
- **Type:** Unit
- **Priority:** Medium
- **Given** `SP_DEFAULT_INFRA_ENV="default-infra"` is configured
- **When** `Create()` is called with `platform="baremetal"` and no `infra_env` in request
- **Then** the NodePool references `infra_env="default-infra"`

#### TC-BM-UT-005: Worker resources are informational for BareMetal
- **Requirements:** REQ-BM-060
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `workers.cpu=8, workers.memory="32GB", workers.storage="500GB"`
- **Then** the NodePool is created with `replicas=<count>`
- **And** cpu/memory/storage do NOT appear as resource constraints on the NodePool (physical hardware determines resources)

#### TC-BM-UT-006: Delete BareMetal cluster
- **Requirements:** REQ-ACM-140
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a BareMetal HostedCluster with `dcm-instance-id=bm-id`
- **When** `Delete(ctx, "bm-id")` is called
- **Then** the HostedCluster is deleted

#### TC-BM-UT-007: Get BareMetal cluster -- confirms shared status mapping delegation
- **Requirements:** REQ-ACM-110, REQ-ACM-160
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a BareMetal HostedCluster with `Available=True`, `Progressing=False`, and kubeconfig Secret
- **When** `Get(ctx, "bm-id")` is called
- **Then** status is `READY` with `api_endpoint`, `console_uri`, and `kubeconfig` populated
- **Note:** This single test validates the BareMetal service uses the shared status mapper. Full condition coverage is in TC-STS-UT-xxx.

#### TC-BM-UT-008: NodePool creation failure triggers HostedCluster rollback
- **Requirements:** REQ-ACM-170
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client that succeeds on HostedCluster creation but fails on NodePool creation
- **When** `Create(ctx, "test-id", cluster)` is called with `platform="baremetal"`
- **Then** the SP deletes the orphaned HostedCluster
- **And** an error is returned to the caller
- **And** no HostedCluster remains in the fake client

#### TC-BM-UT-009: K8s client Get() error for BareMetal
- **Requirements:** REQ-ACM-110, REQ-API-050
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client that returns an error on `Get()` for HostedCluster
- **When** `Get(ctx, "bm-id")` is called
- **Then** an `Internal` domain error is returned

#### TC-BM-UT-010: Create BareMetal with base_domain override and config fallback
- **Requirements:** REQ-API-380
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **And** `SP_BASE_DOMAIN="cluster.local"` is configured
- **When** `Create()` is called with `platform="baremetal"` and `provider_hints.acm.base_domain="example.com"`
- **Then** the HostedCluster's base domain is set to `"example.com"` (request overrides default)
- **When** `Create()` is called with `platform="baremetal"` WITHOUT `provider_hints.acm.base_domain`
- **Then** the HostedCluster's base domain is set to `"cluster.local"` (falls back to configured default)

#### TC-BM-UT-012: Release image override for BareMetal
- **Requirements:** REQ-ACM-050, REQ-ACM-051
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with NO matching ClusterImageSet for the requested version
- **When** `Create()` is called with `platform="baremetal"`, `infra_env="my-infra"`, and `provider_hints.acm.release_image="quay.io/ocp-release:4.15.2-x86_64"`
- **Then** the HostedCluster uses the specified release image directly
- **And** no ClusterImageSet lookup error occurs
- **Note:** Confirms shared release_image code works for BareMetal (TC-KV-UT-005 covers KubeVirt)

#### TC-BM-UT-013: control_plane.count and storage are ignored
- **Requirements:** REQ-ACM-070, REQ-ACM-080
- **Type:** Unit
- **Priority:** Medium
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `control_plane.count=5, control_plane.storage="500GB"`
- **Then** the HostedCluster does not set `ControllerAvailabilityPolicy`
- **And** the HostedCluster does not set `Etcd.Managed` config

#### TC-BM-UT-014: control_plane CPU and memory map to resource request override annotations
- **Requirements:** REQ-ACM-060, REQ-ACM-061
- **Type:** Unit
- **Priority:** High
- **Given** a fake K8s client with a ClusterImageSet
- **When** `Create()` is called with `platform="baremetal"`, `infra_env="my-infra"`, `control_plane.cpu=4, control_plane.memory="16GB"`
- **Then** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/kube-apiserver.kube-apiserver` with value `cpu=4,memory=16G`
- **And** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/etcd.etcd` with value `cpu=4,memory=16G`
- **Note:** Confirms shared CP resource override code works for BareMetal (TC-KV-UT-003 is the primary test)

#### TC-BM-UT-011: Create BareMetal with no base_domain and no SP_BASE_DOMAIN config
- **Requirements:** REQ-API-380
- **Type:** Unit
- **Priority:** High
- **Given** no `SP_BASE_DOMAIN` config is set
- **And** `provider_hints.acm.base_domain` is absent from the request
- **When** `Create(ctx, "test-id", cluster)` is called with `platform="baremetal"`
- **Then** an error is returned indicating base domain is required

---

### 2.12 Status Monitoring (TC-MON-UT-xxx)

Tests the informer-based status monitor with a fake K8s client and mock `StatusPublisher`.

#### TC-MON-UT-001: Condition change publishes CloudEvent with correct format
- **Requirements:** REQ-MON-010, REQ-MON-030, REQ-MON-040, REQ-MON-050, REQ-MON-060, REQ-MON-070, REQ-MON-080, REQ-MON-090, REQ-MON-100
- **Type:** Unit
- **Priority:** High
- **Given** a fake informer watching HostedCluster resources
- **And** a mock `StatusPublisher`
- **And** `providerName="acm-cluster-sp"`
- **When** a HostedCluster with `dcm-instance-id="my-cluster"` transitions to `Available=True`
- **Then** a CloudEvent is published via the mock `StatusPublisher`
- **And** subject is `dcm.providers.acm-cluster-sp.cluster.instances.my-cluster.status`
- **And** type is `dcm.providers.acm-cluster-sp.status.update`
- **And** payload contains `status="READY"` and a non-empty `message` field
- **And** the event has `specversion="1.0"`, unique `id`, `source`, `type` (CloudEvents v1.0 conformant)

#### TC-MON-UT-002: Deletion event publishes DELETED status
- **Requirements:** REQ-MON-130
- **Type:** Unit
- **Priority:** High
- **Given** a fake informer with a HostedCluster (`dcm-instance-id="my-cluster"`)
- **When** the HostedCluster is deleted (informer DELETE event)
- **Then** a CloudEvent is published with `status="DELETED"`

#### TC-MON-UT-003: Non-DCM resources are ignored
- **Requirements:** REQ-MON-020
- **Type:** Unit
- **Priority:** High
- **Given** a HostedCluster WITHOUT labels `managed-by=dcm` and `dcm-service-type=cluster`
- **When** its conditions change
- **Then** the mock `StatusPublisher` is NOT called

#### TC-MON-UT-004: Debounce rapid oscillations
- **Requirements:** REQ-MON-115
- **Type:** Unit
- **Priority:** Medium
- **Given** `SP_STATUS_DEBOUNCE_INTERVAL=500ms`
- **And** a HostedCluster oscillates conditions 3 times within 200ms
- **When** the informer processes these changes
- **Then** at most 1 CloudEvent per instance is published within the 500ms window
- **And** debounce is per-instance (other instances are not affected)

#### TC-MON-UT-005: Startup re-lists and publishes current statuses
- **Requirements:** REQ-MON-140
- **Type:** Unit
- **Priority:** High
- **Given** 3 DCM-managed HostedClusters exist in the fake K8s client
- **When** the informer starts and completes its initial list
- **Then** 3 CloudEvents are published, one per cluster, with their current status

#### TC-MON-UT-007: StatusPublisher interface decoupling
- **Requirements:** REQ-MON-150
- **Type:** Unit
- **Priority:** Medium
- **Given** the Status Monitor is constructed with a mock `StatusPublisher`
- **When** events are detected
- **Then** all events are published via the `StatusPublisher.Publish()` method
- **And** the monitor code does NOT import NATS packages directly

#### TC-MON-UT-008: Label selector filtering
- **Requirements:** REQ-XC-LBL-030
- **Type:** Unit
- **Priority:** High
- **Given** the informer is configured with label selector `managed-by=dcm,dcm-service-type=cluster`
- **And** the fake K8s client contains both DCM-managed and non-DCM HostedClusters
- **When** the informer lists resources
- **Then** only resources matching BOTH labels are returned

#### TC-MON-UT-009: Condition update without DCM status change does not publish
- **Requirements:** REQ-MON-040
- **Type:** Unit
- **Priority:** High
- **Given** a HostedCluster with `dcm-instance-id="my-cluster"` has DCM status `PROVISIONING` (e.g., `Progressing=True`, `Available=False`)
- **And** a CloudEvent for `PROVISIONING` was already published
- **When** the HostedCluster conditions update (e.g., a `message` changes on `Progressing`) but the mapped DCM status remains `PROVISIONING`
- **Then** no new CloudEvent is published via the mock `StatusPublisher`

#### TC-MON-UT-010: NATS publish failure retries and drops on exhaustion
- **Requirements:** REQ-MON-160
- **Type:** Unit
- **Priority:** High
- **Given** a mock `StatusPublisher.Publish` that returns an error for the first 3 calls then succeeds
- **And** `SP_NATS_PUBLISH_RETRY_MAX=3`, `SP_NATS_PUBLISH_RETRY_INTERVAL=10ms` (fast for testing)
- **When** a status change triggers a CloudEvent publish
- **Then** the publisher retries 3 times
- **And** on the 4th attempt (3 retries), it succeeds
- **Given** a mock `StatusPublisher.Publish` that always returns an error
- **When** a status change triggers a CloudEvent publish
- **Then** the publisher retries `SP_NATS_PUBLISH_RETRY_MAX` times
- **And** after exhaustion, the event is dropped (no panic, no block)
- **And** subsequent status changes trigger new publish attempts normally

---

#### TC-MON-UT-011: Update event with deletionTimestamp publishes DELETING
- **Requirements:** REQ-MON-030, REQ-MON-040
- **Type:** Unit
- **Priority:** High
- **Given** a HostedCluster with `dcm-instance-id="my-cluster"` has status `READY`
- **And** a CloudEvent for `READY` was already published
- **When** the HostedCluster is updated with `deletionTimestamp` set (K8s Update event, not Delete)
- **Then** a CloudEvent is published with `status="DELETING"`
- **And** when the HostedCluster is subsequently fully removed (Delete event)
- **Then** a CloudEvent is published with `status="DELETED"`

#### TC-MON-UT-012: dcm-instance-id secondary index returns correct resources
- **Requirements:** REQ-MON-035
- **Type:** Unit
- **Priority:** Medium
- **Given** the informer has a secondary index on the `dcm-instance-id` label
- **And** 3 HostedClusters exist with distinct `dcm-instance-id` values
- **When** a lookup by `dcm-instance-id="target-id"` is performed
- **Then** only the matching HostedCluster is returned

#### TC-MON-UT-014: UNAVAILABLE to READY recovery transition
- **Requirements:** REQ-MON-040, REQ-ACM-110
- **Type:** Unit
- **Priority:** Medium
- **Given** a HostedCluster with `dcm-instance-id="my-cluster"` has DCM status `UNAVAILABLE`
- **And** a CloudEvent for `UNAVAILABLE` was already published
- **When** the HostedCluster conditions change to `Available=True`, `Progressing=False`
- **Then** a CloudEvent is published with `status="READY"`
- **Note:** Explicitly covers the recovery path (UNAVAILABLE → READY) as a legitimate transition

#### TC-MON-UT-015: UNAVAILABLE CloudEvent message includes context
- **Requirements:** REQ-MON-090, REQ-API-167
- **Type:** Unit
- **Priority:** Low
- **Given** a HostedCluster with `dcm-instance-id="my-cluster"` has `Available=False`, `Progressing=False` with a condition message "API server unreachable"
- **When** the informer detects the status change to UNAVAILABLE
- **Then** the published CloudEvent's `message` field includes context from conditions (e.g., "API server unreachable")

#### TC-MON-UT-016: Missing dcm-instance-id label is handled gracefully
- **Requirements:** REQ-MON-080
- **Type:** Unit
- **Priority:** Medium
- **Given** a HostedCluster with labels `managed-by=dcm`, `dcm-service-type=cluster` but WITHOUT `dcm-instance-id`
- **When** the informer receives an event for this resource
- **Then** the resource is skipped (no CloudEvent published)
- **And** a warning is logged indicating the missing label

#### TC-MON-UT-017: Duplicate dcm-instance-id in monitoring path
- **Requirements:** REQ-MON-080
- **Type:** Unit
- **Priority:** Low
- **Given** two HostedClusters both have label `dcm-instance-id="shared-id"`
- **When** the informer receives events for both resources
- **Then** CloudEvents are published for each (both with the same instanceId)
- **And** a warning is logged indicating duplicate `dcm-instance-id` detected

#### TC-MON-UT-013: FAILED CloudEvent message includes failure reason
- **Requirements:** REQ-MON-155
- **Type:** Unit
- **Priority:** Medium
- **Given** a HostedCluster with `dcm-instance-id="my-cluster"` has `Degraded=True` with condition message "etcd cluster unhealthy"
- **When** the informer detects the status change to FAILED
- **Then** the published CloudEvent's `message` field includes the failure reason "etcd cluster unhealthy"

### 2.13 Configuration (TC-CFG-UT-xxx)

#### TC-CFG-UT-001: Required config missing causes fail-fast
- **Requirements:** REQ-XC-CFG-020
- **Type:** Unit
- **Priority:** High
- **Given** each required variable is unset in turn: `DCM_REGISTRATION_URL`, `SP_ENDPOINT`, `SP_NATS_URL`, `SP_CLUSTER_NAMESPACE`
- **When** the configuration loader runs
- **Then** an error is returned immediately with a clear message naming the missing variable

#### TC-CFG-UT-002: All config from environment variables with defaults
- **Requirements:** REQ-XC-CFG-010
- **Type:** Unit
- **Priority:** Medium
- **Given** all required env vars are set: `DCM_REGISTRATION_URL`, `SP_ENDPOINT`, `SP_NATS_URL`
- **And** optional vars are NOT set
- **When** the configuration loader runs
- **Then** defaults are applied: `SP_SERVER_ADDRESS=":8080"`, `SP_SERVER_SHUTDOWN_TIMEOUT=15s`, `SP_SERVER_REQUEST_TIMEOUT=30s`, `SP_NAME="acm-cluster-sp"`, `SP_REGISTRATION_RETRY_MAX=5`, etc.

#### TC-CFG-UT-003: K8s client config resolution
- **Requirements:** REQ-XC-K8S-020
- **Type:** Unit
- **Priority:** Low
- **Given** `SP_HUB_KUBECONFIG=/path/to/kubeconfig` is set
- **When** the K8s client factory is called
- **Then** it uses the kubeconfig file (out-of-cluster mode)
- **Given** `SP_HUB_KUBECONFIG` is NOT set
- **When** the K8s client factory is called
- **Then** it attempts in-cluster config

---

### 2.14 Cross-Cutting: Resource Identity (TC-XC-ID-UT-xxx)

#### TC-XC-ID-UT-001: Resources have both K8s name and DCM instance ID
- **Requirements:** REQ-XC-ID-010
- **Type:** Unit (Structural)
- **Priority:** Medium
- **Given** a cluster is created with `id="dcm-id"` and `metadata.name="my-cluster"`
- **When** the K8s resources are inspected
- **Then** the HostedCluster has `metadata.name="my-cluster"` and label `dcm-instance-id=dcm-id`
- **And** the NodePool has label `dcm-instance-id=dcm-id`

#### TC-XC-ID-UT-002: dcm-instance-id label matches the id field
- **Requirements:** REQ-XC-ID-020
- **Type:** Unit (Structural)
- **Priority:** Medium
- **Given** a cluster is created with `id="test-id"`
- **When** the HostedCluster is retrieved by label `dcm-instance-id=test-id`
- **Then** the matching resource is found
- **And** conflict detection checks both `id` uniqueness (via label) and `metadata.name` uniqueness (via K8s name) independently

---

## Coverage Matrix

### Registration Requirements (REQ-REG-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-REG-010 | TC-REG-UT-001 | Covered |
| REQ-REG-020 | TC-REG-UT-001 | Covered |
| REQ-REG-030 | TC-REG-IT-004 | Covered |
| REQ-REG-031 | TC-REG-IT-004 | Covered |
| REQ-REG-040 | TC-REG-UT-003, TC-REG-UT-004, TC-REG-UT-009, TC-REG-UT-010 | Covered |
| REQ-REG-050 | TC-REG-UT-003, TC-REG-UT-009 | Covered |
| REQ-REG-051 | TC-REG-UT-003, TC-REG-UT-004, TC-REG-UT-009, TC-REG-IT-005 | Covered |
| REQ-REG-060 | TC-REG-UT-002 | Covered |
| REQ-REG-070 | TC-REG-UT-011 | Covered |
| REQ-REG-080 | TC-REG-UT-001 | Covered |
| REQ-REG-090 | TC-REG-UT-001, TC-REG-UT-007 | Covered |
| REQ-REG-091 | TC-REG-UT-012, TC-REG-UT-013 | Covered |
| REQ-REG-100 | TC-REG-UT-005, TC-REG-UT-008 | Covered |
| REQ-REG-110 | TC-REG-UT-005, TC-REG-UT-008, TC-REG-UT-010 | Covered |

### HTTP Server Requirements (REQ-HTTP-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-HTTP-010 | TC-HTTP-UT-006 | Covered |
| REQ-HTTP-020 | TC-HTTP-IT-001, TC-INT-007 | Covered |
| REQ-HTTP-030 | TC-HTTP-IT-002, TC-HTTP-IT-009 | Covered |
| REQ-HTTP-040 | TC-HTTP-IT-002, TC-HTTP-IT-009 | Covered |
| REQ-HTTP-050 | TC-HTTP-UT-006, TC-HTTP-UT-012 | Covered |
| REQ-HTTP-060 | TC-HTTP-IT-010 | Covered |
| REQ-HTTP-070 | TC-HTTP-IT-003 | Covered |
| REQ-HTTP-080 | TC-HTTP-IT-011 | Covered |
| REQ-HTTP-090 | TC-HTTP-IT-004, TC-INT-005 | Covered |
| REQ-HTTP-091 | TC-HTTP-IT-005 | Covered |
| REQ-HTTP-110 | TC-HTTP-IT-008 | Covered |

### Health Requirements (REQ-HLT-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-HLT-010 | TC-HLT-UT-001, TC-HLT-IT-007, TC-INT-004 | Covered |
| REQ-HLT-020 | TC-HLT-UT-001, TC-HLT-UT-007, TC-HLT-IT-007 | Covered |
| REQ-HLT-030 | TC-HLT-UT-001 | Covered |
| REQ-HLT-040 | TC-HLT-UT-001 | Covered |
| REQ-HLT-050 | TC-HLT-UT-001, TC-INT-004 | Covered |
| REQ-HLT-060 | TC-HLT-UT-002, TC-HLT-UT-003 | Covered |
| REQ-HLT-070 | TC-HLT-UT-002 | Covered |
| REQ-HLT-080 | TC-HLT-UT-003 | Covered |
| REQ-HLT-090 | TC-HLT-UT-005 | Covered |
| REQ-HLT-100 | TC-HLT-UT-006 | Covered |
| REQ-HLT-110 | TC-HLT-UT-004 | Covered |
| REQ-HLT-120 | TC-HLT-UT-001 | Covered (no auth in test = no auth required) |

### API / Handler Requirements (REQ-API-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-API-010 | Structural | Compile-time `var _` check |
| REQ-API-011 | Structural | Import analysis / depguard |
| REQ-API-012 | Structural | Constructor signature |
| REQ-API-020 | TC-ERR-UT-001 | Covered |
| REQ-API-030 | TC-ERR-UT-001 | Covered |
| REQ-API-040 | TC-ERR-UT-001 | Covered |
| REQ-API-050 | TC-HDL-CRT-UT-012, TC-ERR-UT-002, TC-KV-UT-021, TC-KV-UT-022, TC-KV-UT-023, TC-BM-UT-009 | Covered |
| REQ-API-060 | TC-HDL-CRT-UT-001, TC-INT-001 | Covered |
| REQ-API-070 | TC-HDL-CRT-UT-011, TC-HDL-CRT-UT-013 | Covered |
| REQ-API-080 | TC-HDL-CRT-UT-005, TC-HDL-CRT-UT-014 | Covered |
| REQ-API-090 | TC-HDL-CRT-UT-004, TC-HDL-CRT-UT-016 | Covered |
| REQ-API-100 | TC-HDL-CRT-UT-001, TC-HDL-CRT-UT-002, TC-HDL-CRT-UT-015, TC-HDL-CRT-UT-019 | Covered |
| REQ-API-101 | TC-HDL-CRT-UT-003 | Covered |
| REQ-API-102 | TC-HDL-CRT-UT-007, TC-KV-UT-014 | Covered |
| REQ-API-103 | TC-HDL-CRT-UT-008, TC-KV-UT-015 | Covered |
| REQ-API-104 | TC-HDL-CRT-UT-002, TC-HDL-CRT-UT-003 | Covered |
| REQ-API-110 | TC-HDL-CRT-UT-001, TC-INT-001 | Covered |
| REQ-API-130 | TC-HDL-CRT-UT-009 | Covered |
| REQ-API-140 | TC-HDL-CRT-UT-010 | Covered |
| REQ-API-150 | TC-HDL-CRT-UT-011, TC-HDL-CRT-UT-013, TC-HDL-CRT-UT-015, TC-HDL-CRT-UT-016 | Covered |
| REQ-API-160 | TC-HDL-CRT-UT-003 | Covered |
| REQ-API-165 | TC-KV-UT-010 | Covered (update_time from lastTransitionTime) |
| REQ-API-166 | TC-KV-UT-021 | Covered (status_message for FAILED) |
| REQ-API-167 | TC-MON-UT-015 | Covered (status_message for UNAVAILABLE) |
| REQ-API-170 | TC-HDL-CRT-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018 | Covered |
| REQ-API-175 | Structural (OpenAPI pattern + validation middleware) | Verified by OpenAPI spec |
| REQ-API-180 | TC-HDL-GET-UT-001 | Covered |
| REQ-API-190 | TC-HDL-GET-UT-001, TC-HDL-GET-UT-002 | Covered |
| REQ-API-200 | TC-HDL-GET-UT-003 | Covered |
| REQ-API-210 | TC-HDL-GET-UT-004, TC-HDL-DEL-UT-006 | Covered |
| REQ-API-220 | TC-HDL-GET-UT-001 | Covered |
| REQ-API-230 | TC-HDL-GET-UT-002 | Covered |
| REQ-API-231 | TC-HDL-GET-UT-005, TC-KV-UT-027 | Covered |
| REQ-API-240 | TC-HDL-LST-UT-001, TC-HDL-LST-UT-006 | Covered |
| REQ-API-250 | TC-HDL-LST-UT-001, TC-INT-003 | Covered |
| REQ-API-260 | TC-HDL-LST-UT-001 | Covered |
| REQ-API-270 | TC-HDL-LST-UT-002, TC-HDL-LST-UT-007 | Covered |
| REQ-API-280 | TC-HDL-LST-UT-003, TC-HDL-LST-UT-007 | Covered |
| REQ-API-290 | TC-HDL-LST-UT-004, TC-HDL-LST-UT-008 | Covered |
| REQ-API-291 | TC-HDL-LST-UT-004 | Covered (token opacity/validation) |
| REQ-API-300 | TC-HDL-LST-UT-005, TC-HDL-LST-UT-006, TC-INT-003 | Covered |
| REQ-API-310 | TC-HDL-LST-UT-001 (implicit), TC-KV-UT-023 | Covered |
| REQ-API-315 | TC-KV-UT-026, TC-INT-003 | Covered |
| REQ-API-320 | TC-HDL-DEL-UT-001, TC-INT-002 | Covered |
| REQ-API-330 | TC-HDL-DEL-UT-001 | Covered |
| REQ-API-340 | TC-HDL-DEL-UT-002, TC-INT-002 | Covered |
| REQ-API-350 | TC-HDL-DEL-UT-001 | Covered |
| REQ-API-360 | TC-HDL-DEL-UT-003 | Covered |
| REQ-API-370 | TC-HDL-DEL-UT-004, TC-HDL-DEL-UT-005 | Covered |
| REQ-API-380 | TC-KV-UT-006, TC-KV-UT-020, TC-BM-UT-010, TC-BM-UT-011 | Covered |
| REQ-API-390 | TC-KV-UT-019, TC-KV-UT-010, TC-KV-UT-027, TC-BM-UT-007 | Covered |

### ACM Common Requirements (REQ-ACM-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-ACM-010 | TC-KV-UT-001, TC-KV-UT-016, TC-BM-UT-001, TC-INT-001, TC-INT-006 | Covered |
| REQ-ACM-020 | TC-KV-UT-001, TC-BM-UT-001 | Covered |
| REQ-ACM-030 | TC-KV-UT-004 | Covered |
| REQ-ACM-031 | TC-KV-UT-004 | Covered |
| REQ-ACM-032 | TC-KV-UT-004 | Covered |
| REQ-ACM-040 | TC-KV-UT-004 | Covered |
| REQ-ACM-050 | TC-KV-UT-005, TC-BM-UT-012 | Covered |
| REQ-ACM-051 | TC-KV-UT-005, TC-BM-UT-012 | Covered |
| REQ-ACM-060 | TC-KV-UT-003, TC-BM-UT-014 | Covered |
| REQ-ACM-061 | TC-KV-UT-003, TC-BM-UT-014 | Covered |
| REQ-ACM-070 | TC-KV-UT-002, TC-BM-UT-013 | Covered |
| REQ-ACM-080 | TC-KV-UT-002, TC-BM-UT-013 | Covered |
| REQ-ACM-090 | TC-KV-UT-001, TC-BM-UT-001 | Covered |
| REQ-ACM-100 | TC-KV-UT-001 | Covered |
| REQ-ACM-110 | TC-STS-UT-001..005, TC-STS-UT-011, TC-KV-UT-010, TC-KV-UT-011, TC-KV-UT-021, TC-KV-UT-024, TC-KV-UT-027, TC-BM-UT-007, TC-BM-UT-009 | Covered |
| REQ-ACM-120 | TC-KV-UT-010 | Covered |
| REQ-ACM-130 | TC-KV-UT-010, TC-KV-UT-018, TC-KV-UT-025 | Covered |
| REQ-ACM-140 | TC-KV-UT-013, TC-KV-UT-022, TC-BM-UT-006, TC-INT-002 | Covered |
| REQ-ACM-150 | TC-KV-UT-008 | Covered |
| REQ-ACM-160 | TC-STS-UT-001..012, TC-BM-UT-007 | Covered |
| REQ-ACM-170 | TC-KV-UT-017, TC-KV-UT-028, TC-BM-UT-008 | Covered |

### KubeVirt Requirements (REQ-KV-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-KV-010 | TC-KV-UT-001, TC-INT-001, TC-INT-008 | Covered |
| REQ-KV-020 | TC-KV-UT-001, TC-INT-001 | Covered |
| REQ-KV-030 | TC-KV-UT-001 | Covered |
| REQ-KV-040 | TC-KV-UT-009 | Covered |

### BareMetal Requirements (REQ-BM-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-BM-010 | TC-BM-UT-001, TC-INT-006, TC-INT-008 | Covered |
| REQ-BM-020 | TC-BM-UT-001, TC-INT-006 | Covered |
| REQ-BM-030 | TC-BM-UT-001, TC-BM-UT-003, TC-BM-UT-004 | Covered |
| REQ-BM-040 | TC-BM-UT-001, TC-INT-006 | Covered |
| REQ-BM-050 | TC-BM-UT-002 | Covered |
| REQ-BM-060 | TC-BM-UT-005 | Covered |

### Monitoring Requirements (REQ-MON-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-MON-010 | TC-MON-UT-001 | Covered |
| REQ-MON-020 | TC-MON-UT-003, TC-MON-UT-008 | Covered |
| REQ-MON-030 | TC-MON-UT-001, TC-MON-UT-011 | Covered |
| REQ-MON-035 | TC-MON-UT-012 | Covered |
| REQ-MON-040 | TC-MON-UT-001, TC-MON-UT-009, TC-MON-UT-011 | Covered |
| REQ-MON-050 | TC-MON-UT-001 | Covered |
| REQ-MON-060 | TC-MON-UT-001 | Covered |
| REQ-MON-070 | TC-MON-UT-001 | Covered |
| REQ-MON-080 | TC-MON-UT-001 | Covered |
| REQ-MON-090 | TC-MON-UT-001 | Covered |
| REQ-MON-100 | TC-MON-UT-001 | Covered |
| REQ-MON-110 | TC-MON-IT-006 | Covered |
| REQ-MON-115 | TC-MON-UT-004 | Covered |
| REQ-MON-125 | TC-MON-IT-007 | Covered |
| REQ-MON-126 | TC-MON-IT-008 | Covered |
| REQ-MON-130 | TC-MON-UT-002 | Covered |
| REQ-MON-135 | TC-MON-IT-009 | Covered |
| REQ-MON-140 | TC-MON-UT-005, TC-MON-IT-010 | Covered |
| REQ-MON-170 | TC-MON-IT-xxx | Covered |
| REQ-MON-150 | TC-MON-UT-007 | Covered |
| REQ-MON-155 | TC-MON-UT-013, TC-MON-IT-011 | Covered |
| REQ-MON-160 | TC-MON-UT-010 | Covered |

### Cross-Cutting Requirements (REQ-XC-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-XC-ERR-010 | TC-ERR-UT-001, TC-INT-005 | Covered |
| REQ-XC-ERR-020 | TC-ERR-UT-001 | Covered |
| REQ-XC-ERR-030 | TC-ERR-UT-003 | Covered |
| REQ-XC-ERR-040 | TC-ERR-UT-002, TC-HDL-CRT-UT-012 | Covered |
| REQ-XC-LBL-010 | TC-KV-UT-001, TC-BM-UT-001 | Covered |
| REQ-XC-LBL-020 | TC-KV-UT-001 | Covered |
| REQ-XC-LBL-030 | TC-MON-UT-008 | Covered |
| REQ-XC-K8S-010 | Structural | Verified by go.mod dependency |
| REQ-XC-K8S-020 | TC-CFG-UT-003 | Covered |
| REQ-XC-K8S-030 | Structural | Code review (context usage in all K8s calls) |
| REQ-XC-LOG-010 | Structural | Code review / linter |
| REQ-XC-LOG-020 | TC-HTTP-IT-010 | Covered |
| REQ-XC-LOG-030 | Structural | Code review |
| REQ-XC-CFG-010 | TC-CFG-UT-001, TC-CFG-UT-002 | Covered |
| REQ-XC-CFG-020 | TC-CFG-UT-001 | Covered |
| REQ-XC-ID-010 | TC-XC-ID-UT-001 | Covered |
| REQ-XC-ID-020 | TC-XC-ID-UT-002 | Covered |

---

## Consolidation Notes

### Status Mapping Consolidated to SHARED Component

The spec defines status mapping as a common behavior (REQ-ACM-110, REQ-ACM-160) shared across all platforms. Rather than duplicating status tests per platform:

- **10 shared test cases** (TC-STS-UT-001 through TC-STS-UT-008, TC-STS-UT-011, TC-STS-UT-012) cover all condition combinations and precedence rules
- **1 KubeVirt test** (TC-KV-UT-010) confirms the KubeVirt service extracts READY-specific fields using the shared mapper
- **1 BareMetal test** (TC-BM-UT-007) confirms the BareMetal service delegates to the shared mapper

This reduces 15+ potential duplicate tests to 12 without losing coverage.

### Shared Code Beyond Status Mapping

| Shared Behavior | Tested Via (KubeVirt) | BareMetal Confirmation |
|---|---|---|
| Version validation (ClusterImageSet lookup) | TC-KV-UT-004 | Shared code; TC-BM-UT-001 implicitly confirms |
| Conflict detection (duplicate `id` by label) | TC-KV-UT-014 | Shared code |
| Conflict detection (duplicate `metadata.name`) | TC-KV-UT-015 | Shared code |
| `console_uri` construction | TC-KV-UT-019 | TC-BM-UT-007 confirms via READY status |
| Release image override | TC-KV-UT-005 | TC-BM-UT-012 confirms shared code |
| `base_domain` handling (config default + request override) | TC-KV-UT-006 | TC-BM-UT-010 (new) |
| Memory/storage format conversion | TC-KV-UT-008 | N/A for BareMetal (informational per REQ-BM-060) |
| CP resource override annotations (DEC-004) | TC-KV-UT-003 | TC-BM-UT-014 confirms shared code |
| List ordering (`metadata.name` ascending) | TC-KV-UT-026 | Shared code; no platform-specific sort |

### Spec ACs Merged into Fewer Test Cases

| Spec AC(s) | Merged Into TC | Rationale |
|---|---|---|
| AC-REG-010, AC-REG-030 | TC-REG-UT-001 | Payload and capabilities are naturally tested together |
| AC-REG-040 | TC-REG-UT-003, TC-REG-UT-004 | Infinite retry + 4xx non-retryable are complementary failure paths |
| AC-ACM-110, AC-ACM-160 | TC-STS-UT-001..012 | All status mapping consolidated to shared component |
| AC-API-010, AC-API-011 | TC-HDL-CRT-UT-001, TC-HDL-CRT-UT-002 | Server vs client ID are two sides of the same feature |
| AC-API-070 | TC-HDL-CRT-UT-003 | Read-only field test covers all 9 read-only fields at once |
| AC-API-080, AC-API-090 | TC-HDL-GET-UT-001, TC-HDL-GET-UT-002 | READY vs not-READY are complementary |
| AC-API-130, AC-API-140 | TC-HDL-DEL-UT-001, TC-HDL-DEL-UT-002 | Happy and error path for delete |

### Eliminated Redundancy

- **REQ-API-310** (List results from K8s): Handler just passes through; real K8s sourcing tested in service layer
- **REQ-ACM-110 / REQ-ACM-160**: Tested ONCE in shared mapper (TC-STS-UT-xxx), NOT per platform
- **AC-API-150 / AC-ACM-140**: DELETING status tested once in TC-STS-UT-005; handler pass-through in TC-HDL-DEL-UT-005
- **REQ-API-315** (List ordering): Sorting is service-layer concern; handler passes through. Tested once via KubeVirt service (TC-KV-UT-026)

---

## Summary Statistics

| Category | Count |
|---|---|
| **Total unit test cases** | **137** |
| High priority | 63 |
| Medium priority | 59 |
| Low priority | 15 |
| Structural (no behavioral test) | 10 requirements |
| Total requirements covered (across both unit and integration) | 166 (all REQ-xxx IDs) |
| Coverage gaps | **0** |

### Test Cases by Component

| Component | Test Case Prefix | Count |
|---|---|---|
| Registration | TC-REG-UT-xxx | 12 |
| HTTP Server | TC-HTTP-UT-xxx | 1 |
| Health Service | TC-HLT-UT-xxx | 7 |
| Handler: Create | TC-HDL-CRT-UT-xxx | 19 |
| Handler: Get | TC-HDL-GET-UT-xxx | 5 |
| Handler: List | TC-HDL-LST-UT-xxx | 8 |
| Handler: Delete | TC-HDL-DEL-UT-xxx | 6 |
| Error Mapping | TC-ERR-UT-xxx | 3 |
| Status Mapping (shared) | TC-STS-UT-xxx | 12 |
| KubeVirt Service | TC-KV-UT-xxx | 29 |
| BareMetal Service | TC-BM-UT-xxx | 14 |
| Status Monitoring | TC-MON-UT-xxx | 16 |
| Configuration | TC-CFG-UT-xxx | 3 |
| Cross-Cutting: Identity | TC-XC-ID-UT-xxx | 2 |

### Notes

- Test cases are numbered sequentially within each prefix and MUST NOT be renumbered after approval
- Test helpers in `internal/testutil/` are shared across all test packages
- Table-driven test patterns are preferred for status mapping (TC-STS-UT-xxx) and error mapping (TC-ERR-UT-xxx)

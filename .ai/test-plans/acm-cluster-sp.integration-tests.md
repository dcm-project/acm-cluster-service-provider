# Test Plan: ACM Cluster Service Provider -- Integration Tests

## Overview

- **Related Spec:** .ai/specs/acm-cluster-sp.spec.md
- **Related Requirements:** REQ-REG-xxx, REQ-HTTP-xxx, REQ-HLT-xxx, REQ-API-xxx, REQ-ACM-xxx, REQ-KV-xxx, REQ-BM-xxx, REQ-MON-xxx, REQ-XC-xxx
- **Created:** 2026-02-17
- **Last Updated:** 2026-03-26 (TC-KV-UT → TC-OPS-UT rename for shared ops; coverage matrix corrections; phantom BM TC references removed)
- **Scope:** This file covers **integration tests only** (31 test cases). Unit tests are in `acm-cluster-sp.unit-tests.md`.

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

Test design decisions are documented in `.ai/decisions/test-decisions.md` (TD-001 through TD-014).
Design and implementation decisions referenced by test cases are in `.ai/decisions/design-decisions.md` (DD-xxx) and `.ai/decisions/implementation-decisions.md` (IMPL-xxx).

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

### 2.1 Registration (TC-REG-IT-xxx)

#### TC-REG-IT-004: Server accepts requests before registration completes
- **Requirements:** REQ-REG-030, REQ-REG-031
- **Type:** Integration
- **Priority:** Medium
- **Given** the DCM Registry is reachable but responds slowly (artificial delay)
- **When** the SP starts up
- **Then** the HTTP server starts accepting requests immediately
- **And** registration proceeds asynchronously in the background
- **And** requests arriving before registration completes are served normally (not 503)

#### TC-REG-IT-005: Registration failure does not cause SP exit
- **Requirements:** REQ-REG-051
- **Type:** Integration
- **Priority:** High
- **Given** the DCM Registry returns 500 errors
- **When** the SP starts up
- **Then** registration retries are attempted indefinitely in the background
- **And** error logs include response body content
- **And** the SP process continues running
- **And** the HTTP server continues serving requests

#### TC-REG-IT-006: Registration retries with exponential backoff and max cap
- **Requirements:** REQ-REG-040
- **Type:** Integration
- **Priority:** Medium
- **Given** the DCM Registry returns HTTP 500 on every request
- **When** the SP starts up and attempts registration
- **Then** retries continue indefinitely with exponential backoff
- **And** the backoff does not exceed `SP_REGISTRATION_MAX_BACKOFF`
- **And** retries stop only on success or context cancellation

#### TC-REG-IT-007: Registration uses DCM client library
- **Requirements:** REQ-REG-070
- **Type:** Integration
- **Priority:** Low
- **Given** the SP is configured with a DCM Registry URL
- **When** the SP registers with the registry
- **Then** the official DCM SP API client library is used for communication

---

### 2.2 HTTP Server (TC-HTTP-IT-xxx)

#### TC-HTTP-IT-001: Routes mounted under correct paths
- **Requirements:** REQ-HTTP-020
- **Type:** Integration
- **Priority:** High
- **Given** the HTTP server is started with stub handlers
- **When** `GET /clusters` is sent (no prefix)
- **Then** the response is 404
- **When** `GET /api/v1alpha1/clusters` is sent
- **Then** the response is NOT 404
- **When** `GET /api/v1alpha1/clusters/health` is sent
- **Then** the response is NOT 404

#### TC-HTTP-IT-002: Graceful shutdown drains in-flight requests
- **Requirements:** REQ-HTTP-030, REQ-HTTP-040
- **Type:** Integration
- **Priority:** High
- **Given** the server is running and a slow handler is processing a request (artificially delayed)
- **When** SIGTERM (or SIGINT) is sent to the server
- **Then** the in-flight request completes successfully
- **And** new connection attempts after SIGTERM are refused
- **And** the server exits cleanly after the request completes

#### TC-HTTP-IT-003: Panic recovery keeps server alive
- **Requirements:** REQ-HTTP-070
- **Type:** Integration
- **Priority:** Medium
- **Given** a handler that panics on a specific route
- **When** a request triggers the panic
- **Then** the response is 500
- **When** a subsequent normal request is sent
- **Then** the response is successful (server did not crash)

#### TC-HTTP-IT-004: Request errors return RFC 7807
- **Requirements:** REQ-HTTP-090
- **Type:** Integration
- **Priority:** High
- **Given** the server is running with the generated strict handler
- **When** `POST /api/v1alpha1/clusters` is sent with malformed JSON
- **Then** Content-Type is `application/problem+json`
- **And** the body contains `type`, `title`, and `status` fields

#### TC-HTTP-IT-005: Response errors return RFC 7807 with type=INTERNAL
- **Requirements:** REQ-HTTP-091
- **Type:** Integration
- **Priority:** Medium
- **Given** the ClusterService returns an unexpected internal error
- **When** a request is processed
- **Then** the response Content-Type is `application/problem+json`
- **And** `type` is `"INTERNAL"`
- **And** no stack traces or K8s error details are leaked

#### TC-HTTP-IT-008: Request timeout middleware
- **Requirements:** REQ-HTTP-110
- **Type:** Integration
- **Priority:** Low
- **Given** `SP_SERVER_REQUEST_TIMEOUT=1s` and a handler that takes 3s
- **When** a request is sent
- **Then** the request is cancelled after 1s with a timeout error

#### TC-HTTP-IT-009: Shutdown timeout expiry force-closes connections
- **Requirements:** REQ-HTTP-030, REQ-HTTP-040
- **Type:** Integration
- **Priority:** Low
- **Given** the server is running with `SP_SERVER_SHUTDOWN_TIMEOUT=1s`
- **And** a request is in-flight that will take >2s
- **When** SIGTERM is received
- **And** `SP_SERVER_SHUTDOWN_TIMEOUT` elapses
- **Then** the server force-closes the in-flight connection
- **And** the server exits

#### TC-HTTP-IT-010: Request logging middleware
- **Requirements:** REQ-HTTP-060
- **Type:** Integration
- **Priority:** Low
- **Given** the server is running with structured logging captured to a buffer
- **When** `GET /api/v1alpha1/clusters` is sent and returns 200
- **Then** the log output contains entries with method, path, status code, and duration fields

#### TC-HTTP-IT-011: Lifecycle events logged
- **Requirements:** REQ-HTTP-080
- **Type:** Integration
- **Priority:** Low
- **Given** the server is running with structured logging captured to a buffer
- **When** the server starts up
- **Then** a log entry is emitted containing the listen address
- **When** shutdown is initiated
- **Then** a log entry is emitted indicating shutdown

---

### 2.3 Health Service (TC-HLT-IT-xxx)

#### TC-HLT-IT-007: Health endpoint integration (HTTP layer)
- **Requirements:** REQ-HLT-010, REQ-HLT-020
- **Type:** Integration
- **Priority:** High
- **Given** the server is running with a mock `HealthChecker` returning `status="healthy"`
- **When** `GET /api/v1alpha1/clusters/health` is sent
- **Then** response status is 200
- **And** Content-Type is `application/json`
- **And** body contains `type`, `status`, `path`, `version`, `uptime` (all required fields present)

---

### 2.12 Status Monitoring (TC-MON-IT-xxx)

#### TC-MON-IT-006: Informer reconnects after watch disconnect
- **Requirements:** REQ-MON-110
- **Type:** Integration
- **Priority:** Medium
- **Given** the informer is watching
- **When** the K8s API watch connection drops (simulated)
- **Then** the informer re-lists and resumes watching
- **And** subsequent status changes are still detected and published

#### TC-MON-IT-007: Watchers start after HTTP server is ready
- **Requirements:** REQ-MON-125
- **Type:** Integration
- **Priority:** Medium
- **Given** the SP is starting up
- **When** the HTTP server is ready to accept requests
- **Then** resource watchers are started as async background tasks
- **And** the watchers begin watching HostedCluster resources

#### TC-MON-IT-008: Watchers stop during graceful shutdown
- **Requirements:** REQ-MON-126
- **Type:** Integration
- **Priority:** Medium
- **Given** the SP is running with active resource watchers
- **When** a shutdown signal is received
- **Then** resource watchers are stopped
- **And** the SP exits cleanly

#### TC-MON-IT-009: Periodic resync triggers re-evaluation
- **Requirements:** REQ-MON-135
- **Type:** Integration
- **Priority:** Medium
- **Given** `SP_STATUS_RESYNC_INTERVAL` is configured to a short interval (e.g., 1s for testing)
- **And** the informer has been watching for longer than the resync interval
- **When** the resync interval elapses
- **Then** the informer re-evaluates status for all cached resources
- **And** any status changes since last evaluation are published as CloudEvents

#### TC-MON-IT-010: Initial status sync publishes events for all existing resources on startup
- **Requirements:** REQ-MON-140
- **Type:** Integration
- **Priority:** High
- **Given** 3 DCM-managed HostedClusters exist in the K8s cluster
- **When** the SP starts and the initial cache sync completes
- **Then** a status CloudEvent is published for each of the 3 clusters with their current status

#### TC-MON-IT-011: FAILED status events include failure reason
- **Requirements:** REQ-MON-155
- **Type:** Integration
- **Priority:** Medium
- **Given** a HostedCluster has `Degraded=True` with a condition message "control plane unavailable"
- **When** the informer detects the status change to FAILED
- **Then** the published CloudEvent's `message` field includes the failure reason

---

### 2.14 Integration Tests (TC-INT-xxx)

Full-stack integration tests using `controller-runtime/envtest` (real etcd + kube-apiserver) with `httptest` for the HTTP server. No mocks -- real handler, real service, real K8s client.

Build constraint: `//go:build integration`

#### TC-INT-001: Create and Get KubeVirt cluster round-trip
- **Requirements:** REQ-API-060, REQ-API-100, REQ-API-110, REQ-ACM-010, REQ-KV-010, REQ-KV-020
- **Type:** Integration
- **Priority:** High
- **Given** envtest is running with HyperShift CRDs installed and a ClusterImageSet for OCP "4.17.0" (K8s "1.30") created
- **When** `POST /api/v1alpha1/clusters` with valid KubeVirt cluster body
- **Then** response is 201 with server-generated `id`, `path`, `status=PENDING`
- **When** the HostedCluster conditions are manually updated to `Available=True`, `Progressing=False` in envtest
- **And** `GET /api/v1alpha1/clusters/{id}` is called
- **Then** response is 200 with `status=READY`

#### TC-INT-002: Create, Delete, Get(DELETING), Get(404) lifecycle
- **Requirements:** REQ-API-320, REQ-API-340, REQ-API-370, REQ-ACM-140
- **Type:** Integration
- **Priority:** High
- **Given** a cluster exists in envtest (created via POST)
- **When** `DELETE /api/v1alpha1/clusters/{id}` is called
- **Then** response is 204
- **When** `GET /api/v1alpha1/clusters/{id}` is called immediately after DELETE (before resource is fully removed)
- **Then** response is 200 with `status="DELETING"`
- **When** `GET /api/v1alpha1/clusters/{id}` is called after HostedCluster is fully removed
- **Then** response is 404

#### TC-INT-003: List pagination walk
- **Requirements:** REQ-API-240, REQ-API-250, REQ-API-300, REQ-API-315
- **Type:** Integration
- **Priority:** Medium
- **Given** 5 clusters exist in envtest
- **When** `GET /api/v1alpha1/clusters?max_page_size=2` is called
- **Then** response contains 2 results and a `next_page_token`
- **When** subsequent pages are walked using `next_page_token`
- **Then** all 5 clusters are eventually returned
- **And** results across all pages are in `metadata.name` ascending order
- **And** the final page has no `next_page_token`

#### TC-INT-004: Health endpoint with real K8s
- **Requirements:** REQ-HLT-010, REQ-HLT-050
- **Type:** Integration
- **Priority:** High
- **Given** envtest is running (provides a real K8s API server)
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** response is 200 with `status="healthy"`

#### TC-INT-005: RFC 7807 error on actual HTTP request
- **Requirements:** REQ-HTTP-090, REQ-XC-ERR-010
- **Type:** Integration
- **Priority:** Medium
- **Given** the HTTP server is running with real handlers
- **When** `POST /api/v1alpha1/clusters` is sent with `{invalid json`
- **Then** response Content-Type is `application/problem+json`
- **And** the body contains `type`, `title`, `status` fields

#### TC-INT-006: Create BareMetal cluster round-trip
- **Requirements:** REQ-ACM-010, REQ-BM-010, REQ-BM-020, REQ-BM-040
- **Type:** Integration
- **Priority:** Medium
- **Given** envtest is running with HyperShift CRDs and a ClusterImageSet
- **When** `POST /api/v1alpha1/clusters` with BareMetal platform, infra_env, and agent_labels
- **Then** response is 201
- **And** a HostedCluster with `platform.type=Agent` exists in envtest
- **And** a NodePool with InfraEnv reference exists

#### TC-INT-007: Route prefix enforcement
- **Requirements:** REQ-HTTP-020
- **Type:** Integration
- **Priority:** Low
- **Given** the HTTP server is running
- **When** `GET /clusters` is sent (missing `/api/v1alpha1` prefix)
- **Then** response is 404
- **When** `PUT /api/v1alpha1/clusters/some-id` is sent (unsupported method)
- **Then** response is 405

#### TC-INT-008: Platform dispatch end-to-end
- **Requirements:** REQ-KV-010, REQ-BM-010
- **Type:** Integration
- **Priority:** High
- **Given** envtest is running with HyperShift CRDs and a ClusterImageSet
- **When** `POST /api/v1alpha1/clusters` with no `provider_hints.acm.platform` (default)
- **Then** response is 201
- **And** the HostedCluster in envtest has `platform.type=KubeVirt`
- **When** `POST /api/v1alpha1/clusters` with `provider_hints.acm.platform="baremetal"` and `infra_env`
- **Then** response is 201
- **And** the HostedCluster in envtest has `platform.type=Agent`

---

## Coverage Matrix

### Registration Requirements (REQ-REG-xxx)

| Requirement | Test Cases | Status |
|---|---|---|
| REQ-REG-010 | TC-REG-UT-001 | Covered |
| REQ-REG-020 | TC-REG-UT-001 | Covered |
| REQ-REG-030 | TC-REG-IT-004 | Covered |
| REQ-REG-031 | TC-REG-IT-004 | Covered |
| REQ-REG-040 | TC-REG-UT-003, TC-REG-UT-009, TC-REG-UT-010, TC-REG-IT-006 | Covered |
| REQ-REG-050 | TC-REG-UT-003, TC-REG-UT-009 | Covered |
| REQ-REG-051 | TC-REG-UT-003, TC-REG-UT-009, TC-REG-IT-005 | Covered |
| REQ-REG-060 | TC-REG-UT-002 | Covered |
| REQ-REG-070 | TC-REG-UT-011, TC-REG-IT-007 | Covered |
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
| REQ-HTTP-050 | TC-HTTP-UT-006 | Covered |
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
| REQ-API-030 | TC-ERR-UT-001 | Covered (TC-ERR-UT-001 now asserts errType return value) |
| REQ-API-040 | TC-ERR-UT-001 | Covered |
| REQ-API-050 | TC-HDL-CRT-UT-012, TC-ERR-UT-002, TC-OPS-UT-007, TC-OPS-UT-008, TC-OPS-UT-009 | Covered (shared ops cover both platforms) |
| REQ-API-060 | TC-HDL-CRT-UT-001, TC-INT-001 | Covered |
| REQ-API-070 | TC-HDL-CRT-UT-011, TC-HDL-CRT-UT-013 | Covered |
| REQ-API-080 | TC-HDL-CRT-UT-005, TC-HDL-CRT-UT-014 | Covered |
| REQ-API-090 | TC-HDL-CRT-UT-004, TC-HDL-CRT-UT-016 | Covered |
| REQ-API-100 | TC-HDL-CRT-UT-001, TC-HDL-CRT-UT-002, TC-HDL-CRT-UT-015 | Covered |
| REQ-API-101 | TC-HDL-CRT-UT-003 | Structural (IMPL-001: enforced by OpenAPI middleware) |
| REQ-API-102 | TC-HDL-CRT-UT-007, TC-KV-UT-014 | Covered |
| REQ-API-103 | TC-HDL-CRT-UT-008, TC-KV-UT-015 | Covered |
| REQ-API-104 | TC-HDL-CRT-UT-002, TC-HDL-CRT-UT-003 | Covered |
| REQ-API-110 | TC-HDL-CRT-UT-001, TC-INT-001 | Covered |
| REQ-API-130 | TC-HDL-CRT-UT-009 | Covered |
| REQ-API-140 | TC-HDL-CRT-UT-010 | Covered |
| REQ-API-150 | TC-HDL-CRT-UT-011, TC-HDL-CRT-UT-013, TC-HDL-CRT-UT-015, TC-HDL-CRT-UT-016 | Covered |
| REQ-API-160 | TC-HDL-CRT-UT-003 | Structural (IMPL-001: enforced by OpenAPI middleware) |
| REQ-API-165 | TC-OPS-UT-001 | Covered |
| REQ-API-166 | TC-OPS-UT-016 | Covered (status_message from Degraded condition) |
| REQ-API-167 | TC-OPS-UT-017 | Covered (status_message from Available condition) |
| REQ-API-170 | TC-HDL-CRT-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018 | Covered |
| REQ-API-175 | Structural (OpenAPI pattern + validation middleware) | Verified by OpenAPI spec |
| REQ-API-180 | TC-HDL-GET-UT-001 | Covered |
| REQ-API-190 | TC-HDL-GET-UT-001, TC-HDL-GET-UT-002 | Covered |
| REQ-API-200 | TC-HDL-GET-UT-003 | Covered |
| REQ-API-210 | TC-HDL-GET-UT-004, TC-HDL-DEL-UT-006 | Covered |
| REQ-API-220 | TC-HDL-GET-UT-001 | Covered |
| REQ-API-230 | TC-HDL-GET-UT-002 | Covered |
| REQ-API-231 | TC-HDL-GET-UT-005, TC-OPS-UT-013 | Covered |
| REQ-API-240 | TC-HDL-LST-UT-001, TC-HDL-LST-UT-006 | Covered |
| REQ-API-250 | TC-HDL-LST-UT-001, TC-INT-003 | Covered |
| REQ-API-260 | TC-HDL-LST-UT-001 | Covered |
| REQ-API-270 | TC-HDL-LST-UT-002, TC-HDL-LST-UT-007 | Covered |
| REQ-API-280 | TC-HDL-LST-UT-003, TC-HDL-LST-UT-007 | Covered |
| REQ-API-290 | TC-HDL-LST-UT-004, TC-HDL-LST-UT-008 | Covered |
| REQ-API-291 | TC-HDL-LST-UT-004 | Covered |
| REQ-API-300 | TC-HDL-LST-UT-005, TC-HDL-LST-UT-006, TC-INT-003 | Covered |
| REQ-API-310 | TC-HDL-LST-UT-001 (implicit), TC-OPS-UT-009 | Covered |
| REQ-API-315 | TC-OPS-UT-012, TC-INT-003 | Covered |
| REQ-API-320 | TC-HDL-DEL-UT-001, TC-INT-002 | Covered |
| REQ-API-330 | TC-HDL-DEL-UT-001 | Covered |
| REQ-API-340 | TC-HDL-DEL-UT-002, TC-INT-002 | Covered |
| REQ-API-350 | TC-HDL-DEL-UT-001 | Covered |
| REQ-API-360 | TC-HDL-DEL-UT-003 | Covered |
| REQ-API-370 | TC-HDL-DEL-UT-004, TC-HDL-DEL-UT-005 | Covered |
| REQ-API-380 | TC-KV-UT-006, TC-KV-UT-020, TC-BM-UT-010, TC-BM-UT-011 | Covered |
| REQ-API-390 | TC-OPS-UT-006, TC-OPS-UT-001, TC-OPS-UT-013 | Covered (shared ops cover both platforms) |

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
| REQ-ACM-060 | TC-KV-UT-003 | Covered |
| REQ-ACM-070 | TC-KV-UT-002 | Covered |
| REQ-ACM-080 | TC-KV-UT-002 | Covered |
| REQ-ACM-090 | TC-KV-UT-001, TC-BM-UT-001 | Covered |
| REQ-ACM-100 | TC-KV-UT-001 | Covered |
| REQ-ACM-110 | TC-STS-UT-001..005, TC-STS-UT-011, TC-OPS-UT-001, TC-OPS-UT-002, TC-OPS-UT-007, TC-OPS-UT-010, TC-OPS-UT-013 | Covered (shared ops cover both platforms) |
| REQ-ACM-120 | TC-OPS-UT-001 | Covered |
| REQ-ACM-130 | TC-OPS-UT-001, TC-OPS-UT-005, TC-OPS-UT-011 | Covered |
| REQ-ACM-140 | TC-OPS-UT-004, TC-OPS-UT-008, TC-INT-002 | Covered (shared delete via TC-OPS-UT-004) |
| REQ-ACM-150 | TC-KV-UT-008 | Covered |
| REQ-ACM-160 | TC-STS-UT-001..012, TC-OPS-UT-001 | Covered (shared ops confirm delegation to shared mapper) |
| REQ-ACM-170 | TC-KV-UT-017, TC-BM-UT-008 | Covered |

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
| REQ-XC-ERR-020 | TC-ERR-UT-001 | Covered (TC-ERR-UT-001 now asserts type, title, and status) |
| REQ-XC-ERR-030 | TC-ERR-UT-003 | Covered |
| REQ-XC-ERR-040 | TC-ERR-UT-002, TC-HDL-CRT-UT-012 | Covered |
| REQ-XC-LBL-010 | TC-KV-UT-001, TC-BM-UT-001 | Covered |
| REQ-XC-LBL-020 | TC-KV-UT-001 | Covered |
| REQ-XC-LBL-030 | TC-MON-UT-008 | Covered |
| REQ-XC-K8S-010 | Structural | Verified by go.mod dependency |
| REQ-XC-K8S-020 | Deferred (SP_HUB_KUBECONFIG investigation needed) | Deferred |
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
- **1 KubeVirt test** (TC-OPS-UT-001) confirms the KubeVirt service extracts READY-specific fields using the shared mapper
- **Shared ops tests** (TC-OPS-UT-001) confirm both platforms delegate to the shared mapper

This reduces 15+ potential duplicate tests to 12 without losing coverage.

### Shared Code Beyond Status Mapping

| Shared Behavior | Tested Via (KubeVirt) | BareMetal Confirmation |
|---|---|---|
| Version validation (ClusterImageSet lookup) | TC-KV-UT-004 | Shared code; TC-BM-UT-001 implicitly confirms |
| Conflict detection (duplicate `id` by label) | TC-KV-UT-014 | Shared code |
| Conflict detection (duplicate `metadata.name`) | TC-KV-UT-015 | Shared code |
| `console_uri` construction | TC-OPS-UT-006 | Shared ops (TC-OPS-UT-001 confirms via READY status) |
| `base_domain` handling (config default + request override) | TC-KV-UT-006 | TC-BM-UT-010 (new) |
| Memory/storage format conversion | TC-KV-UT-008 | N/A for BareMetal (informational per REQ-BM-060) |
| List ordering (`metadata.name` ascending) | TC-OPS-UT-012 | Shared code; no platform-specific sort |

### Spec ACs Merged into Fewer Test Cases

| Spec AC(s) | Merged Into TC | Rationale |
|---|---|---|
| AC-REG-010, AC-REG-030 | TC-REG-UT-001 | Payload and capabilities are naturally tested together |
| AC-REG-040 | TC-REG-UT-003 | Retry + fatal exit are the same failure path |
| AC-ACM-110, AC-ACM-160 | TC-STS-UT-001..012 | All status mapping consolidated to shared component |
| AC-API-010, AC-API-011 | TC-HDL-CRT-UT-001, TC-HDL-CRT-UT-002 | Server vs client ID are two sides of the same feature |
| AC-API-070 | TC-HDL-CRT-UT-003 | Read-only field test covers all 9 read-only fields at once |
| AC-API-080, AC-API-090 | TC-HDL-GET-UT-001, TC-HDL-GET-UT-002 | READY vs not-READY are complementary |
| AC-API-130, AC-API-140 | TC-HDL-DEL-UT-001, TC-HDL-DEL-UT-002 | Happy and error path for delete |

### Eliminated Redundancy

- **REQ-API-310** (List results from K8s): Handler just passes through; real K8s sourcing tested in service layer
- **REQ-ACM-110 / REQ-ACM-160**: Tested ONCE in shared mapper (TC-STS-UT-xxx), NOT per platform
- **AC-API-150 / AC-ACM-140**: DELETING status tested once in TC-STS-UT-005; handler pass-through in TC-HDL-DEL-UT-005
- **REQ-API-315** (List ordering): Sorting is service-layer concern; handler passes through. Tested once via KubeVirt service (TC-OPS-UT-012)

---

## Summary Statistics

| Category | Count |
|---|---|
| **Total integration test cases** | **29** |
| High priority | 10 |
| Medium priority | 12 |
| Low priority | 7 |
| Structural (no behavioral test) | 10 requirements |
| Total requirements covered (across both unit and integration) | 165 (all REQ-xxx IDs) |
| Coverage gaps | **2 deferred** (TC-CFG-UT-003 pending investigation, REQ-HLT-010/REQ-HLT-120 integration-covered only) |

### Test Cases by Component

| Component | Test Case Prefix | Count |
|---|---|---|
| Registration | TC-REG-IT-xxx | 4 |
| HTTP Server | TC-HTTP-IT-xxx | 10 |
| Health Service | TC-HLT-IT-xxx | 1 |
| Status Monitoring | TC-MON-IT-xxx | 6 |
| Integration | TC-INT-xxx | 8 |

### Notes

- Test cases are numbered sequentially within each prefix and MUST NOT be renumbered after approval
- Integration tests require `//go:build integration` build tag
- Test helpers in `internal/testutil/` are shared across all test packages

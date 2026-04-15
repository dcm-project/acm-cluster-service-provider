# CLAUDE.md

## Project Overview

The **ACM Cluster Service Provider** is a REST API that manages OpenShift cluster lifecycle using Red Hat Advanced Cluster Management (ACM) with HyperShift (Hosted Control Planes). It integrates with the DCM (Distributed Cloud Management) ecosystem as a registered service provider, exposing cluster CRUD endpoints, reporting health, and publishing status changes via CloudEvents over NATS.

- **Language:** Go 1.25.5
- **License:** Apache 2.0
- **Organization:** [dcm-project](https://github.com/dcm-project)
- **Container base:** UBI9 (`registry.access.redhat.com/ubi9`)

## Build & Test

```bash
make build              # compile to bin/acm-cluster-service-provider
make run                # go run
make test               # Ginkgo test suite (--race)
make test-cover         # with coverage
make lint               # golangci-lint (config: .golangci.yml)
make vet                # go vet
make fmt                # gofmt -s
make check              # fmt + vet + lint + test
make generate-api       # regenerate all code from OpenAPI spec
make check-generate-api # verify generated code is up-to-date (used in CI)
make check-aep          # OpenAPI AEP compliance via Spectral (.spectral.yaml)
make tidy               # go mod tidy
make clean              # rm -rf bin/
```

### CI/CD (GitHub Actions)

| Workflow | Purpose |
|----------|---------|
| `ci.yaml` | Runs on push to `main` and PRs. Uses `dcm-project/shared-workflows` shared Go CI. Post-test: `vet`. |
| `lint.yaml` | golangci-lint |
| `check-generate.yaml` | Verifies generated code is in sync with OpenAPI spec |
| `check-aep.yaml` | Spectral AEP compliance linting on the OpenAPI spec |
| `check-clean-commits.yaml` | Commit hygiene checks |
| `build-push-quay.yaml` | Build container image and push to Quay |

## Code Generation

Types, server, spec, and client are generated from `api/v1alpha1/openapi.yaml` via [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). Always run `make generate-api` after modifying the OpenAPI spec. **Never edit `*.gen.go` files by hand.**


### Dual-Package Type System

Both `api/v1alpha1/` and `internal/api/server/` are generated from the **same OpenAPI spec** but produce **different Go types**:

- **`api/v1alpha1/`** (alias: `v1alpha1`) — Domain/service layer types. Used by `internal/service/`, `internal/cluster/`, `internal/handler/` for business logic.
- **`internal/api/server/`** (alias: `oapigen`) — HTTP handler layer types. Used for request/response objects in the strict server interface.

The handler converts between the two via **JSON roundtrip** (`internal/handler/convert.go`). Tests must use `oapigen.*` for request/response bodies and `v1alpha1.*` for service layer mocks.

### Enum Naming

oapi-codegen uses type-prefix disambiguation for enum constants:
- Status: `ClusterStatusPENDING`, `ClusterStatusREADY`, `ClusterStatusFAILED`, etc.
- Error types: `ErrorTypeINTERNAL`, `ErrorTypeNOTFOUND`, `ErrorTypeINVALIDARGUMENT`, etc.
- **Exception — Platform:** `v1alpha1.Kubevirt`, `v1alpha1.Baremetal` (unprefixed)

## Architecture

### Directory Structure

```
cmd/acm-cluster-service-provider/
  main.go                          # Entry point, wiring, graceful shutdown

api/v1alpha1/
  openapi.yaml                     # Source of truth — OpenAPI 3.0.4 spec
  types.gen.go                     # Generated domain types
  spec.gen.go                      # Generated embedded spec

internal/
  api/server/
    server.gen.go                  # Generated Chi server + strict interface

  apiserver/
    server.go                      # HTTP server (Chi, kin-openapi validation)
    middleware.go                   # Recovery, logging, timeout, OpenAPI validation

  handler/
    handler.go                     # StrictServerInterface impl, delegates to services
    convert.go                     # oapigen <-> v1alpha1 JSON roundtrip conversion
    errors.go                      # Domain error -> RFC 7807 mapping
    validation.go                  # Request validation (service_type, memory format, ID)

  service/
    interfaces.go                  # ClusterService, HealthChecker interfaces
    errors.go                      # DomainError type (typed, maps to ErrorType enum)
    status/
      mapper.go                    # MapConditionsToStatus() — HostedCluster conditions -> DCM status
      nodepool_mapper.go           # MapNodePoolConditionsToStatus() — NodePool conditions -> DCM status
      composite.go                 # ComputeCompositeStatus() — worst-of HC + NP status

  cluster/
    labels.go                      # DCM label constants and builder
    operations.go                  # Platform-agnostic Get, List, Delete (K8s client)
    convert.go                     # HostedCluster -> v1alpha1.Cluster conversion
    create.go                      # Shared create helpers
    create_helpers.go              # HostedCluster/NodePool builder utilities
    version.go                     # Release image <-> K8s version mapping
    pullsecret.go                  # Shared PullSecret Secret lifecycle
    clustertest/helpers.go         # Shared test fixtures (BuildFakeClient, BuildHostedCluster)
    dispatcher/dispatcher.go       # Routes Create to kubevirtprovider/ or baremetal/
    kubevirtprovider/              # KubeVirt platform Create implementation
    baremetal/                     # BareMetal platform Create implementation

  config/
    config.go                      # All config via env vars (caarlos0/env/v11)

  health/
    health.go                      # HealthChecker: K8s API + CRD probes

  registration/
    registration.go                # DCM SP Registry registration + retry
    version.go                     # CompatibilityMatrix, VersionDiscoverer (ClusterImageSets)

  monitoring/
    monitor.go                     # StatusMonitor: dynamic informers for HC + NP
    reconcile.go                   # Reconciliation state machine
    debouncer.go                   # Event debouncing
    indexer.go                     # Resource indexing
    publisher.go                   # StatusPublisher interface + NATSPublisher impl
    event.go                       # CloudEvent construction
    config.go                      # MonitorConfig struct

  util/
    ptr.go                         # Ptr[T]() — generic pointer helper
    gvk.go                         # Pre-built GVK/GVR constants (HyperShift, Hive)

pkg/client/
  client.gen.go                    # Generated HTTP client

hack/
  deploy-acm-mce.sh               # ACM/MCE deployment helper script
```

### Architectural Style

The service follows a **layered architecture with interface-based injection**:

```
HTTP Request
    │
    ▼
┌─────────────────────────────────────┐
│  OpenAPI Validation Middleware       │  kin-openapi (request validation)
│  Recovery / Logging / Timeout        │
└──────────────┬──────────────────────┘
               │
    ▼
┌─────────────────────────────────────┐
│  Handler (StrictServerInterface)     │  oapigen types, validation, error mapping
│  handler.go, convert.go, errors.go   │
└──────────────┬──────────────────────┘
               │  ClusterService / HealthChecker interfaces
    ▼
┌─────────────────────────────────────┐
│  Dispatcher                          │  Routes Create by platform
│  ├── KubeVirt Provider               │
│  └── BareMetal Provider              │
│  Shared: Get, List, Delete           │  Platform-agnostic operations
└──────────────┬──────────────────────┘
               │  client.Client (controller-runtime)
    ▼
┌─────────────────────────────────────┐
│  K8s API (ACM Hub)                   │  Source of truth
│  HostedCluster + NodePool CRDs       │  HyperShift API
└─────────────────────────────────────┘
```

### Internal Interfaces (Testability Seams)

| Interface | Package | Purpose |
|-----------|---------|---------|
| `ClusterService` | `internal/service` | CRUD operations on clusters. Platform dispatch behind it. |
| `HealthChecker` | `internal/service` | Dependency health probes. Returns `v1alpha1.Health`. |
| `StatusPublisher` | `internal/monitoring` | Decouples event detection (informer) from delivery (NATS). |

The K8s client (`sigs.k8s.io/controller-runtime/pkg/client.Client`) is an existing ecosystem interface injected as a constructor dependency. Tests use `controller-runtime/pkg/client/fake`.

### Data Flow

- **Reads** (GET): Handler -> ClusterService -> K8s `client.Client` -> HostedCluster CR -> v1alpha1.Cluster
- **Writes** (POST): Handler -> Dispatcher -> Platform Provider -> K8s `client.Client` -> HostedCluster + NodePool CRs
- **Deletes** (DELETE): Handler -> ClusterService -> K8s `client.Client` -> Delete NodePools + HostedCluster
- **Health** (GET): Handler -> HealthChecker -> K8s `client.Client` -> CRD availability probes
- **Status**: Dynamic Informer -> Reconciler -> Debouncer -> StatusPublisher -> NATS CloudEvents

### Key Design Decisions

Documented in `.ai/decisions/design-decisions.md`:

| ID | Decision |
|----|----------|
| DD-001 | K8s minor version format enforced by SP via compatibility matrix |
| DD-002 | Health status: `"healthy"` / `"unhealthy"` (not `"pass"`) |
| DD-003 | Provider name (`SP_NAME`) as unique identifier, not registry ID |
| DD-004 | CP resources mapped via HyperShift annotation overrides |
| DD-005 | All 4 control-plane services with default strategies |
| DD-006 | NodePool management: InPlace upgrade type |
| DD-007 | PullSecret: shared Secret from env var, created at startup |
| DD-008 | Etcd: Managed with PersistentVolume storage |

### Error Handling

- **Domain errors**: `service.DomainError` with typed `ErrorType` (from OpenAPI enum)
- **Error constructors**: `NewNotFoundError`, `NewAlreadyExistsError`, `NewInvalidArgumentError`, `NewUnprocessableEntityError`, `NewInternalError`, `NewUnavailableError`
- **Error assertion**: Use `errors.As(err, &domainErr)` — never `BeAssignableToTypeOf`
- **HTTP errors**: RFC 7807 `application/problem+json` format via `handler.MapDomainError()`
- **Mapping**: ErrorType -> HTTP status (400, 404, 409, 422, 500, 503) in `handler/errors.go`

### Status Mapping

`internal/service/status/` provides shared, pure-function status mappers:

**HostedCluster** (`MapConditionsToStatus`):
1. `deletionTimestamp` set -> DELETING
2. `Degraded=True` -> FAILED
3. `Available=True` -> READY
4. `Progressing=True` -> PROVISIONING
5. `Available=False AND Progressing=False` -> UNAVAILABLE
6. default -> PENDING

**NodePool** (`MapNodePoolConditionsToStatus`):
1. `UpdatingVersion=True` or `UpdatingConfig=True` -> PROVISIONING
2. `Ready=False` -> UNAVAILABLE
3. `Ready=True` -> READY
4. default -> PENDING

**Composite** (`ComputeCompositeStatus`): worst-of HostedCluster and NodePool statuses.

### Middleware Stack (Chi router)

Applied in order:
1. `rfc7807RecoveryMiddleware` — panic recovery, RFC 7807 error response
2. `requestLoggingMiddleware` — structured logging (method, path, status, duration)
3. `openAPIValidationMiddleware` — kin-openapi request validation against embedded spec
4. `requestTimeoutMiddleware` — context deadline per request

### Platform Dispatch

The `dispatcher.Service` implements `ClusterService`. Only `Create` is platform-specific:
- **KubeVirt**: Creates HostedCluster with `KubevirtPlatform` + KubeVirt NodePool
- **BareMetal**: Creates HostedCluster with `AgentBaremetalPlatform` + Agent NodePool

`Get`, `List`, `Delete` are shared and platform-agnostic (in `internal/cluster/operations.go`).

Default platform is `Kubevirt` (per OpenAPI spec default).

### DCM Labels

All managed K8s resources are labeled with:
```
dcm.project/managed-by: dcm
dcm.project/dcm-instance-id: <uuid>
dcm.project/dcm-service-type: cluster
```

Defined in `internal/cluster/labels.go`. Used for lookup, listing, filtering, and informer label selectors.

## Development Methodology

### Spec-Driven BDD Development

This project follows a strict **Spec -> Test Plan -> Tests -> Implementation** workflow. All artifacts live under `.ai/`:

```
.ai/
├── specs/              # Feature specifications (Given/When/Then, REQ-xxx, AC-xxx)
├── test-plans/         # Test plans with TC-xxx IDs, traceability to specs
├── decisions/          # Design (DD-xxx), test (TD-xxx), implementation (IMPL-xxx) decisions
├── plans/              # Implementation plans (per-topic, timestamped)
├── checkpoints/        # Session state snapshots
├── reviews/            # Code reviews and gap analyses
└── research/           # External research and analysis
```

**Only `specs/`, `test-plans/`, and `decisions/` are tracked in git** (`.gitignore` uses `!.ai/specs/` etc. negation). All other subdirectories are local-only.

### Specification Structure

The spec file (`.ai/specs/acm-cluster-sp.spec.md`) contains:
- **Requirements**: `REQ-{TOPIC}-{NUMBER}` (e.g., `REQ-REG-010`, `REQ-ACM-030`) with priority (MUST/SHOULD)
- **Acceptance Criteria**: `AC-{TOPIC}-{NUMBER}` tied to requirements
- **Decision Log**: `DD-xxx` (design), `TD-xxx` (test), `IMPL-xxx` (implementation)
- **Architecture diagram**, topic dependency graph, internal interface contracts

### Test Plan Structure

Two test plan files:
- **Unit tests** (`.ai/test-plans/acm-cluster-sp.unit-tests.md`): 172 test cases
- **Integration tests** (`.ai/test-plans/acm-cluster-sp.integration-tests.md`)

Test case IDs follow `TC-{COMPONENT}-{TYPE}-{NUMBER}`:
- `TC-HDL-CRT-UT-001` — Handler Create Unit Test #1
- `TC-REG-IT-004` — Registration Integration Test #4
- `TC-STS-UT-001` — Status mapper Unit Test #1
- `TC-MON-UT-009` — Monitoring Unit Test #9

Every TC maps to a REQ-xxx and AC-xxx from the spec. Zero gaps, zero redundancy.

### Test Architecture (Layered)

```
         Integration tests    ~7 tests (httptest, no mocks, full round-trip)
        ─────────────────────
         Service tests        ~25 tests (fake K8s client, no HTTP)
        ─────────────────────
         Handler tests        ~27 tests (mocked ClusterService/HealthChecker)
        ─────────────────────
         Shared components    ~11 table-driven tests (pure functions)
```

**Layer isolation principles:**
- Handler tests mock `ClusterService`/`HealthChecker` interfaces
- Service tests use fake K8s client (`controller-runtime/pkg/client/fake`)
- Integration tests use `httptest` with no mocks
- Status mapping tested once in shared component, never duplicated per platform
- No behavioral scenario tested twice at the same layer

### Test Patterns

- **External test packages**: `package handler_test`, `package registration_test`, etc.
- **Suite bootstrap**: Separate `_test.go` with `TestXxx(t) { RegisterFailHandler(Fail); RunSpecs(t, "Suite") }`
- **File naming**: `*_unit_test.go` for unit tests, `*_integration_test.go` for integration tests
- **Functional mocks**: Struct with `XxxFunc` fields, panics if not set (no nil-return surprises)
- **Test helpers**: `internal/cluster/clustertest/helpers.go` (shared `BuildFakeClient`, `BuildHostedCluster`, `DefaultConfig`, functional options via `HCOption`)
- **Assertions**: Gomega matchers (`Expect(x).To(Equal(y))`), `errors.As` for domain errors
- **Async testing**: `Eventually()` for informer-based tests
- **Concurrent test safety**: `atomic` types for request counting in mock servers

### Topic-Based Development

Work is organized into topics with clear dependencies:

| Topic | Area | Dependencies |
|-------|------|-------------|
| 1 | DCM Registration | Independent |
| 2 | HTTP Server | Independent |
| 3 | Health Service | Topic 2 |
| 4 | OpenAPI Endpoints (Handler) | Topic 2 |
| 5a | KubeVirt Platform | Topic 4 |
| 5b | BareMetal Platform | Topic 4 |
| 6 | Status Monitoring | Topics 1, 5 |

Each topic follows RED (write failing tests) -> GREEN (implement) -> REFACTOR phases.


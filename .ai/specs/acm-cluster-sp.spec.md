# ACM Cluster Service Provider — Specification

> **Status**: Draft
> **Created**: 2026-02-13
> **Last Updated**: 2026-04-02 (PullSecret strategy change: shared Secret from env var at startup, replaces per-cluster lifecycle — DD-007 revised) | 2026-04-01 (review fix: PullSecret Secret naming `<cluster-name>-pull-secret`, labeling per REQ-ACM-020, label-based lookup for deletion) | 2026-04-01 (HostedCluster required fields: Services, PullSecret, Management; PullSecret aligned with catalog-manager PR #59 — top-level required field)
> **Authors**: @gabriel-farache (with Claude Code)

## 1. Overview

The ACM Cluster Service Provider is a REST API that manages OpenShift cluster lifecycle using Red Hat Advanced Cluster Management (ACM) with HyperShift (Hosted Control Planes). It integrates with the DCM ecosystem by registering as a service provider, exposing cluster CRUD endpoints, reporting health, and publishing status changes via CloudEvents over NATS.

### Reference Documents

- Enhancement: [acm-cluster-sp.md](https://github.com/dcm-project/enhancements/blob/main/enhancements/acm-cluster-sp/acm-cluster-sp.md)
- Registration: [sp-registration-flow.md](https://github.com/dcm-project/enhancements/blob/main/enhancements/sp-registration-flow/sp-registration-flow.md)
- Health: [service-provider-health-check.md](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-health-check/service-provider-health-check.md)
- Status Reporting: [service-provider-status-report-implementation.md](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-status-report-implementation/service-provider-status-report-implementation.md)
- Cluster Service Type: [catalog-manager/api/v1alpha1/servicetypes/cluster/spec.yaml](https://github.com/dcm-project/catalog-manager/blob/main/api/v1alpha1/servicetypes/cluster/spec.yaml)

### Existing Codebase

- OpenAPI spec: `api/v1alpha1/openapi.yaml`
- Generated types: `api/v1alpha1/types.gen.go`
- Generated server (Chi v5 + StrictServerInterface): `internal/api/server/server.gen.go`
- Generated client: `pkg/client/client.gen.go`
- Entry point (stub): `cmd/acm-cluster-service-provider/main.go`

### Decision Log

| ID | Type | Decision | Rationale | Details |
|----|------|----------|-----------|---------|
| DD-001 | Design | **K8s minor version format enforced by SP:** The SP advertises K8s minor versions derived from ClusterImageSets via compatibility matrix. Callers MUST use exactly one of the advertised versions. | Platform-agnostic K8s versions; compatibility matrix owned by SP. | `.ai/decisions/design-decisions.md` |
| DD-002 | Design | **Health status values:** `"healthy"` / `"unhealthy"` instead of `"pass"` from the enhancement. | Aligns with AEP conventions and provides clearer semantics. | `.ai/decisions/design-decisions.md` |
| DD-003 | Design | **Provider name as unique identifier:** `SP_NAME` used for CloudEvents subjects and DCM correlation. Provider ID from registry not persisted. | Persisting the ID adds complexity with no identified use case. | `.ai/decisions/design-decisions.md` |
| DD-004 | Design | **CP resources mapped via annotation overrides targeting kube-apiserver and etcd.** Each component gets full specified values (total = 2x stated values). | Annotations are HyperShift's official per-cluster resource override mechanism. | `.ai/decisions/design-decisions.md` |
| DD-005 | Design | **Services — All 4 control-plane services with default strategies.** SP sets 4 `ServicePublishingStrategyMapping` entries: `APIServer/LoadBalancer`, `OAuthServer/Route`, `Konnectivity/Route`, `Ignition/Route`. | CRD `Spec.Services` is `+required`; ACM expects all 4 services declared; adding them costs nothing. | `.ai/decisions/design-decisions.md` |
| DD-006 | Design | **NodePool Management — InPlace upgrade type (v1 opinionated).** SP sets `Spec.Management.UpgradeType=InPlace` for all platforms. | CRD `Spec.Management.UpgradeType` is `+required`; InPlace is simpler and avoids node churn. | `.ai/decisions/design-decisions.md` |
| DD-007 | Design | **PullSecret — Shared Secret from env var, created at startup.** SP creates a single shared PullSecret Secret (`<SP_NAME>-pull-secret`) at startup from `SP_PULL_SECRET` env var. All HostedClusters reference it. No per-cluster Secret lifecycle. | CRD `Spec.PullSecret` is `+required`; shared Secret eliminates per-cluster complexity; env var aligns with operational deployment patterns. | `.ai/decisions/design-decisions.md` |
| IMPL-001 | Implementation | **ReadOnly field stripping removed from handler:** Trusts OpenAPI validation middleware (`VisitAsRequest()`) to reject readOnly properties in requests. | Single point of defense at the middleware boundary; handler does not re-validate. | `.ai/decisions/implementation-decisions.md` |

## 2. Scope

### 2.1 In Scope (v1)

- **Provisioning method**: HyperShift only (HostedCluster + NodePool CRDs)
- **Platforms**: KubeVirt (VMs on OpenShift Virtualization) and BareMetal (Agent provider with Assisted Installer)
- **Operations**: CREATE, READ (Get, List), DELETE
- **DCM integration**: registration, health endpoint, status monitoring via CloudEvents/NATS
- **K8s as source of truth**: cluster state read from HostedCluster CRs via labels and informer cache

### 2.2 Out of Scope (v1)

- UPDATE endpoint / Day-2 operations (scale, upgrade, hibernate, resume)
- Cluster import
- Hive-based provisioning
- Cloud providers (AWS, OpenStack, etc.)
- ACM policies, governance, or observability features
- Multi-cluster workload distribution
- Authentication/authorization on SP endpoints (deferred)

## 3. Architecture

```
                    DCM Control Plane
                    +--------------------------+
                    |  SP Registry             |<-- Registration (Topic 1)
                    |  Health Poller            |--> Health (Topic 3)
                    |  SP Resource Manager      |--> OpenAPI Endpoints (Topic 4)
                    |  NATS Messaging           |<-- Status Monitoring (Topic 6)
                    +--------------------------+

                    ACM Cluster Service Provider
                    +------------------------------------------------+
                    |  HTTP Server (Topic 2)                          |
                    |  +------------------------------------------+  |
                    |  | OpenAPI Handlers (Topic 4)                |  |
                    |  |  - CreateCluster                          |  |
                    |  |  - GetCluster                             |  |
                    |  |  - ListClusters                           |  |
                    |  |  - DeleteCluster                          |  |
                    |  |  - GetHealth (Topic 3)                    |  |
                    |  +------+-------------------+---------------+  |
                    |         |                   |                   |
                    |  ·······|·ClusterService·····|·HealthChecker··  |
                    |  ·      |    interface    ·  |  interface    ·  |
                    |  ·······|·················+··|···············+  |
                    |         |                 |  |               |  |
                    |  +------v-----------+    |  +---v--------+  |  |
                    |  | KubeVirt (T5a)    |    |  | HealthCheck|  |  |
                    |  | BareMetal (T5b)   |    |  | impl       |  |  |
                    |  +------+-----------+    |  +---+--------+  |  |
                    |         |                |      |            |  |
                    |  ·······|·····client.Client······|···········+  |
                    |  ·      |   (injected dep)      |           ·  |
                    |  ·······|·······················+|···········+  |
                    |         |                       |               |
                    |  +------v-----------------------v-----------+  |
                    |  | K8s API (ACM Hub) — source of truth       |  |
                    |  +------------------------------------------+  |
                    |                                                 |
                    |  Status Monitor (Topic 6)                       |
                    |  SharedIndexInformer (via client.Client)         |
                    |         |                                       |
                    |  ·······|···StatusPublisher·interface············|
                    |         |                                       |
                    |  +------v-----------+                           |
                    |  | NATS Publisher    |                           |
                    |  |  impl            |                           |
                    |  +------------------+                           |
                    +-------------------------------------------------+

    ······· = interface boundary (testability seam)
```

### Data Flow

- **Reads** (GET): Handler -> `ClusterService` -> K8s `client.Client` -> response
- **Writes** (POST): Handler -> `ClusterService` -> K8s `client.Client` -> response
- **Health** (GET): Handler -> `HealthChecker` -> K8s `client.Client` -> response
- **Deletes** (DELETE): Handler -> `ClusterService` -> K8s `client.Client` -> response
- **Status**: Informer (K8s `client.Client`) -> `StatusPublisher` -> NATS

### Internal Interfaces

The architecture defines three internal interfaces that serve as testability seams. These are the contracts between layers; implementations are injected at construction time.

> **Note:** The interface definitions below are a **reference design**, not normative. The behavioral contract (what each interface does) is what matters; exact method signatures may evolve during implementation.

#### ClusterService

Operates on DCM domain types (`api/v1alpha1` generated types). Platform dispatch (KubeVirt vs BareMetal) happens behind this interface. The handler layer depends only on this interface and has no knowledge of K8s.

```go
type ClusterService interface {
    Create(ctx context.Context, id string, cluster api.Cluster) (*api.Cluster, error)
    Get(ctx context.Context, id string) (*api.Cluster, error)
    List(ctx context.Context, pageToken string, pageSize int) (*api.ClusterList, error)
    Delete(ctx context.Context, id string) error
}
```

> **Note:** The handler resolves the `id` before calling `Create`: it reads `?id=` from the query parameter or generates a UUID if absent. `Get` and `Delete` receive the `id` from the URL path parameter (`/clusters/{clusterId}`).

#### HealthChecker

Used by the health endpoint handler. The implementation probes K8s API connectivity and CRD availability.

```go
type HealthChecker interface {
    Check(ctx context.Context) api.Health
}
```

#### StatusPublisher

Decouples event detection (informer) from event delivery (NATS). The Status Monitor depends on this interface, not on a NATS client directly.

```go
type StatusPublisher interface {
    Publish(ctx context.Context, event cloudevents.Event) error
}
```

> **Note — K8s client:** The K8s client itself (`sigs.k8s.io/controller-runtime/pkg/client.Client`) is an existing interface from the ecosystem and does not need a custom abstraction. It is injected as a constructor dependency into service implementations. The `controller-runtime` library provides a fake implementation (`sigs.k8s.io/controller-runtime/pkg/client/fake`) suitable for unit testing.

## 4. Topic Dependency Graph

```
Topic 1: DCM Registration      (independent)
Topic 2: HTTP Server            (independent)
Topic 3: Health Service         <- depends on Topic 2
Topic 4: OpenAPI Endpoints      <- depends on Topic 2
Topic 5: ACM Platform Services  <- depends on Topic 4
  Common Behaviors              (shared by 5a and 5b)
  5a: KubeVirt                  (platform-specific)
  5b: BareMetal                 (platform-specific)
Topic 6: Status Monitoring      <- depends on Topics 1, 5
```

Topics 1 and 2 can be delivered in parallel. Topics 5a and 5b can be delivered in parallel after common behaviors are established.

---

## 5. Topic Specifications

---

### 5.1 Topic 1: DCM Registration

#### Overview

On startup, the SP registers itself with the DCM SP Registry by calling `POST /api/v1alpha1/providers`. Registration is per-service-type, idempotent by name, and follows [`sp-registration-flow.md`](https://github.com/dcm-project/enhancements/blob/main/enhancements/sp-registration-flow/sp-registration-flow.md).

> **Note (DD-003):** The provider name (`SP_NAME`) is the unique identifier used for CloudEvents subjects and DCM correlation. The SP does not persist the provider ID returned by the registry. The administrator is responsible for ensuring name uniqueness across SP instances.

#### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-REG-010 | The SP MUST send a `POST {DCM_REGISTRATION_URL}/providers` request to the DCM SP Registry on startup | MUST |
| REQ-REG-020 | The registration payload MUST include: `service_type` ("cluster"), `endpoint`, and `operations` at top level; `display_name` SHOULD be included when configured. Provider metadata nested under `metadata` SHOULD include `region` and `zone` when configured | MUST |
| REQ-REG-030 | Registration MUST execute asynchronously in a background goroutine | MUST |
| REQ-REG-031 | Registration MUST NOT block HTTP server startup; the server MUST accept requests before registration completes | MUST |
| REQ-REG-040 | The SP MUST retry registration indefinitely with exponential backoff (starting at `SP_REGISTRATION_INITIAL_BACKOFF`, capped at `SP_REGISTRATION_MAX_BACKOFF`) until success or context cancellation. Non-retryable errors (4xx) MUST cause immediate failure without further retries | MUST |
| REQ-REG-050 | Registration failures MUST be logged with sufficient detail for operator diagnosis, including truncated response body content (max 200 chars) when available | MUST |
| REQ-REG-051 | Registration failures MUST NOT cause the SP process to exit; the SP MUST continue serving requests. Non-retryable errors (4xx) MUST stop retries immediately. Retryable errors MUST be retried indefinitely until success or context cancellation | MUST |
| REQ-REG-060 | Registration MUST be idempotent -- re-registration with the same name MUST update the existing record, not create a duplicate | MUST |
| REQ-REG-070 | The SP MUST use the official DCM SP API client library for registry communication | MUST |
| REQ-REG-080 | Capabilities MUST advertise: `supportedPlatforms` ["kubevirt", "baremetal"], `supportedProvisioningTypes` ["hypershift"], `kubernetesSupportedVersions` (dynamic: K8s minor versions derived from ClusterImageSets via compatibility matrix), `operations` ["CREATE", "DELETE", "READ"] | MUST |
| REQ-REG-090 | The SP MUST query `ClusterImageSet` resources on startup, map OCP versions to K8s minor versions using the internal compatibility matrix, and include resulting K8s versions in registration `kubernetesSupportedVersions` | MUST |
| REQ-REG-091 | The SP MUST only include a K8s version in `kubernetesSupportedVersions` if BOTH conditions are met: (1) the compatibility matrix has a mapping for the K8s↔OCP translation, AND (2) at least one matching ClusterImageSet exists on the ACM Hub for the translated OCP version | MUST |
| REQ-REG-100 | The SP SHOULD periodically re-check ClusterImageSets at `SP_VERSION_CHECK_INTERVAL` | SHOULD |
| REQ-REG-110 | When available versions change, the SP MUST re-register with DCM to advertise updated `kubernetesSupportedVersions` | MUST |

#### Configuration Introduced

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DCM_REGISTRATION_URL` | string | _(required)_ | Base URL of the DCM SP Registry |
| `SP_NAME` | string | `acm-cluster-sp` | Provider name for registration |
| `SP_ENDPOINT` | string | _(required)_ | This SP's externally reachable resource endpoint URL (must include the resource path, e.g. https://host/api/v1alpha1/clusters) |
| `SP_REGISTRATION_INITIAL_BACKOFF` | duration | `1s` | Initial retry backoff interval for registration |
| `SP_REGISTRATION_MAX_BACKOFF` | duration | `5m` | Maximum backoff interval cap for registration retries |
| `SP_VERSION_CHECK_INTERVAL` | duration | `5m` | Interval for checking ClusterImageSet changes |
| `SP_DISPLAY_NAME` | string | _(empty)_ | Optional display name for registration |
| `SP_REGION` | string | _(empty)_ | Optional region metadata for registration |
| `SP_ZONE` | string | _(empty)_ | Optional zone metadata for registration |

#### Acceptance Criteria

##### AC-REG-010: Successful first-time registration
- **Requirements:** REQ-REG-010, REQ-REG-020
- **Given** the DCM Registry is reachable at the configured URL
- **And** no provider with name "acm-cluster-sp" exists
- **When** the SP starts up
- **Then** the SP sends `POST {DCM_REGISTRATION_URL}/providers` with the correct payload
- **And** the payload includes `display_name` when `SP_DISPLAY_NAME` is configured
- **And** the payload includes `metadata.region` and `metadata.zone` when configured
- **And** the DCM Registry responds with 201

##### AC-REG-020: Idempotent re-registration on restart
- **Requirements:** REQ-REG-060
- **Given** a provider with name "acm-cluster-sp" already exists in the registry
- **When** the SP restarts and sends the registration request
- **Then** the registry returns the existing provider record
- **And** the SP continues normally

##### AC-REG-030: Registration payload includes required capabilities
- **Requirements:** REQ-REG-080
- **When** the SP sends the registration request
- **Then** the payload includes `service_type="cluster"`
- **And** the payload includes `operations=["CREATE", "DELETE", "READ"]`
- **And** the payload includes `supportedPlatforms=["kubevirt", "baremetal"]`
- **And** the payload includes `supportedProvisioningTypes=["hypershift"]`
- **And** the payload includes `kubernetesSupportedVersions` with K8s minor versions derived from ClusterImageSets via the compatibility matrix

##### AC-REG-040: DCM Registry unreachable at startup
- **Requirements:** REQ-REG-040, REQ-REG-050, REQ-REG-051
- **Given** the DCM Registry is unreachable
- **When** the SP starts
- **Then** the SP retries registration indefinitely with exponential backoff (starting at `SP_REGISTRATION_INITIAL_BACKOFF`, capped at `SP_REGISTRATION_MAX_BACKOFF`)
- **And** registration failures are logged with sufficient detail including response body content when available
- **And** retries continue until success or context cancellation
- **And** non-retryable errors (4xx) cause immediate failure without further retries
- **And** the SP continues running and serving requests (does not exit)

##### AC-REG-080: Empty kubernetesSupportedVersions at startup
- **Requirements:** REQ-REG-090
- **Given** no ClusterImageSet resources exist on the ACM Hub at startup
- **When** the SP registers with DCM
- **Then** the SP registers with `kubernetesSupportedVersions=[]` (empty list)
- **And** registration succeeds without error
- **And** the SP continues running normally

##### AC-REG-050: SP accepts requests before registration completes
- **Requirements:** REQ-REG-030, REQ-REG-031
- **Given** the DCM Registry is reachable but responds slowly
- **When** the SP starts up
- **Then** the HTTP server starts accepting requests immediately
- **And** registration proceeds asynchronously in the background
- **And** requests arriving before registration completes are served normally

##### AC-REG-060: Version refresh triggers re-registration
- **Requirements:** REQ-REG-100, REQ-REG-110
- **Given** the SP is registered with `kubernetesSupportedVersions=["1.29", "1.30"]`
- **And** a new ClusterImageSet for OCP "4.18" (K8s "1.31") is created on the ACM Hub
- **When** the `SP_VERSION_CHECK_INTERVAL` elapses
- **Then** the SP detects the new version
- **And** re-registers with DCM including `kubernetesSupportedVersions=["1.29", "1.30", "1.31"]`

##### AC-REG-070: Startup queries ClusterImageSets for kubernetesSupportedVersions
- **Requirements:** REQ-REG-090
- **Given** the ACM Hub has ClusterImageSets for OCP versions "4.16" and "4.17"
- **When** the SP starts up
- **Then** the registration payload includes `kubernetesSupportedVersions=["1.29", "1.30"]`

##### AC-REG-071: ClusterImageSet without matrix mapping is not advertised
- **Requirements:** REQ-REG-091
- **Given** the ACM Hub has ClusterImageSets for OCP "4.16", "4.17", and "4.19"
- **And** the compatibility matrix only covers OCP 4.14 through 4.18
- **When** the SP queries ClusterImageSets and builds the registration payload
- **Then** `kubernetesSupportedVersions` includes "1.29" (4.16) and "1.30" (4.17)
- **And** `kubernetesSupportedVersions` does NOT include any mapping for OCP "4.19" (no matrix entry)

##### AC-REG-072: Matrix entry without ClusterImageSet is not advertised
- **Requirements:** REQ-REG-091
- **Given** the compatibility matrix maps K8s "1.31" to OCP "4.18"
- **And** NO ClusterImageSet for OCP "4.18" exists on the ACM Hub
- **When** the SP queries ClusterImageSets and builds the registration payload
- **Then** `kubernetesSupportedVersions` does NOT include "1.31"

##### AC-REG-080: Registration uses DCM client library
- **Requirements:** REQ-REG-070
- **When** the SP communicates with the DCM SP Registry
- **Then** it uses the official DCM SP API client library
- **And** does not make raw HTTP calls to the registry

#### Dependencies

None - independently deliverable.

---

### 5.2 Topic 2: HTTP Server

#### Overview

Foundational HTTP server infrastructure. Sets up the router, mounts generated handler bindings, applies middleware, and provides graceful shutdown.

#### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-HTTP-010 | The server MUST start and listen on a configurable bind address | MUST |
| REQ-HTTP-020 | The server MUST mount cluster routes under `/api/v1alpha1` and the health route at `/api/v1alpha1/clusters/health` as defined in the OpenAPI spec | MUST |
| REQ-HTTP-030 | The server MUST initiate graceful shutdown on SIGTERM, draining in-flight requests within a configurable timeout | MUST |
| REQ-HTTP-040 | The server MUST initiate graceful shutdown on SIGINT, draining in-flight requests within a configurable timeout | MUST |
| REQ-HTTP-050 | Server configuration (bind address, timeouts) MUST be provided via environment variables | MUST |
| REQ-HTTP-060 | The server MUST apply request logging middleware with method, path, status code, and duration | MUST |
| REQ-HTTP-070 | The server MUST apply panic recovery middleware | MUST |
| REQ-HTTP-080 | The server MUST log lifecycle events: startup (with listen address) and shutdown initiation | MUST |
| REQ-HTTP-090 | Request validation errors MUST return RFC 7807 `application/problem+json` responses | MUST |
| REQ-HTTP-091 | Response serialization errors MUST return RFC 7807 responses with `type=INTERNAL` | MUST |
| REQ-HTTP-110 | The server SHOULD apply a request timeout middleware | SHOULD |

#### Configuration Introduced

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_SERVER_ADDRESS` | string | `:8080` | HTTP server listen address |
| `SP_SERVER_SHUTDOWN_TIMEOUT` | duration | `15s` | Graceful shutdown drain timeout |
| `SP_SERVER_REQUEST_TIMEOUT` | duration | `30s` | Per-request timeout |
| `SP_SERVER_READ_TIMEOUT` | duration | `15s` | HTTP server read timeout (Slowloris protection) |
| `SP_SERVER_WRITE_TIMEOUT` | duration | `15s` | HTTP server write timeout |
| `SP_SERVER_IDLE_TIMEOUT` | duration | `60s` | HTTP server idle connection timeout |

#### Acceptance Criteria

##### AC-HTTP-010: Server starts and listens
- **Requirements:** REQ-HTTP-010
- **Given** the SP is configured with `SP_SERVER_ADDRESS=":8080"`
- **When** the server starts
- **Then** it accepts TCP connections on port 8080

##### AC-HTTP-020: Routes mounted under correct paths
- **Requirements:** REQ-HTTP-020
- **When** a client sends `GET /clusters` (no `/api/v1alpha1` prefix)
- **Then** the server responds with 404
- **When** a client sends `GET /api/v1alpha1/clusters`
- **Then** the server responds with a valid response (not 404)
- **When** a client sends `GET /api/v1alpha1/clusters/health`
- **Then** the server responds with a valid response (not 404)

##### AC-HTTP-030: Graceful shutdown drains in-flight requests
- **Requirements:** REQ-HTTP-030, REQ-HTTP-040
- **Given** the server is running and a slow request is in-flight
- **When** SIGTERM is received
- **Then** the server waits for the in-flight request to complete (up to `SP_SERVER_SHUTDOWN_TIMEOUT`)
- **And** no new connections are accepted
- **And** the server exits cleanly

##### AC-HTTP-040: Request errors return RFC 7807
- **Requirements:** REQ-HTTP-090
- **When** a client sends `POST /api/v1alpha1/clusters` with malformed JSON
- **Then** the response Content-Type is `application/problem+json`
- **And** the body contains `type`, `title`, and `status` fields

##### AC-HTTP-050: Panic in handler does not crash server
- **Requirements:** REQ-HTTP-070
- **Given** a handler panics during request processing
- **When** the request is processed
- **Then** the server returns 500
- **And** the server continues accepting new requests

##### AC-HTTP-060: Response errors return RFC 7807 with type=INTERNAL
- **Requirements:** REQ-HTTP-091
- **Given** the server encounters an internal error while serializing a response
- **When** the `ResponseErrorHandlerFunc` is invoked
- **Then** the response Content-Type is `application/problem+json`
- **And** the body contains `type="INTERNAL"`

##### AC-HTTP-070: Lifecycle events are logged
- **Requirements:** REQ-HTTP-080
- **When** the server starts
- **Then** a log entry is emitted with the listen address
- **When** shutdown is initiated
- **Then** a log entry is emitted indicating shutdown

#### Dependencies

None - independently deliverable.

---

### 5.3 Topic 3: Health Service

#### Overview

The health endpoint (`GET /api/v1alpha1/clusters/health`) reports whether the SP is operational. DCM polls this endpoint every 10 seconds. Per [`service-provider-health-check.md`](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-health-check/service-provider-health-check.md), the endpoint always returns HTTP 200 when the SP process is alive; the `status` field communicates the actual dependency health. DCM only detects SP failure when the process itself is down (no response).

> **Note:** DCM only checks HTTP status code (200 = alive) per the enhancement. The SP's Health response (`type`, `status`, `path`, `version`, `uptime`) provides richer information for debugging and AEP compliance. `path` is required per AEP singleton resource rules.

> **Note (DD-002):** Health status values are `"healthy"` / `"unhealthy"` rather than `"pass"` from the enhancement. This is an intentional deviation for clearer semantics.

#### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-HLT-010 | `GET /api/v1alpha1/clusters/health` MUST return HTTP 200 with `application/json` content-type when the SP process is running | MUST |
| REQ-HLT-020 | The response MUST include: `status`, `type`, `path`, `version` (SP build version string), and `uptime` (seconds since SP started, integer) | MUST |
| REQ-HLT-030 | `path` MUST be `"health"` | MUST |
| REQ-HLT-040 | `type` MUST be `"acm-cluster-service-provider.dcm.io/health"` | MUST |
| REQ-HLT-050 | `status` MUST be `"healthy"` when all dependency checks pass (DD-002) | MUST |
| REQ-HLT-060 | `status` MUST be `"unhealthy"` when any critical dependency check fails (DD-002) | MUST |
| REQ-HLT-070 | The health check MUST verify connectivity to the ACM Hub K8s API server | MUST |
| REQ-HLT-080 | The health check MUST verify HyperShift operator availability (e.g., HostedCluster CRD exists) | MUST |
| REQ-HLT-090 | When KubeVirt platform is listed in `SP_ENABLED_PLATFORMS`, the health check SHOULD verify KubeVirt infrastructure accessibility | SHOULD |
| REQ-HLT-100 | When BareMetal platform is listed in `SP_ENABLED_PLATFORMS`, the health check SHOULD verify Agent/CIM resource availability | SHOULD |
| REQ-HLT-110 | The health endpoint MUST respond within `SP_HEALTH_CHECK_TIMEOUT` | MUST |
| REQ-HLT-120 | The health endpoint MUST NOT require authentication | MUST |

#### Configuration Introduced

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_HEALTH_CHECK_TIMEOUT` | duration | `5s` | Maximum time for dependency probes |
| `SP_ENABLED_PLATFORMS` | string | `kubevirt,baremetal` | Comma-separated list of enabled platforms for health checks |

#### Acceptance Criteria

##### AC-HLT-010: Healthy SP returns 200 with status=healthy
- **Requirements:** REQ-HLT-010, REQ-HLT-020, REQ-HLT-040, REQ-HLT-050
- **Given** the ACM Hub K8s API is reachable and HyperShift operator is installed
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** the response status is 200
- **And** the body contains `status="healthy"`, `type="acm-cluster-service-provider.dcm.io/health"`, `path="health"`
- **And** the body contains a non-empty `version` field
- **And** the body contains an `uptime` field with value >= 0

##### AC-HLT-020: Unhealthy SP returns 200 with status=unhealthy
- **Requirements:** REQ-HLT-060, REQ-HLT-070
- **Given** the ACM Hub K8s API is unreachable
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** the response status is 200
- **And** the body contains `status="unhealthy"`

##### AC-HLT-030: Health endpoint responds within timeout
- **Requirements:** REQ-HLT-110
- **Given** dependency checks are slow (simulated >5s latency)
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** the response is returned within `SP_HEALTH_CHECK_TIMEOUT`
- **And** the body contains `status="unhealthy"` (timeout = failure)

> **Note:** During startup initialization (before the HealthChecker's internal state is fully initialized), the health endpoint returns HTTP 200 with `status="unhealthy"` until all dependency probes have completed at least once.

##### AC-HLT-040: Response conforms to Health schema
- **Requirements:** REQ-HLT-020, REQ-HLT-030
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** Content-Type is `application/json`
- **And** all required fields (`type`, `status`, `path`, `version`, `uptime`) are present

##### AC-HLT-050: HyperShift CRD unavailable results in unhealthy
- **Requirements:** REQ-HLT-080
- **Given** the ACM Hub K8s API is reachable
- **And** the HostedCluster CRD does not exist (HyperShift not installed)
- **When** `GET /api/v1alpha1/clusters/health` is called
- **Then** the response status is 200
- **And** the body contains `status="unhealthy"`

#### Dependencies

- **Topic 2 (HTTP Server)**: needs a running server to serve the endpoint.

---

### 5.4 Topic 4: OpenAPI Endpoints

#### Overview

Thin HTTP handler implementations of `StrictServerInterface` (defined at `internal/api/server/server.gen.go:728-744`). Handlers parse requests, validate inputs, return RFC 7807 errors, and delegate business logic to internal services (Topic 5). Handlers do NOT contain K8s API calls or cluster provisioning logic.

> **Design Decision — Dual Identity (`id` + `metadata.name`):** The SP follows AEP-133's dual-identity pattern. `id` is the DCM-level resource identifier: readOnly, either client-specified via the `?id=` query parameter or server-generated as a UUID. `metadata.name` is the user-provided human-readable name used as the K8s HostedCluster `metadata.name`. The `path` field is set to `"clusters/<id>"`. The `dcm.project/dcm-instance-id` K8s label stores `id`. URL paths use `id` (`/clusters/{clusterId}`). On creation, the SP checks for conflicts on both `id` (via label query) and `metadata.name` (via K8s name lookup in the target namespace).

#### Requirements - General

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-API-010 | A struct implementing `server.StrictServerInterface` MUST be provided with compile-time verification (`var _ server.StrictServerInterface = (*Handler)(nil)`) | MUST |
| REQ-API-020 | All error responses MUST use `application/problem+json` with the `Error` schema (RFC 7807) | MUST |
| REQ-API-030 | Error `type` MUST be one of the `ErrorType` enum values defined in the OpenAPI spec | MUST |
| REQ-API-040 | Error `status` MUST match the HTTP status code | MUST |
| REQ-API-050 | Internal errors MUST NOT leak implementation details (stack traces, K8s API errors) | MUST |
| REQ-API-011 | The Handler MUST depend on a `ClusterService` interface for all cluster operations (Create, Get, List, Delete). The Handler MUST NOT import or call K8s client packages directly | MUST |
| REQ-API-012 | The Handler MUST depend on a `HealthChecker` interface for the health endpoint | MUST |

#### Requirements - POST /api/v1alpha1/clusters

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-API-060 | MUST accept a JSON body conforming to the `Cluster` schema | MUST |
| REQ-API-070 | `version` and `nodes.control_plane` MUST be present (required fields) | MUST |
| REQ-API-080 | `nodes.workers` MUST be present and have `count >= 1`; if omitted, MUST return 400 with `type=INVALID_ARGUMENT` and a message indicating workers are required | MUST |
| REQ-API-090 | The SP MUST validate that `service_type == "cluster"` and return 400 with `type=INVALID_ARGUMENT` if not | MUST |
| REQ-API-100 | `id` MUST be the cluster resource identifier. If a client-specified `id` is provided via `?id=` query parameter, it MUST be used; if `?id=` is present but empty, it MUST be treated as absent (generate UUID). Otherwise the SP MUST generate a UUID. `path` MUST be set to `"clusters/<id>"`. The `dcm.project/dcm-instance-id` K8s label MUST be set to `id`. `metadata.name` MUST be used as the K8s HostedCluster resource name | MUST |
| REQ-API-101 | `id` MUST be readOnly. If `id` appears in the request body, it MUST be ignored (IMPL-001) | MUST |
| REQ-API-102 | On creation, the SP MUST check for an existing resource with the same `id` (by querying the `dcm.project/dcm-instance-id` label). If found, MUST return 409 with `type=ALREADY_EXISTS` | MUST |
| REQ-API-103 | On creation, the SP MUST check for an existing K8s HostedCluster with the same `metadata.name` in the target namespace. If found, MUST return 409 with `type=ALREADY_EXISTS` and a detail message indicating the name conflict | MUST |
| REQ-API-104 | If both `?id=` query parameter and `id` in the request body are provided, the query parameter MUST take precedence and the body value MUST be ignored | MUST |
| REQ-API-110 | Successful creation MUST return 201 with the full `Cluster` resource including server-set fields: `id`, `path`, `status=PENDING`, `create_time`, `update_time` | MUST |
| REQ-API-130 | If the platform is not supported, MUST return 422 with `type=UNPROCESSABLE_ENTITY` | MUST |
| REQ-API-140 | If the version has no matching ClusterImageSet, MUST return 422 with `type=UNPROCESSABLE_ENTITY` | MUST |
| REQ-API-150 | If the body is malformed or fails validation, MUST return 400 with `type=INVALID_ARGUMENT` | MUST |
| REQ-API-160 | Read-only fields in the request body (`id`, `status`, `api_endpoint`, `kubeconfig`, `create_time`, `update_time`, `path`, `status_message`, `console_uri`) MUST be ignored (IMPL-001: enforced by OpenAPI middleware, not handler) | MUST |
| REQ-API-165 | `update_time` MUST reflect the timestamp of the most recent status transition as determined by the HostedCluster condition `lastTransitionTime`, or equal `create_time` if no transition has occurred | MUST |
| REQ-API-166 | When `status` is `FAILED`, `status_message` SHOULD contain the failure reason from the HostedCluster's `Degraded` condition message | SHOULD |
| REQ-API-167 | When `status` is `UNAVAILABLE`, `status_message` SHOULD contain a human-readable explanation if available from HostedCluster conditions | SHOULD |
| REQ-API-170 | Memory and storage values MUST match the pattern `^[1-9][0-9]*(MB\|GB\|TB)$` per DCM service type definition; invalid formats (including zero-value like `"0GB"`) MUST return 400 | MUST |
| REQ-API-175 | `metadata.name` MUST match `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` (1-63 chars). This is enforced by the OpenAPI schema pattern and validation middleware | MUST |
| REQ-API-380 | A base domain MUST be available for cluster creation. It is resolved from `provider_hints.acm.base_domain` (request-level) or `SP_BASE_DOMAIN` (config-level, fallback). If neither is provided, the operation MUST fail with an error indicating base domain is required | MUST |


> **Note:** On reads, the base domain for `console_uri` construction (REQ-API-390) is sourced from the HostedCluster's `spec.dns.baseDomain` field, not from configuration.

#### Requirements - GET /api/v1alpha1/clusters/\{clusterId\}

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-API-180 | MUST return the `Cluster` resource with the given ID | MUST |
| REQ-API-190 | Cluster state MUST be sourced from the K8s HostedCluster resource (K8s is the read source) | MUST |
| REQ-API-200 | If no cluster exists, MUST return 404 with `type=NOT_FOUND` | MUST |
| REQ-API-210 | `clusterId` MUST match `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` (1-63 chars) | MUST |
| REQ-API-220 | When status is `READY`, response MUST include populated `api_endpoint`, `console_uri`, `kubeconfig` (base64-encoded) | MUST |
| REQ-API-230 | When status is `PENDING`, `PROVISIONING`, `FAILED`, `DELETING`, or `DELETED`, `api_endpoint`, `console_uri`, `kubeconfig` MUST be empty/absent | MUST |
| REQ-API-231 | When status is `UNAVAILABLE`, `api_endpoint`, `console_uri`, `kubeconfig` SHOULD be present if available from the HostedCluster (the cluster was previously operational; credentials may still be valid) | SHOULD |
| REQ-API-390 | When status is READY, `console_uri` MUST be constructed using the `SP_CONSOLE_URI_PATTERN` template with `{name}` and `{base_domain}` substitutions. When status is UNAVAILABLE, `console_uri` SHOULD be constructed using the same pattern if `base_domain` is available from the HostedCluster. For all other non-READY statuses, `console_uri` MUST be empty | MUST |

#### Requirements - GET /api/v1alpha1/clusters

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-API-240 | MUST return a paginated `ClusterList` | MUST |
| REQ-API-250 | MUST support cursor-based pagination with `page_token` and `max_page_size` | MUST |
| REQ-API-260 | `max_page_size` MUST default to 50 | MUST |
| REQ-API-270 | `max_page_size` values above 100 MUST return 400 with `type=INVALID_ARGUMENT` and a detail message indicating the allowed range is 1-100 | MUST |
| REQ-API-280 | `max_page_size` below 1 MUST return 400 with `type=INVALID_ARGUMENT` and a detail message indicating the minimum allowed value is 1 | MUST |
| REQ-API-290 | Invalid or expired `page_token` MUST return 400 with `type=INVALID_ARGUMENT` | MUST |
| REQ-API-291 | Pagination tokens MUST be opaque to clients. Tokens MAY expire after a reasonable duration. Pagination provides eventual consistency — results may reflect concurrent modifications | MUST |
| REQ-API-300 | When no more results exist, `next_page_token` MUST be absent or empty | MUST |
| REQ-API-310 | Cluster state for each result MUST be sourced from K8s (same as GetCluster) | MUST |
| REQ-API-315 | Results MUST be ordered by cluster `metadata.name` ascending (alphabetical) | MUST |

#### Requirements - DELETE /api/v1alpha1/clusters/{clusterId}

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-API-320 | MUST return 204 No Content on success | MUST |
| REQ-API-330 | The DELETE endpoint returns 204 after initiating HostedCluster deletion via K8s. Actual resource cleanup (control plane teardown, NodePool removal) is asynchronous and managed by the HyperShift operator | MUST |
| REQ-API-340 | If no cluster exists, MUST return 404 with `type=NOT_FOUND` | MUST |
| REQ-API-350 | The handler MUST delegate to the ACM service to initiate HostedCluster + NodePool deletion | MUST |
| REQ-API-360 | Deleting a cluster already in `DELETED` status SHOULD return 404 | SHOULD |
| REQ-API-370 | Deleting a cluster already being deleted (status `DELETING`) SHOULD be idempotent and return 204. GET returns `DELETING` during the teardown window | SHOULD |

#### Acceptance Criteria

##### AC-API-010: Create cluster with server-generated id
- **Requirements:** REQ-API-100, REQ-API-110
- **Given** no clusters exist
- **When** `POST /api/v1alpha1/clusters` (no `?id=` param) with valid body (service_type, version, nodes with control_plane and workers, metadata.name="my-cluster")
- **Then** response is 201
- **And** body contains a server-generated `id` (valid UUID)
- **And** body contains `path="clusters/<id>"`
- **And** body contains `status="PENDING"`, `create_time`, `update_time`

##### AC-API-011: Create cluster with client-specified id
- **Requirements:** REQ-API-100
- **When** `POST /api/v1alpha1/clusters?id=my-custom-id` with valid body (metadata.name="my-cluster")
- **Then** response is 201
- **And** body contains `id="my-custom-id"`, `path="clusters/my-custom-id"`

##### AC-API-020: Create cluster with duplicate metadata.name
- **Requirements:** REQ-API-103
- **Given** a cluster with `metadata.name="my-cluster"` exists
- **When** `POST /api/v1alpha1/clusters` with a new `?id=` but `metadata.name="my-cluster"`
- **Then** response is 409 with `type="ALREADY_EXISTS"` and detail indicating the name conflict

##### AC-API-021: Create cluster with duplicate id
- **Requirements:** REQ-API-102
- **Given** a cluster with `id="abc123"` exists
- **When** `POST /api/v1alpha1/clusters?id=abc123` with a different `metadata.name`
- **Then** response is 409 with `type="ALREADY_EXISTS"`

##### AC-API-030: Create with invalid service_type
- **Requirements:** REQ-API-090
- **When** `POST /api/v1alpha1/clusters` with `service_type="compute"`
- **Then** response is 400 with `type="INVALID_ARGUMENT"`

##### AC-API-040: Create with missing workers
- **Requirements:** REQ-API-080
- **When** `POST /api/v1alpha1/clusters` with body containing `nodes.control_plane` but no `nodes.workers`
- **Then** response is 400 with `type="INVALID_ARGUMENT"` and detail indicating workers are required

##### AC-API-050: Create with unsupported platform
- **Requirements:** REQ-API-130
- **When** `POST` with `provider_hints.acm.platform="aws"`
- **Then** response is 422 with `type="UNPROCESSABLE_ENTITY"`

##### AC-API-060: Create with invalid memory format
- **Requirements:** REQ-API-170
- **When** `POST` with `nodes.control_plane.memory="16Gi"` (K8s format instead of DCM format)
- **Then** response is 400 with `type="INVALID_ARGUMENT"`

##### AC-API-070: Read-only fields in request are ignored
- **Requirements:** REQ-API-160, REQ-API-101
- **When** `POST` with body containing `id="custom"`, `status="READY"`, `api_endpoint="https://fake"`
- **Then** response is 201 with server-set `id` (not "custom"), `status="PENDING"`, and no `api_endpoint`

##### AC-API-080: Get existing cluster (READY)
- **Requirements:** REQ-API-180, REQ-API-220
- **Given** cluster with `id="my-cluster-id"` exists with status READY in K8s
- **When** `GET /api/v1alpha1/clusters/my-cluster-id`
- **Then** response is 200 with `id`, `metadata.name`, `api_endpoint`, `console_uri`, `kubeconfig` populated

##### AC-API-090: Get existing cluster (non-READY, non-UNAVAILABLE)
- **Requirements:** REQ-API-230
- **Given** cluster with `id="my-cluster-id"` exists with status PROVISIONING
- **When** `GET /api/v1alpha1/clusters/my-cluster-id`
- **Then** response is 200 with empty `api_endpoint`, `console_uri`, `kubeconfig`

##### AC-API-091: Get existing cluster (UNAVAILABLE with credentials)
- **Requirements:** REQ-API-231
- **Given** cluster with `id="my-cluster-id"` exists with status UNAVAILABLE and the HostedCluster still has api_endpoint and kubeconfig Secret available
- **When** `GET /api/v1alpha1/clusters/my-cluster-id`
- **Then** response is 200 with `api_endpoint`, `console_uri`, `kubeconfig` populated

##### AC-API-100: Get non-existent cluster
- **Requirements:** REQ-API-200
- **When** `GET /api/v1alpha1/clusters/nonexistent-id`
- **Then** response is 404 with `type="NOT_FOUND"`

##### AC-API-110: List with default pagination
- **Requirements:** REQ-API-240, REQ-API-260
- **Given** 60 clusters exist
- **When** `GET /api/v1alpha1/clusters`
- **Then** response contains 50 results and a `next_page_token`

##### AC-API-120: List with max_page_size exceeding maximum
- **Requirements:** REQ-API-270
- **When** `GET /api/v1alpha1/clusters?max_page_size=200`
- **Then** response is 400 with `type="INVALID_ARGUMENT"` and detail indicating the allowed range is 1-100

##### AC-API-130: Delete existing cluster
- **Requirements:** REQ-API-320
- **Given** cluster with `id="my-cluster-id"` exists
- **When** `DELETE /api/v1alpha1/clusters/my-cluster-id`
- **Then** response is 204 with empty body

##### AC-API-140: Delete non-existent cluster
- **Requirements:** REQ-API-340
- **When** `DELETE /api/v1alpha1/clusters/nonexistent-id`
- **Then** response is 404

##### AC-API-150: GET during deletion returns DELETING status
- **Requirements:** REQ-API-190
- **Given** cluster with `id="my-cluster-id"` deletion has been initiated (HostedCluster has `deletionTimestamp` set)
- **When** `GET /api/v1alpha1/clusters/my-cluster-id`
- **Then** response is 200 with `status="DELETING"`

##### AC-API-160: Console URI format when READY
- **Requirements:** REQ-API-390
- **Given** a cluster with `metadata.name="my-cluster"` and `base_domain="example.com"` has status READY
- **When** `GET /api/v1alpha1/clusters/my-cluster-id`
- **Then** `console_uri` matches the `SP_CONSOLE_URI_PATTERN` template (default: `https://console-openshift-console.apps.my-cluster.example.com`)

##### AC-API-170: Query param id takes precedence over body id
- **Requirements:** REQ-API-104
- **When** `POST /api/v1alpha1/clusters?id=query-id` with body containing `id="body-id"`
- **Then** response is 201 with `id="query-id"`

##### AC-API-180: Internal errors do not leak details
- **Requirements:** REQ-API-050
- **Given** the underlying service returns a K8s API error with internal details
- **When** the handler processes the error
- **Then** the response contains a generic error message without stack traces or K8s API details

##### AC-API-190: Version not matching ClusterImageSet returns 422
- **Requirements:** REQ-API-140
- **When** `POST /api/v1alpha1/clusters` with `version="9.99"`
- **And** no K8s-to-OCP mapping exists for version "9.99" in the compatibility matrix
- **Then** response is 422 with `type="UNPROCESSABLE_ENTITY"`

##### AC-API-200: max_page_size below 1 returns 400
- **Requirements:** REQ-API-280
- **When** `GET /api/v1alpha1/clusters?max_page_size=0`
- **Then** response is 400 with `type="INVALID_ARGUMENT"` and detail indicating the minimum allowed value is 1

##### AC-API-210: Invalid page_token returns 400
- **Requirements:** REQ-API-290
- **When** `GET /api/v1alpha1/clusters?page_token=invalid-token`
- **Then** response is 400 with `type="INVALID_ARGUMENT"`

##### AC-API-220: Last page has empty next_page_token
- **Requirements:** REQ-API-300
- **Given** 10 clusters exist
- **When** `GET /api/v1alpha1/clusters?max_page_size=50`
- **Then** response contains 10 results
- **And** `next_page_token` is absent or empty

##### AC-API-230: Delete DELETED cluster returns 404
- **Requirements:** REQ-API-360
- **Given** a cluster with `id="deleted-cluster"` has already been fully deleted (no longer exists in K8s)
- **When** `DELETE /api/v1alpha1/clusters/deleted-cluster`
- **Then** response is 404

##### AC-API-240: Delete DELETING cluster is idempotent
- **Requirements:** REQ-API-370
- **Given** a cluster with `id="deleting-cluster"` is currently being deleted (status `DELETING`)
- **When** `DELETE /api/v1alpha1/clusters/deleting-cluster`
- **Then** response is 204

##### AC-API-250: Base domain from request hints
- **Requirements:** REQ-API-380
- **When** `POST /api/v1alpha1/clusters` with `provider_hints.acm.base_domain="custom.example.com"`
- **Then** the cluster is created with `base_domain="custom.example.com"`

##### AC-API-260: Base domain from config fallback
- **Requirements:** REQ-API-380
- **Given** `SP_BASE_DOMAIN="default.example.com"` is configured
- **When** `POST /api/v1alpha1/clusters` without `provider_hints.acm.base_domain`
- **Then** the cluster is created with `base_domain="default.example.com"`

##### AC-API-270: Base domain missing returns error
- **Requirements:** REQ-API-380
- **Given** no `SP_BASE_DOMAIN` is configured
- **When** `POST /api/v1alpha1/clusters` without `provider_hints.acm.base_domain`
- **Then** the operation fails with an error indicating base domain is required

#### Dependencies

- **Topic 2 (HTTP Server)**: server infrastructure

---

### 5.5 Topic 5: ACM Platform Services

#### Overview

Internal service implementing cluster lifecycle operations for all HyperShift-based platforms. Translates DCM API requests into HostedCluster + NodePool CRDs. K8s (ACM Hub) is the authoritative read source. Common behaviors (status mapping, label management, version validation, resource format conversion) are shared across platforms; platform-specific details (VM templates, agent discovery) are handled in subsections 5.5.1 (KubeVirt) and 5.5.2 (BareMetal).

#### Version Mapping

> **Note (DD-001):** The SP advertises available K8s minor versions in `kubernetesSupportedVersions` during registration. Internally, the SP uses the compatibility matrix to translate K8s → OCP for ClusterImageSet lookup. Callers MUST use one of the advertised K8s minor versions when creating a cluster. The compatibility matrix SHOULD be loaded from a configuration file (`SP_VERSION_MATRIX_PATH`) to allow updates without code changes or redeployment. The table below shows the default compatibility matrix.

| OpenShift Version | Kubernetes Version |
|-------------------|--------------------|
| 4.14              | 1.27               |
| 4.15              | 1.28               |
| 4.16              | 1.29               |
| 4.17              | 1.30               |
| 4.18              | 1.31               |

Pattern: OCP 4.x = K8s 1.(x+13). The SP SHOULD support expanding this range without code changes via the `SP_VERSION_MATRIX_PATH` configuration file.

Reference: [Red Hat KB - Which Kubernetes API version is included by each OpenShift 4 release?](https://access.redhat.com/solutions/4870701)

> **Known Limitation (DNS name length):** Combining a long `metadata.name` (up to 63 characters) with a long `base_domain` in the `console_uri` pattern could produce FQDNs exceeding the 253-character DNS limit. Operators MUST ensure their `base_domain` choice keeps resulting FQDNs within DNS limits.

#### Status Mapping Table

| DCM Status | Condition / Signal | Description |
|------------|-------------------|-------------|
| `PENDING` | `Progressing=Unknown` | Cluster creation initiated |
| `PROVISIONING` | `Progressing=True`, `Available=False` | Control plane being provisioned |
| `READY` | `Available=True`, `Progressing=False` | Cluster fully operational |
| `UNAVAILABLE` | `Available=False`, `Progressing=False` | Cluster was operational but is no longer available and not progressing |
| `FAILED` | `Degraded=True` | Cluster in degraded/failed state |
| `DELETING` | `metadata.deletionTimestamp != nil` | Cluster deletion in progress |
| `DELETED` | HostedCluster not found (informer DELETE event) | Resource no longer exists (CloudEvents only — API returns 404) |

> **Condition Precedence (REQ-ACM-160):** `deletionTimestamp != nil` (highest) > `Degraded=True` > `Available=True, Progressing=False` > `Progressing=True, Available=False` > `Available=False, Progressing=False` > `Progressing=Unknown` (lowest).

> **DELETING Mechanism:** When `DELETE` is called on a HostedCluster, K8s sets `metadata.deletionTimestamp` and returns HTTP 202. The HostedCluster has a finalizer (`hypershift.openshift.io/finalizer`) that prevents immediate removal. While the HyperShift operator processes cleanup (tearing down control plane pods, NodePools, ManagedCluster), the resource still exists with `deletionTimestamp` set. The SP's informer receives this as an `Update` event. The SP detects the deleting state by checking `resource.DeletionTimestamp != nil`.

#### Common Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-ACM-010 | MUST create resources in order: HostedCluster CR → NodePool CR for the given platform type | MUST |
| REQ-ACM-020 | All created K8s resources MUST carry labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=<id>`, `dcm.project/dcm-service-type=cluster` | MUST |
| REQ-ACM-030 | `version` (a K8s minor version) MUST be translated to the corresponding OCP version using the internal compatibility matrix, then validated against available ClusterImageSet resources on the ACM Hub (DD-001) | MUST |
| REQ-ACM-031 | If the caller-provided K8s version has no entry in the SP's compatibility matrix, the operation MUST fail (handler returns 422) with an error indicating the version is not supported | MUST |
| REQ-ACM-032 | If the compatibility matrix maps the K8s version to an OCP version but no matching ClusterImageSet exists on the ACM Hub, the operation MUST fail (handler returns 422) with an error indicating no available release for that version | MUST |
| REQ-ACM-040 | If no matching ClusterImageSet exists for the translated OCP version (or no K8s-to-OCP mapping exists), the operation MUST fail (handler returns 422) | MUST |
| REQ-ACM-050 | The SP SHOULD support `provider_hints.acm.release_image` override | SHOULD |
| REQ-ACM-051 | When `provider_hints.acm.release_image` is specified and supported, the SP MUST use it directly, bypassing ClusterImageSet lookup | MUST |
| REQ-ACM-060 | `nodes.control_plane.cpu` and `memory` MUST be applied as resource request overrides on the HostedCluster using HyperShift's `resource-request-override.hypershift.openshift.io` annotation prefix (DD-004). The SP MUST target `kube-apiserver.kube-apiserver` and `etcd.etcd` components. Each targeted component receives the full specified CPU and memory values. If `cpu` is 0 or `memory` is empty, the corresponding resource type MUST NOT appear in the annotation value | MUST |
| REQ-ACM-061 | The annotation value format MUST be `cpu=<cores>,memory=<quantity>` where `<cores>` is the integer CPU core count and `<quantity>` is the DCM memory value converted to K8s SI format (e.g., `"16GB"` → `"16G"`). The SP MUST use the `ResourceRequestOverrideAnnotationPrefix` constant from the HyperShift API package to construct annotation keys (DD-004) | MUST |
| REQ-ACM-070 | `nodes.control_plane.count` MUST be accepted but IGNORED (HyperShift manages HA internally) | MUST |
| REQ-ACM-080 | `nodes.control_plane.storage` MUST be accepted but IGNORED (etcd managed by HyperShift) | MUST |
| REQ-ACM-090 | `nodes.workers.count` MUST map to NodePool `replicas` | MUST |
| REQ-ACM-100 | Resources MUST be created in a configurable namespace | MUST |
| REQ-ACM-110 | For reads, MUST query HostedCluster CRs from K8s and map conditions to DCM status (see Status Mapping Table) | MUST |
| REQ-ACM-120 | When status is READY, MUST extract `api_endpoint` from HostedCluster status | MUST |
| REQ-ACM-130 | When status is READY, MUST extract kubeconfig from the associated K8s Secret, base64-encode it | MUST |
| REQ-ACM-140 | For deletion, MUST explicitly delete associated NodePool CRs (by `dcm.project/dcm-instance-id` label), then delete the HostedCluster CR. NodePool NotFound is treated as success (HyperShift may have already cascaded or external deletion). | MUST |
| REQ-ACM-150 | Memory/storage values from DCM format (`"16GB"`) MUST be converted to K8s resource quantity format | MUST |
| REQ-ACM-160 | When multiple HostedCluster conditions are true simultaneously, status MUST be resolved using the following precedence (highest to lowest): `deletionTimestamp != nil` → DELETING, `Degraded=True` → FAILED, `Available=True + Progressing=False` → READY, `Progressing=True + Available=False` → PROVISIONING, `Available=False + Progressing=False` → UNAVAILABLE, `Progressing=Unknown` → PENDING. The highest-precedence matching condition wins. | MUST |
| REQ-ACM-170 | On partial create failure (HostedCluster created but NodePool creation fails), the SP MUST delete the orphaned HostedCluster before returning the error | MUST |
| REQ-ACM-180 | The SP MUST set `Spec.Services` on every HostedCluster to 4 entries: `APIServer` with `LoadBalancer`, and `OAuthServer`, `Konnectivity`, `Ignition` with `Route` strategy (DD-005) | MUST |
| REQ-ACM-190 | At startup, the SP MUST create (or update if existing) a K8s Secret named `<SP_NAME>-pull-secret` of type `kubernetes.io/dockerconfigjson` from the base64-decoded `SP_PULL_SECRET` env var in `SP_CLUSTER_NAMESPACE`. The Secret carries labels `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=cluster` (DD-007) | MUST |
| REQ-ACM-191 | The HostedCluster's `Spec.PullSecret.Name` MUST reference the shared PullSecret Secret (`<SP_NAME>-pull-secret`) created at startup (DD-007) | MUST |
| REQ-ACM-195 | The SP MUST fail to start if `SP_PULL_SECRET` env var is not set or empty, with an error naming the missing variable (DD-007) | MUST |
| REQ-ACM-200 | The SP MUST set `Spec.Management.UpgradeType` to `InPlace` on every NodePool, for all platforms (DD-006) | MUST |

#### Common Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_HUB_KUBECONFIG` | string | _(in-cluster)_ | Path to kubeconfig for ACM Hub |
| `SP_CLUSTER_NAMESPACE` | string | _(required)_ | Namespace for HostedCluster/NodePool resources. MUST be provided; the SP MUST fail at startup with a clear error message if not set |
| `SP_CONSOLE_URI_PATTERN` | string | `https://console-openshift-console.apps.{name}.{base_domain}` | Template for constructing `console_uri`. Supports `{name}` and `{base_domain}` substitutions |
| `SP_VERSION_MATRIX_PATH` | string | _(empty — use built-in defaults)_ | Path to a JSON/YAML file containing the OCP↔K8s version compatibility matrix. When empty, the SP uses the built-in default matrix |
| `SP_BASE_DOMAIN` | string | _(empty)_ | Default base DNS domain for clusters; overridden by `provider_hints.acm.base_domain` per request |
| `SP_PULL_SECRET` | string | _(required)_ | Base64-encoded `.dockerconfigjson` content for the shared PullSecret Secret. The SP creates (or updates) a Secret named `<SP_NAME>-pull-secret` at startup. MUST be provided; the SP MUST fail at startup with a clear error message if not set |

#### Common Acceptance Criteria

> **Note (concurrent create):** Two simultaneous `POST` requests with the same `metadata.name` could both pass the conflict check before either creates the resource. The K8s API server provides the final uniqueness guarantee — the SP MUST handle K8s `AlreadyExists` errors and translate them to domain `ALREADY_EXISTS` errors.

> **Note (external deletion):** If a HostedCluster is deleted outside DCM (e.g., via `kubectl delete`), the informer detects the deletion and publishes a `DELETED` CloudEvent. API endpoints return 404 for the deleted resource. External deletions are handled transparently — no special handling is required beyond the existing informer + API behavior.

> **Note (empty platform string):** If `provider_hints.acm.platform` is present but set to an empty string (`""`), the SP MUST treat it as absent and default to KubeVirt.

##### AC-ACM-010: Create cluster creates HostedCluster and NodePool
- **Requirements:** REQ-ACM-010
- **Given** the ACM Hub is reachable and a ClusterImageSet for the requested version exists
- **When** CreateCluster is called with valid parameters
- **Then** a HostedCluster CR is created with the appropriate platform type, referencing the shared PullSecret Secret
- **And** a NodePool CR is created and associated with the HostedCluster

##### AC-ACM-020: DCM labels applied to all resources
- **Requirements:** REQ-ACM-020
- **Given** a cluster is being created
- **When** K8s resources (HostedCluster, NodePool) are created
- **Then** both CRs carry labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=<id>`, `dcm.project/dcm-service-type=cluster`

##### AC-ACM-030: Version validated against ClusterImageSets via compatibility matrix
- **Requirements:** REQ-ACM-030
- **Given** a ClusterImageSet for OCP "4.15" (K8s "1.28") exists on the ACM Hub
- **When** CreateCluster is called with `version="1.28"`
- **Then** the SP translates "1.28" to OCP via the compatibility matrix and finds a matching ClusterImageSet
- **And** the version is accepted and used for cluster creation

##### AC-ACM-040: K8s version not in compatibility matrix
- **Requirements:** REQ-ACM-031, REQ-ACM-040
- **Given** the compatibility matrix does not contain an entry for K8s "9.99"
- **When** CreateCluster is called with `version="9.99"`
- **Then** the operation fails with 422 indicating the version is not supported

##### AC-ACM-041: K8s version has matrix entry but no ClusterImageSet
- **Requirements:** REQ-ACM-032
- **Given** the compatibility matrix maps K8s "1.30" to OCP "4.17"
- **And** NO ClusterImageSet for OCP "4.17" exists on the ACM Hub
- **When** CreateCluster is called with `version="1.30"`
- **Then** the operation fails with 422 indicating no available release for that version

##### AC-ACM-042: Caller uses OCP version format (wrong format)
- **Requirements:** REQ-ACM-031
- **Given** the compatibility matrix maps K8s minor versions, not OCP versions
- **When** CreateCluster is called with `version="4.17.0"` (OCP format)
- **Then** the operation fails with 422 (no matrix entry for "4.17.0")

##### AC-ACM-050: Release image override bypasses ClusterImageSet
- **Requirements:** REQ-ACM-050, REQ-ACM-051
- **When** CreateCluster is called with `provider_hints.acm.release_image="quay.io/ocp-release:4.15.2"`
- **Then** the HostedCluster uses the specified release image directly
- **And** ClusterImageSet lookup is bypassed

##### AC-ACM-060: control_plane cpu/memory mapped to resource request override annotations
- **Requirements:** REQ-ACM-060, REQ-ACM-061
- **When** CreateCluster is called with `control_plane.cpu=4, control_plane.memory="16GB"`
- **Then** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/kube-apiserver.kube-apiserver` with value `cpu=4,memory=16G`
- **And** the HostedCluster has annotation `resource-request-override.hypershift.openshift.io/etcd.etcd` with value `cpu=4,memory=16G`

##### AC-ACM-070: control_plane.count is ignored
- **Requirements:** REQ-ACM-070
- **When** CreateCluster is called with `control_plane.count=1`
- **Then** the HostedCluster is created without an explicit control plane replica count

##### AC-ACM-080: control_plane.storage is ignored
- **Requirements:** REQ-ACM-080
- **When** CreateCluster is called with `control_plane.storage="100GB"`
- **Then** the HostedCluster is created without explicit etcd storage configuration

##### AC-ACM-090: workers.count maps to NodePool replicas
- **Requirements:** REQ-ACM-090
- **When** CreateCluster is called with `workers.count=3`
- **Then** the NodePool is created with `replicas=3`

##### AC-ACM-100: Resources created in configured namespace
- **Requirements:** REQ-ACM-100
- **Given** `SP_CLUSTER_NAMESPACE="clusters"`
- **When** CreateCluster is called
- **Then** HostedCluster and NodePool are created in the "clusters" namespace

##### AC-ACM-110: Get cluster maps conditions to status
- **Requirements:** REQ-ACM-110
- **Given** a HostedCluster exists with `Available=True`, `Progressing=False`
- **When** GetCluster is called
- **Then** returned status is `READY`

##### AC-ACM-120: READY cluster includes api_endpoint
- **Requirements:** REQ-ACM-120
- **Given** a HostedCluster has status `READY`
- **When** GetCluster is called
- **Then** `api_endpoint` is populated from HostedCluster status

##### AC-ACM-130: READY cluster includes kubeconfig
- **Requirements:** REQ-ACM-130
- **Given** a HostedCluster has status `READY`
- **When** GetCluster is called
- **Then** `kubeconfig` is populated with base64-encoded content from the associated K8s Secret

##### AC-ACM-140: Delete cluster deletes NodePools and HostedCluster
- **Requirements:** REQ-ACM-140
- **Given** a HostedCluster and NodePool(s) with label `dcm.project/dcm-instance-id="my-cluster-id"` exist
- **When** DeleteCluster is called for `id="my-cluster-id"`
- **Then** NodePools with matching `dcm.project/dcm-instance-id` label are deleted
- **And** the HostedCluster CR is deleted from K8s

##### AC-ACM-150: Memory format conversion
- **Requirements:** REQ-ACM-150
- **When** CreateCluster is called with `workers.memory="16GB"`
- **Then** the NodePool uses the equivalent K8s resource quantity format

##### AC-ACM-160: Condition precedence — Degraded takes priority over Available
- **Requirements:** REQ-ACM-160
- **Given** a HostedCluster has `Degraded=True` AND `Available=True`
- **When** GetCluster is called
- **Then** returned status is `FAILED` (not `READY`)

##### AC-ACM-170: Partial create failure cleans up orphaned resources
- **Requirements:** REQ-ACM-170
- **Given** a HostedCluster was created successfully
- **And** NodePool creation fails
- **When** the error is detected
- **Then** the orphaned HostedCluster is deleted
- **And** the error is returned to the caller

##### AC-ACM-180: HostedCluster has Services set
- **Requirements:** REQ-ACM-180
- **Given** a cluster is being created (any platform)
- **When** the HostedCluster CR is constructed
- **Then** `Spec.Services` contains exactly 4 `ServicePublishingStrategyMapping` entries: `APIServer/LoadBalancer`, `OAuthServer/Route`, `Konnectivity/Route`, `Ignition/Route`

##### AC-ACM-190: Shared PullSecret Secret created at startup from env var
- **Requirements:** REQ-ACM-190
- **Given** `SP_PULL_SECRET` env var contains base64-encoded `.dockerconfigjson` content
- **When** the SP starts up
- **Then** a Secret named `<SP_NAME>-pull-secret` of type `kubernetes.io/dockerconfigjson` is created (or updated if existing) in `SP_CLUSTER_NAMESPACE` with the base64-decoded content in the `.dockerconfigjson` key
- **And** the Secret carries labels `dcm.project/managed-by=dcm`, `dcm.project/dcm-service-type=cluster`

##### AC-ACM-191: HostedCluster PullSecret references shared Secret
- **Requirements:** REQ-ACM-191
- **When** the HostedCluster CR is constructed
- **Then** `HostedCluster.Spec.PullSecret.Name` equals `<SP_NAME>-pull-secret`

##### AC-ACM-195: SP fails to start without SP_PULL_SECRET
- **Requirements:** REQ-ACM-195, REQ-XC-CFG-020
- **Given** the `SP_PULL_SECRET` env var is not set (or is empty)
- **When** the SP starts up
- **Then** the SP fails with a clear error message naming `SP_PULL_SECRET`

##### AC-ACM-200: NodePool has InPlace upgrade type
- **Requirements:** REQ-ACM-200
- **Given** a cluster is being created (any platform)
- **When** the NodePool CR is constructed
- **Then** `Spec.Management.UpgradeType` is set to `InPlace`

---

#### 5.5.1 KubeVirt Platform

##### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-KV-010 | MUST set `spec.platform.type=KubeVirt` on the HostedCluster CR | MUST |
| REQ-KV-020 | MUST create the NodePool CR with a KubeVirt VM template | MUST |
| REQ-KV-030 | `nodes.workers.cpu`, `memory`, and `storage` MUST map to KubeVirt VM template resource specifications | MUST |
| REQ-KV-040 | `workers.storage` MUST map to the KubeVirt VM root disk size | MUST |

##### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_KUBEVIRT_INFRA_KUBECONFIG` | string | _(empty)_ | Kubeconfig for KubeVirt infrastructure cluster |
| `SP_KUBEVIRT_INFRA_NAMESPACE` | string | _(empty)_ | Namespace for KubeVirt worker VMs |

##### Acceptance Criteria

###### AC-KV-010: Create KubeVirt cluster with correct platform type
- **Requirements:** REQ-KV-010, REQ-KV-020
- **Given** the ACM Hub is reachable and a ClusterImageSet exists
- **When** CreateCluster is called with `platform="kubevirt"`, `version="1.28"`, `workers={count:2, cpu:4, memory:"16GB", storage:"120GB"}`
- **Then** HostedCluster CR has `platform.type=KubeVirt`
- **And** NodePool CR has a KubeVirt VM template
- **And** both CRs carry DCM labels

###### AC-KV-020: Workers map to KubeVirt VM template
- **Requirements:** REQ-KV-030
- **When** CreateCluster is called with `workers={cpu:4, memory:"16GB"}`
- **Then** the NodePool VM template specifies the corresponding compute resources

###### AC-KV-030: Worker storage maps to root disk size
- **Requirements:** REQ-KV-040
- **When** CreateCluster is called with `workers.storage="120GB"`
- **Then** the NodePool VM template root disk size is set accordingly

---

#### 5.5.2 BareMetal Platform

##### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-BM-010 | MUST set `spec.platform.type=Agent` on the HostedCluster CR | MUST |
| REQ-BM-020 | MUST create the NodePool CR with `spec.platform.type=Agent` | MUST |
| REQ-BM-030 | `infra_env` MUST be resolved from `provider_hints.acm.infra_env` (request-level), then `SP_DEFAULT_INFRA_ENV` (config-level fallback). If neither is available, the operation MUST fail with an error indicating `infra_env` is required | MUST |
| REQ-BM-040 | The InfraEnv reference MUST be set on the NodePool for agent discovery | MUST |
| REQ-BM-050 | If `provider_hints.acm.agent_labels` are specified, they MUST be used as label selectors to match available agents | MUST |
| REQ-BM-060 | `nodes.workers.cpu`, `memory`, and `storage` are informational for baremetal — actual resources depend on physical hardware | MUST |

##### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_DEFAULT_INFRA_ENV` | string | _(empty)_ | Default InfraEnv name if not specified in request |
| `SP_AGENT_NAMESPACE` | string | _(empty)_ | Namespace where Agents are registered |

##### Acceptance Criteria

###### AC-BM-010: Create BareMetal cluster with Agent platform
- **Requirements:** REQ-BM-010, REQ-BM-020, REQ-BM-040, REQ-BM-050
- **Given** the ACM Hub is reachable and a ClusterImageSet exists
- **When** CreateCluster is called with `platform="baremetal"`, `infra_env="my-infra"`, `agent_labels={"location":"dc1"}`
- **Then** HostedCluster CR has `platform.type=Agent`
- **And** NodePool CR references `infra_env="my-infra"` and agent label selector `{"location":"dc1"}`
- **And** both CRs carry DCM labels

###### AC-BM-020: BareMetal infra_env resolution
- **Requirements:** REQ-BM-030
- **When** CreateCluster is called with `platform="baremetal"` but no `infra_env`
- **And** no `SP_DEFAULT_INFRA_ENV` is configured
- **Then** the operation fails with an error indicating `infra_env` is required

###### AC-BM-030: Worker resources are informational for BareMetal
- **Requirements:** REQ-BM-060
- **When** CreateCluster is called with `workers.cpu=8, workers.memory="32GB"`
- **Then** the NodePool is created with `replicas=<count>`
- **And** the cpu/memory values do not constrain agent selection (physical hardware determines resources)

---

#### Requirement Migration Notes

The following table maps old requirement IDs (from the original Topic 5: KubeVirt and Topic 6: BareMetal) to their new IDs in the merged Topic 5: ACM Platform Services.

| Old ID | New ID | Notes |
|--------|--------|-------|
| REQ-KV-010 (old) | REQ-ACM-010, REQ-KV-010 | Create HC split: common creation + platform type |
| REQ-KV-020 (old) | REQ-ACM-010, REQ-KV-020 | Create NP split: common creation + VM template |
| REQ-KV-030 (old) | REQ-ACM-020 | DCM labels → common |
| REQ-KV-040 (old) | REQ-ACM-060 | control_plane cpu/memory → common |
| REQ-KV-050 (old) | REQ-ACM-070 | control_plane.count ignored → common |
| REQ-KV-060 (old) | REQ-ACM-080 | control_plane.storage ignored → common |
| REQ-KV-070 (old) | REQ-ACM-090, REQ-KV-030 | Split: count→common replicas, cpu/mem/storage→KV VM template |
| REQ-KV-080 (old) | REQ-ACM-030 | ClusterImageSet validation → common |
| REQ-KV-090 (old) | REQ-ACM-040 | No ClusterImageSet → fail → common |
| REQ-KV-100 (old) | REQ-ACM-050, REQ-ACM-051 | Release image override → common (split: feature support vs behavior) |
| REQ-KV-120 (old) | REQ-ACM-100 | Configurable namespace → common |
| REQ-KV-130 (old) | REQ-ACM-110 | Read: query HC, map conditions → common |
| REQ-KV-140 (old) | REQ-ACM-120 | READY: extract api_endpoint → common |
| REQ-KV-150 (old) | REQ-ACM-130 | READY: extract kubeconfig → common |
| REQ-KV-160 (old) | REQ-ACM-140 | Delete HC (cascading) → common |
| REQ-KV-170 (old) | REQ-ACM-150 | Memory/storage format conversion → common |
| REQ-KV-180 (old) | REQ-ACM-160 | Status precedence rules → common |
| REQ-KV-190 (old) | REQ-KV-040 | workers.storage → root disk (KubeVirt-specific) |
| REQ-KV-200 (old) | REQ-ACM-170 | Partial create failure cleanup → common |
| REQ-BM-010 (old) | REQ-BM-010 | platform.type=Agent (renumbered, same concept) |
| REQ-BM-020 (old) | REQ-BM-020 | NodePool with Agent platform (renumbered) |
| REQ-BM-030 (old) | REQ-ACM-020 | DCM labels → common |
| REQ-BM-040 (old) | REQ-BM-030 | infra_env resolution (renumbered, enhanced with fallback) |
| REQ-BM-050 (old) | REQ-BM-040 | InfraEnv reference on NodePool (renumbered) |
| REQ-BM-060 (old) | REQ-BM-050 | Agent label selectors (renumbered) |
| REQ-BM-070 (old) | REQ-ACM-090 | workers.count → replicas → common |
| REQ-BM-080 (old) | REQ-BM-060 | Worker resources informational (renumbered) |
| REQ-BM-090 (old) | REQ-ACM-160 | Status precedence rules → common |
| REQ-BM-100 (old) | REQ-ACM-140 | Delete HC (cascading) → common |
| REQ-BM-110 (old) | REQ-ACM-170 | Partial create failure cleanup → common |

---

### 5.6 Topic 6: Cluster Status Monitoring

#### Overview

Background controller that watches HostedCluster resources via `SharedIndexInformer`, detects status transitions, and publishes CloudEvents to NATS. Follows Pattern A (Event-Driven Streaming) from [`service-provider-status-report-implementation.md`](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-status-report-implementation/service-provider-status-report-implementation.md).

#### Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-MON-010 | MUST use a `SharedIndexInformer` (or equivalent) to watch HostedCluster resources | MUST |
| REQ-MON-020 | The informer MUST filter by labels `dcm.project/managed-by=dcm` AND `dcm.project/dcm-service-type=cluster` | MUST |
| REQ-MON-030 | On condition changes, MUST map HostedCluster conditions to DCM status using the Status Mapping Table (section 5.5) | MUST |
| REQ-MON-035 | The informer MUST maintain a secondary index on `dcm.project/dcm-instance-id` label for fast lookups | MUST |
| REQ-MON-040 | On status change, MUST publish a CloudEvent to NATS | MUST |
| REQ-MON-050 | CloudEvent `subject` MUST be set to `dcm.cluster` (the NATS subject / service-type identifier) | MUST |
| REQ-MON-060 | CloudEvent `type` MUST be `dcm.status.cluster` | MUST |
| REQ-MON-070 | CloudEvent `source` MUST be `dcm/providers/<providerName>` where `providerName` is the configured `SP_NAME` value (DD-003) | MUST |
| REQ-MON-080 | `instanceId` MUST be extracted from the HostedCluster's `dcm.project/dcm-instance-id` label | MUST |
| REQ-MON-090 | CloudEvent payload MUST include: `id` (string, instance ID), `status` (string), `message` (string). The JSON keys MUST be lowercase (`"id"`, `"status"`, `"message"`) | MUST |
| REQ-MON-100 | CloudEvents MUST conform to CloudEvents v1.0 specification | MUST |
| REQ-MON-110 | The informer MUST be resilient -- on watch disconnect, it MUST re-list and resume watching | MUST |
| REQ-MON-115 | The monitor MUST implement debounce logic to avoid publishing rapid status oscillations | MUST |
| REQ-MON-125 | Resource watchers MUST be started as async background tasks after the HTTP server is ready | MUST |
| REQ-MON-126 | Resource watchers MUST be stopped during graceful shutdown | MUST |
| REQ-MON-130 | MUST detect HostedCluster deletion events and publish `DELETED` status | MUST |
| REQ-MON-135 | Resource watchers MUST periodically re-reconcile status at the configured `SP_STATUS_RESYNC_INTERVAL` | MUST |
| REQ-MON-140 | On startup, after initial cache sync (re-list), the monitor MUST publish a status CloudEvent for every existing DCM-managed HostedCluster. Consumers MUST be designed to handle these idempotent status re-publications on SP restart | MUST |
| REQ-MON-150 | The Status Monitor MUST publish events through a `StatusPublisher` interface. It MUST NOT depend directly on a NATS client | MUST |
| REQ-MON-155 | When status is FAILED, the CloudEvent message MUST include the failure reason when available from HostedCluster conditions | MUST |
| REQ-MON-160 | On `StatusPublisher.Publish` failure, the monitor MUST retry with configurable interval and max attempts (`SP_NATS_PUBLISH_RETRY_INTERVAL`, `SP_NATS_PUBLISH_RETRY_MAX`). On exhaustion, MUST log and drop the event without blocking subsequent events | MUST |
| REQ-MON-170 | The SP MUST handle NATS connection failures gracefully. If NATS is unreachable at startup, the monitor SHOULD buffer or drop events and retry connection. NATS availability MUST NOT block SP startup | MUST |

> **Note (missing `dcm-instance-id`):** If a HostedCluster matches the informer's label selector (`dcm.project/managed-by=dcm`, `dcm.project/dcm-service-type=cluster`) but lacks the `dcm.project/dcm-instance-id` label, the monitor MUST skip the resource and log a warning. It MUST NOT panic or publish a CloudEvent with an empty `instanceId`.

> **Note (duplicate `dcm-instance-id`):** If multiple HostedClusters share the same `dcm.project/dcm-instance-id` label, the monitor may publish conflicting status events for the same instanceId. The SP relies on `dcm.project/dcm-instance-id` uniqueness being enforced at creation time (REQ-API-102). The monitor SHOULD log a warning if it detects duplicate `dcm.project/dcm-instance-id` values.

#### Configuration Introduced

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SP_NATS_URL` | string | _(required)_ | NATS server URL |
| `SP_STATUS_DEBOUNCE_INTERVAL` | duration | `1s` | Minimum interval between status updates per instance |
| `SP_STATUS_RESYNC_INTERVAL` | duration | `10m` | Informer cache resync period |
| `SP_NATS_PUBLISH_RETRY_MAX` | int | `3` | Maximum publish retry attempts |
| `SP_NATS_PUBLISH_RETRY_INTERVAL` | duration | `2s` | Initial retry backoff interval for publish failures |

#### Acceptance Criteria

##### AC-MON-010: Status change triggers CloudEvent
- **Requirements:** REQ-MON-040, REQ-MON-050, REQ-MON-060, REQ-MON-090
- **Given** the informer is watching HostedCluster resources
- **And** a HostedCluster with `dcm.project/dcm-instance-id="my-cluster"` exists
- **When** the HostedCluster conditions change to `Available=True`, `Progressing=False`
- **Then** a CloudEvent is published to NATS with type `dcm.status.cluster` and subject `dcm.cluster`
- **And** the payload contains `id="my-cluster"` and `status="READY"`

##### AC-MON-020: HostedCluster deletion detected
- **Requirements:** REQ-MON-130
- **Given** a HostedCluster with `dcm.project/dcm-instance-id="my-cluster"` exists
- **When** the HostedCluster is deleted from K8s
- **Then** a CloudEvent is published with `status="DELETED"`

##### AC-MON-030: Non-DCM resources are ignored
- **Requirements:** REQ-MON-020
- **Given** a HostedCluster exists WITHOUT labels `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=cluster`
- **When** its conditions change
- **Then** no CloudEvent is published

##### AC-MON-040: Informer reconnects after watch disconnect
- **Requirements:** REQ-MON-110
- **Given** the informer is watching
- **When** the K8s API watch connection drops
- **Then** the informer re-lists and resumes watching
- **And** no status changes are missed

##### AC-MON-050: Debounce rapid oscillations
- **Requirements:** REQ-MON-115
- **Given** a HostedCluster oscillates between `Progressing=True` and `Degraded=True` within 500ms
- **When** the informer detects these changes
- **Then** at most one CloudEvent per instance is published within `SP_STATUS_DEBOUNCE_INTERVAL`

##### AC-MON-060: Restart re-publishes current statuses
- **Requirements:** REQ-MON-140
- **Given** the SP restarts
- **And** 3 DCM-managed HostedClusters exist in K8s
- **When** the informer completes its initial list
- **Then** a CloudEvent is published for each of the 3 clusters with their current status
- **And** consumers treat these as idempotent status updates

##### AC-MON-070: CloudEvent type follows DCM standard
- **Requirements:** REQ-MON-060
- **Given** a status change occurs for a cluster
- **When** a CloudEvent is published
- **Then** the event type is `dcm.status.cluster`

##### AC-MON-080: CloudEvent conforms to v1.0 spec
- **Requirements:** REQ-MON-100
- **Given** a status change occurs for a cluster
- **When** a CloudEvent is published
- **Then** the event includes all required CloudEvents v1.0 attributes: `specversion`, `id`, `source`, `type`

##### AC-MON-090: Publish retry on failure
- **Requirements:** REQ-MON-160
- **Given** `StatusPublisher.Publish` returns an error
- **When** the monitor handles the failure
- **Then** it retries up to `SP_NATS_PUBLISH_RETRY_MAX` times with `SP_NATS_PUBLISH_RETRY_INTERVAL` backoff
- **And** on exhaustion, it logs the failure and continues processing subsequent events

##### AC-MON-100: Watchers start after HTTP server
- **Requirements:** REQ-MON-125
- **Given** the SP is starting up
- **When** the HTTP server is ready to accept requests
- **Then** resource watchers are started as async background tasks

##### AC-MON-110: Watchers stop during graceful shutdown
- **Requirements:** REQ-MON-126
- **Given** the SP receives a shutdown signal
- **When** graceful shutdown begins
- **Then** resource watchers are stopped before the process exits

##### AC-MON-120: Periodic resync triggers re-evaluation
- **Requirements:** REQ-MON-135
- **Given** `SP_STATUS_RESYNC_INTERVAL=10m`
- **And** the informer has been watching for more than 10 minutes
- **When** the resync interval elapses
- **Then** the informer re-evaluates status for all cached resources
- **And** any status changes are published as CloudEvents

##### AC-MON-130: Initial status sync publishes events for all existing resources
- **Requirements:** REQ-MON-140
- **Given** 5 DCM-managed HostedClusters exist in K8s
- **When** the SP starts and the initial cache sync completes
- **Then** a status CloudEvent is published for each of the 5 clusters

##### AC-MON-140: FAILED events include failure reason
- **Requirements:** REQ-MON-155
- **Given** a HostedCluster has `Degraded=True` with a condition message "etcd cluster unhealthy"
- **When** the informer detects the status change to FAILED
- **Then** the CloudEvent message includes the failure reason "etcd cluster unhealthy"

##### AC-MON-145: Informer indexes HostedClusters by dcm.project/dcm-instance-id
- **Requirements:** REQ-MON-035
- **Given** the informer is watching HostedCluster resources
- **When** a HostedCluster with a `dcm.project/dcm-instance-id` label is added or updated
- **Then** the resource is indexed by its `dcm.project/dcm-instance-id` label value
- **And** the index is available for efficient lookups by instance ID

##### AC-MON-150: NATS unavailability does not block SP startup
- **Requirements:** REQ-MON-170
- **Given** the NATS server is unreachable at the configured `SP_NATS_URL`
- **When** the SP starts up
- **Then** the SP starts successfully without blocking
- **And** the status monitor begins watching HostedCluster resources
- **And** publish failures are handled by the existing retry/drop mechanism (REQ-MON-160)

##### AC-MON-155: Status monitor publishes through StatusPublisher interface
- **Requirements:** REQ-MON-150
- **Given** the status monitor is configured with a `StatusPublisher` implementation
- **When** a status change is detected
- **Then** the event is published through the `StatusPublisher` interface
- **And** the monitor has no direct dependency on a NATS client

#### Dependencies

- **Topic 1 (DCM Registration)**: provider name for CloudEvent `source` attribute
- **Topic 5 (ACM Platform Services)**: needs existing HostedCluster resources to watch

---

## 6. Cross-Cutting Concerns

### 6.1 Error Handling

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-ERR-010 | All HTTP error responses MUST use `application/problem+json` content-type | MUST |
| REQ-XC-ERR-020 | All error responses MUST include `type`, `title`, and `status` fields | MUST |
| REQ-XC-ERR-030 | Error responses SHOULD include `detail` and `instance` for tracing | SHOULD |
| REQ-XC-ERR-040 | Internal errors MUST NOT leak implementation details to clients | MUST |

Error type to HTTP status mapping:

| ErrorType | HTTP Status |
|-----------|-------------|
| `INVALID_ARGUMENT` | 400 |
| `NOT_FOUND` | 404 |
| `ALREADY_EXISTS` | 409 |
| `UNPROCESSABLE_ENTITY` | 422 |
| `INTERNAL` | 500 |
| `UNAVAILABLE` | 503 |

### 6.2 Resource Labeling

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-LBL-010 | Every K8s resource created by the SP MUST carry labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=<id>`, `dcm.project/dcm-service-type=cluster`. All DCM labels MUST use the `dcm.project/` prefix | MUST |
| REQ-XC-LBL-020 | These labels MUST be set at creation time and MUST NOT be modified | MUST |
| REQ-XC-LBL-030 | The informer MUST use both labels `dcm.project/managed-by=dcm` AND `dcm.project/dcm-service-type=cluster` as label selector | MUST |

### 6.3 Kubernetes API Integration

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-K8S-010 | MUST interact with the ACM Hub via the Kubernetes API | MUST |
| REQ-XC-K8S-020 | MUST support in-cluster (ServiceAccount) and out-of-cluster (kubeconfig) authentication modes | MUST |
| REQ-XC-K8S-030 | Kubernetes API calls SHOULD include context-based timeouts | SHOULD |

### 6.4 Logging

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-LOG-010 | MUST use structured logging (key-value pairs) | MUST |
| REQ-XC-LOG-020 | API request logs SHOULD include: request ID, method, path, status, duration | SHOULD |
| REQ-XC-LOG-030 | K8s operation logs SHOULD include: resource kind, name, namespace, operation | SHOULD |

### 6.5 Configuration

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-CFG-010 | All configuration MUST be provided via environment variables | MUST |
| REQ-XC-CFG-020 | Required config variables MUST cause fail-fast at startup with clear error messages if missing | MUST |

### 6.6 Resource Identity

| ID | Requirement | Priority |
|----|-------------|----------|
| REQ-XC-ID-010 | Two identifiers MUST be used: `id` (DCM identifier, stored as `dcm.project/dcm-instance-id` label, used in URL paths) and `metadata.name` (K8s resource name, used as HostedCluster/NodePool name) | MUST |
| REQ-XC-ID-020 | Conflict detection MUST check both `id` uniqueness (via `dcm.project/dcm-instance-id` label) and `metadata.name` uniqueness (via K8s name). Both constraints apply independently | MUST |

## 7. Consolidated Configuration Reference

| Variable | Topic | Type | Default | Required |
|----------|-------|------|---------|----------|
| `SP_SERVER_ADDRESS` | 2 | string | `:8080` | No |
| `SP_SERVER_SHUTDOWN_TIMEOUT` | 2 | duration | `15s` | No |
| `SP_SERVER_REQUEST_TIMEOUT` | 2 | duration | `30s` | No |
| `SP_SERVER_READ_TIMEOUT` | 2 | duration | `15s` | No |
| `SP_SERVER_WRITE_TIMEOUT` | 2 | duration | `15s` | No |
| `SP_SERVER_IDLE_TIMEOUT` | 2 | duration | `60s` | No |
| `DCM_REGISTRATION_URL` | 1 | string | - | Yes |
| `SP_NAME` | 1 | string | `acm-cluster-sp` | No |
| `SP_ENDPOINT` | 1 | string | - | Yes |
| `SP_REGISTRATION_INITIAL_BACKOFF` | 1 | duration | `1s` | No |
| `SP_REGISTRATION_MAX_BACKOFF` | 1 | duration | `5m` | No |
| `SP_VERSION_CHECK_INTERVAL` | 1 | duration | `5m` | No |
| `SP_DISPLAY_NAME` | 1 | string | _(empty)_ | No |
| `SP_REGION` | 1 | string | _(empty)_ | No |
| `SP_ZONE` | 1 | string | _(empty)_ | No |
| `SP_HUB_KUBECONFIG` | 5 | string | _(in-cluster)_ | No |
| `SP_CLUSTER_NAMESPACE` | 5 | string | - | Yes |
| `SP_CONSOLE_URI_PATTERN` | 5 | string | `https://console-openshift-console.apps.{name}.{base_domain}` | No |
| `SP_VERSION_MATRIX_PATH` | 5 | string | _(empty)_ | No |
| `SP_KUBEVIRT_INFRA_KUBECONFIG` | 5 (KubeVirt) | string | - | No |
| `SP_KUBEVIRT_INFRA_NAMESPACE` | 5 (KubeVirt) | string | - | No |
| `SP_DEFAULT_INFRA_ENV` | 5 (BareMetal) | string | - | No |
| `SP_BASE_DOMAIN` | 4, 5 | string | _(empty)_ | No |
| `SP_AGENT_NAMESPACE` | 5 (BareMetal) | string | - | No |
| `SP_HEALTH_CHECK_TIMEOUT` | 3 | duration | `5s` | No |
| `SP_ENABLED_PLATFORMS` | 3 | string | `kubevirt,baremetal` | No |
| `SP_NATS_URL` | 6 | string | - | Yes |
| `SP_STATUS_DEBOUNCE_INTERVAL` | 6 | duration | `1s` | No |
| `SP_STATUS_RESYNC_INTERVAL` | 6 | duration | `10m` | No |
| `SP_NATS_PUBLISH_RETRY_MAX` | 6 | int | `3` | No |
| `SP_NATS_PUBLISH_RETRY_INTERVAL` | 6 | duration | `2s` | No |
| `SP_PULL_SECRET` | 5 | string | - | Yes |

## 8. Requirement ID Index

| Prefix | Topic | Count |
|--------|-------|-------|
| REQ-REG-NNN | 1: DCM Registration | 14 |
| REQ-HTTP-NNN | 2: HTTP Server | 11 |
| REQ-HLT-NNN | 3: Health Service | 12 |
| REQ-API-NNN | 4: OpenAPI Endpoints | 51 |
| REQ-ACM-NNN | 5: ACM Platform Services (Common) | 26 |
| REQ-KV-NNN | 5a: ACM - KubeVirt | 4 |
| REQ-BM-NNN | 5b: ACM - BareMetal | 6 |
| REQ-MON-NNN | 6: Status Monitoring | 22 |
| REQ-XC-* | Cross-cutting (6.1-6.6) | 17 |

# ACM Cluster Service Provider

Service provider for managing OpenShift clusters via Red Hat Advanced Cluster Management (ACM) and HyperShift, as part of the [Data Center Management (DCM)](https://github.com/dcm-project) project.

## Overview

The ACM Cluster Service Provider (SP) is a component in the DCM ecosystem that provisions and manages OpenShift clusters using [HyperShift](https://hypershift-docs.netlify.app/) hosted control planes on an ACM Hub cluster. It exposes a REST API that the DCM platform consumes for cluster lifecycle operations.

### How it works

```
  +-------------------------------+           +-------------------------+
  | Service Provider Manager      |           |         NATS            |
  | (dcm-project/                 |           |   (status events bus)   |
  |  service-provider-manager)    |           |                         |
  +-------^-----------+-----------+           +------------^------------+
          |           |                                     |
          | register  | health checks                      | CloudEvents
          | (startup) | (periodic)                         | (dcm.cluster)
          |           |                                     |
  +-------+-----------v-------------------------------------+-----------+
  |                   ACM Cluster Service Provider                      |
  |                   (this repository)                                 |
  |                                                                     |
  |   +---------------+    +------------+    +------------------------+ |
  |   | Registration  |    | Cluster    |    | Status Monitoring      | |
  |   | & Version     |    | Service    |    | (K8s dynamic informers)| |
  |   | Discovery     |    | (CRUD)     |    |                        | |
  |   +---------------+    +------------+    +------------------------+ |
  +---------------------------------+-----------------------------------+
                                    |
                                    | K8s API (controller-runtime)
                                    |
  +---------------------------------v-----------------------------------+
  |                       ACM Hub Cluster                               |
  |   HostedCluster | NodePool | ClusterImageSet | Secrets              |
  +---------------------------------------------------------------------+
```

On startup, the SP:

1. Loads configuration from environment variables
2. Connects to the Kubernetes API (ACM Hub cluster)
3. Creates the shared pull secret in the target namespace
4. Starts the HTTP server
5. Registers with the [DCM Service Provider Manager](https://github.com/dcm-project/service-provider-manager), advertising its supported platforms and Kubernetes versions
6. Starts the status monitor, which watches `HostedCluster` and `NodePool` resources and publishes status changes as [CloudEvents](https://cloudevents.io/) to NATS
7. Periodically re-checks available `ClusterImageSet` resources and re-registers if versions change

### Cluster lifecycle

- **Create** -- The SP creates a HyperShift `HostedCluster` and `NodePool` in the configured namespace. Creation is asynchronous: the API returns immediately with `PENDING` status. The status monitor detects state transitions and publishes updates.
- **Get / List** -- Reads cluster state directly from the `HostedCluster` resources on the Hub cluster. Listing is paginated.
- **Delete** -- Deletes the `NodePool` first, then the `HostedCluster`. Deletion is asynchronous.

### Supported platforms

| Platform | HyperShift type | Description |
|----------|----------------|-------------|
| `kubevirt` | `KubevirtPlatform` | Deploy clusters on KubeVirt virtualization. Worker nodes run as VMs. |
| `baremetal` | `AgentPlatform` | Deploy clusters on bare metal infrastructure via the Agent-based installer. Requires an `InfraEnv` resource. |

### Version discovery

Available Kubernetes versions are discovered dynamically by listing `ClusterImageSet` resources on the ACM Hub and mapping OCP versions to Kubernetes versions using a compatibility matrix (OCP 4.x = K8s 1.(x+13)). The matrix can be overridden via a JSON file (see `SP_VERSION_MATRIX_PATH` in [RUN.md](RUN.md)).

## API

All endpoints are under the `/api/v1alpha1` base path. The full OpenAPI 3.0.4 specification is at [`api/v1alpha1/openapi.yaml`](api/v1alpha1/openapi.yaml).

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1alpha1/clusters` | Create a cluster (accepts optional `?id=` for client-assigned IDs) |
| `GET` | `/api/v1alpha1/clusters` | List clusters (paginated via `?page_token=` and `?max_page_size=`) |
| `GET` | `/api/v1alpha1/clusters/{clusterId}` | Get cluster details |
| `DELETE` | `/api/v1alpha1/clusters/{clusterId}` | Delete a cluster |
| `GET` | `/api/v1alpha1/clusters/health` | Health check |

### Cluster statuses

`PENDING` > `PROVISIONING` > `READY` | `FAILED` | `UNAVAILABLE` > `DELETING` > `DELETED`

### Error format

Errors follow [RFC 7807](https://datatracker.ietf.org/doc/html/rfc7807) (`application/problem+json`).

## Health check

`GET /api/v1alpha1/clusters/health` probes:

1. **Kubernetes API connectivity** -- can the SP reach the API server?
2. **HyperShift CRD** -- is the `HostedCluster` CRD installed?
3. **Platform CRDs** (per enabled platform):
   - `kubevirt`: `VirtualMachineInstance` CRD from `kubevirt.io`
   - `baremetal`: `Agent` CRD from `agent-install.openshift.io`

Returns `"healthy"` if all checks pass, `"unhealthy"` if any fail. Always includes `version` (build version) and `uptime` (seconds since start).

## Status monitoring

The status monitor uses Kubernetes [dynamic informers](https://pkg.go.dev/k8s.io/client-go/dynamic/dynamicinformer) to watch `HostedCluster` and `NodePool` resources labeled with `dcm.project/managed-by=dcm`. When a status change is detected:

1. The change is debounced (configurable via `SP_STATUS_DEBOUNCE_INTERVAL`)
2. A [CloudEvents v1.0](https://cloudevents.io/) message is published to the NATS subject `dcm.cluster`
3. The event type is `dcm.status.cluster` with source `dcm/providers/{SP_NAME}`

The event payload:

```json
{
  "specversion": "1.0",
  "id": "<uuid>",
  "source": "dcm/providers/acm-cluster-sp",
  "type": "dcm.status.cluster",
  "subject": "dcm.cluster",
  "time": "2026-01-15T10:30:00Z",
  "datacontenttype": "application/json",
  "data": {
    "id": "<cluster-instance-id>",
    "status": "READY",
    "message": "Cluster is available"
  }
}
```

## Deployment

### Container image

The container image is built from [`Containerfile`](Containerfile) (multi-stage, UBI 9 minimal runtime) and published to:

```
quay.io/dcm-project/acm-cluster-service-provider
```

Image tags depend on the trigger:

- Push to `main` → `:main` + short SHA (e.g., `:abc1234`)
- `v*` tags → tag name (e.g., `:v1.0.0`) + short SHA
- `release/v*` branches → short SHA only
- Manual dispatch (`workflow_dispatch`) → specified version tag only

### Full DCM stack

To deploy all DCM services together (API Gateway, Service Provider Manager, NATS, and this SP), use the [dcm-project/api-gateway](https://github.com/dcm-project/api-gateway) repository, which provides the orchestration layer for the complete DCM stack.

### Standalone

See [RUN.md](RUN.md) for prerequisites, all configuration environment variables, and CRD requirements.

Minimal example:

```bash
export DCM_REGISTRATION_URL="http://spm:8080"
export SP_ENDPOINT="http://acm-cluster-sp:8080"
export SP_CLUSTER_NAMESPACE="clusters"
export SP_PULL_SECRET="$(echo '{"auths":{"cloud.openshift.com":{"auth":"..."}}}' | base64 -w0)"
export SP_NATS_URL="nats://nats:4222"
export SP_BASE_DOMAIN="example.com"

./acm-cluster-service-provider
```

## Development

### Prerequisites

- Go 1.25.5+
- Access to an ACM Hub cluster with HyperShift enabled (for integration testing)
- `golangci-lint` (for linting)
- `spectral` CLI (for OpenAPI linting, `npm install -g @stoplight/spectral-cli`)

### Build

```bash
make build          # compile binary to bin/
make run            # go run
```

### Test

```bash
make test           # run all tests (Ginkgo)
make test-cover     # run with coverage
```

### Code generation

The API types, server stubs, embedded spec, and HTTP client are generated from the OpenAPI spec using [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen):

```bash
make generate-api         # regenerate all generated code
make check-generate-api   # verify generated code is up-to-date (used in CI)
```

After modifying [`api/v1alpha1/openapi.yaml`](api/v1alpha1/openapi.yaml), always run `make generate-api`.

### Lint

```bash
make lint       # golangci-lint
make check-aep  # OpenAPI AEP compliance (requires spectral)
make vet        # go vet
make check      # fmt + vet + lint + test
```

### CI/CD

All CI runs on GitHub Actions using reusable workflows from [dcm-project/shared-workflows](https://github.com/dcm-project/shared-workflows).

| Workflow | File | Triggers | Purpose |
|----------|------|----------|---------|
| CI | `ci.yaml` | Push to `main`, PRs to `main` | Runs tests and `go vet` via shared Go CI workflow |
| Lint | `lint.yaml` | Push to `main`, PRs to `main` | Runs `golangci-lint` via shared lint workflow |
| Check Generated Code | `check-generate.yaml` | Push to `main`, PRs to `main` | Verifies generated code is in sync with the OpenAPI spec |
| Check AEP Compliance | `check-aep.yaml` | Push to `main`, PRs to `main` | Spectral AEP compliance linting on the OpenAPI spec |
| Check Clean Commits | `check-clean-commits.yaml` | PRs to `main` only | Commit hygiene checks |
| Build and Push Image | `build-push-quay.yaml` | Push to `main` or `release/v*`, `v*` tags, manual dispatch | Builds container image and pushes to Quay (see [Container image](#container-image)) |

## Project structure

```
acm-cluster-service-provider/
├── api/v1alpha1/                   # OpenAPI spec + generated domain types
│   ├── openapi.yaml                # OpenAPI 3.0.4 specification (source of truth)
│   ├── types.gen.go                # Generated type definitions
│   └── spec.gen.go                 # Embedded OpenAPI spec bytes
├── cmd/acm-cluster-service-provider/
│   └── main.go                     # Entrypoint: wiring, startup, shutdown
├── internal/
│   ├── api/server/                 # Generated Chi HTTP server stubs
│   ├── apiserver/                  # HTTP server lifecycle (listen, serve, shutdown)
│   ├── cluster/                    # Cluster CRUD operations, resource conversion
│   │   ├── kubevirtprovider/       # KubeVirt platform implementation
│   │   ├── baremetal/              # Bare metal (Agent) platform implementation
│   │   └── dispatcher/             # Routes requests to the correct platform
│   ├── config/                     # Environment-based configuration (caarlos0/env)
│   ├── handler/                    # API request handler (strict server interface)
│   ├── health/                     # Health checker (K8s API + CRD probes)
│   ├── monitoring/                 # Status monitor (K8s informers + NATS publisher)
│   ├── registration/               # DCM registration + version discovery
│   ├── service/                    # Domain error types and service interfaces
│   └── util/                       # Shared utilities (Ptr, GVK constants)
├── pkg/client/                     # Generated HTTP client for this SP's API
├── hack/                           # Developer scripts
├── Containerfile                   # Multi-stage container build (UBI 9)
├── Makefile                        # Build, test, generate, lint targets
└── tools.go                        # Tool dependencies (oapi-codegen, ginkgo)
```

## DCM registration payload

The SP registers with the Service Provider Manager using:

```json
{
  "name": "acm-cluster-sp",
  "serviceType": "cluster",
  "schemaVersion": "v1alpha1",
  "endpoint": "<SP_ENDPOINT>",
  "operations": ["CREATE", "DELETE", "READ"],
  "metadata": {
    "supportedPlatforms": ["kubevirt", "baremetal"],
    "supportedProvisioningTypes": ["hypershift"],
    "kubernetesSupportedVersions": ["1.29", "1.30", "1.31"]
  }
}
```

Versions are dynamic and reflect the `ClusterImageSet` resources on the Hub.

### Releasing

Images are pushed to `quay.io/dcm-project/acm-cluster-service-provider`.
See [Releasing](https://github.com/dcm-project/shared-workflows#release-flow)
in shared-workflows for the full release process, tag behavior, and version conventions.


## License

[Apache License 2.0](LICENSE)

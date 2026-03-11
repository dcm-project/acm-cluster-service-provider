# ACM Cluster Service Provider

Service provider for managing OpenShift clusters via Red Hat Advanced Cluster Management (ACM) and HyperShift.

## Overview

This service provider is part of the Data Center Management (DCM) project and provides cluster lifecycle management capabilities:

- **Create** - Provision new OpenShift clusters via HyperShift hosted control planes
- **Read** - Query cluster status and details
- **Delete** - Deprovision clusters

### Supported Platforms

- **kubevirt** - Deploy clusters on KubeVirt virtualization
- **baremetal** - Deploy clusters on bare metal infrastructure

### OpenShift Versions

Available versions are determined dynamically based on ClusterImageSets available on the ACM Hub cluster.

## Prerequisites

- Go 1.25.5+
- Access to an ACM Hub cluster with HyperShift enabled
- `oapi-codegen` (installed via `go mod download`)

## Development

### Build

```bash
make build
```

### Run Tests

```bash
make test
```

### Code Generation

After modifying `api/v1alpha1/openapi.yaml`, regenerate the API code:

```bash
make generate-api
```

To verify generated code is up-to-date:

```bash
make check-generate-api
```

### OpenAPI Linting

Lint the OpenAPI specification against AEP rules:

```bash
# Install Spectral CLI first
npm install -g @stoplight/spectral-cli

# Run linting
make check-aep
```

## Project Structure

```
├── api/v1alpha1/           # OpenAPI spec and generated types
│   ├── openapi.yaml        # OpenAPI 3.0.4 specification
│   ├── types.gen.go        # Generated type definitions
│   └── spec.gen.go         # Embedded OpenAPI spec
├── cmd/                    # Application entrypoints
├── internal/api/server/    # Generated Chi server
├── pkg/client/             # Generated HTTP client
└── .github/workflows/      # CI/CD pipelines
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1alpha1/clusters` | Create a cluster |
| GET | `/api/v1alpha1/clusters` | List clusters |
| GET | `/api/v1alpha1/clusters/{clusterId}` | Get cluster details |
| DELETE | `/api/v1alpha1/clusters/{clusterId}` | Delete a cluster |
| GET | `/api/v1alpha1/health` | Health check |

## DCM Registration

This service provider registers with DCM using the following metadata:

- **Name:** `acm-cluster-sp`
- **ServiceType:** `cluster`
- **Operations:** `CREATE`, `DELETE`, `READ`

## License

Apache License 2.0

---
paths:
  - "**/*.go"
  - ".golangci.yml"
  - ".spectral.yaml"
  - "api/v1alpha1/openapi.yaml"
---

# Linting

- `golangci-lint` v2 with extensive linter set (`.golangci.yml`)
- Generated files excluded (`api/v1alpha1`, `pkg/client` paths excluded)
- `ginkgolinter` enabled for test style enforcement
- `gofumpt` + `goimports` for formatting
- Spectral for OpenAPI AEP compliance (`.spectral.yaml` extends `aep-dev/aep-openapi-linter`)

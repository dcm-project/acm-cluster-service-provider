---
paths:
  - "**/*.go"
---

# Code Style

- External test packages (`package xxx_test`)
- Test suite bootstrap in separate `_test.go` file (Ginkgo v2 + Gomega)
- Pointer fields: use `util.Ptr(v)` from `internal/util/ptr.go`
- K8s fake client: `controller-runtime/pkg/client/fake` with explicit scheme registration
- Domain error assertions: use `errors.As(err, &domainErr)` not `BeAssignableToTypeOf`
- HyperShift API imported as `hyperv1` (`github.com/openshift/hypershift/api/hypershift/v1beta1`)
- DCM SP Manager client imported as `spmclient`
- Structured logging via `log/slog` with JSON handler
- Compile-time interface checks: `var _ Interface = (*Impl)(nil)`
- Version set at build time via `-ldflags "-X main.version=X.Y.Z"`

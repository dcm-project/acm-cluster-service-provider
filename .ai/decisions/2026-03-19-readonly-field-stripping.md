# Trust Decision: ReadOnly Field Stripping Removed from Handler

## Context
The `CreateCluster` handler previously stripped readOnly fields (`id`, `status`, `api_endpoint`, `kubeconfig`, `create_time`, `update_time`, `path`, `status_message`, `console_uri`) from the request body before passing it to the service layer.

## Trust Decisions

### No readOnly field stripping in handler
- **What defense was omitted:** Explicit nil-assignment of readOnly fields in `handler.CreateCluster` (9 fields)
- **What mechanism is trusted:** kin-openapi `openapi3filter.ValidateRequest` with `VisitAsRequest()` — rejects any request containing a readOnly property with error `readOnly property "X" in request`
- **Where the trust boundary is:** `openAPIValidationMiddleware` in `internal/apiserver/middleware.go`, executed before the request reaches the handler

### Test removed: TC-HDL-CRT-UT-003
- **What was removed:** Unit test "ignores read-only fields in body" that verified stripping behavior
- **Why:** The test bypassed the middleware (unit test calls handler directly) and only passed coincidentally because the mock ignored the service-layer cluster argument. With stripping removed, the test was testing phantom behavior.

## Known Risk
If `GetSwagger()` or `NewRouter()` fails at startup (`internal/apiserver/server.go:46-58`), the validation middleware is silently disabled and readOnly fields would reach the handler unstripped. This is accepted because:
- The service layer controls what fields are persisted/returned regardless of input
- The failure mode is logged (`logger.Warn`)

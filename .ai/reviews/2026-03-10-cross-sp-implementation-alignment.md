# Cross-SP Implementation Alignment: ACM Cluster SP vs K8s Container SP

> **Created:** 2026-03-10
> **Scope:** Registration (Topic 1) and HTTP Server (Topic 2) implementation code
> **Reference:** `github.com/dcm-project/k8s-container-service-provider`

## 1. Scope

This document compares the **implementation code** (not specs or test plans) of the ACM Cluster SP and K8s Container SP for Topics 1 (Registration) and 2 (HTTP Server). It identifies divergences, classifies each as "FIX" or "OK TO DIFFER", and provides recommendations for both teams.

---

## 2. Recommendations for K8s Container SP

Areas where the ACM Cluster SP is ahead or has patterns worth adopting:

### 2.1 Request Logging Middleware
- **ACM has:** `requestLoggingMiddleware` (REQ-HTTP-060) that logs method, path, status code, and duration for every request
- **K8s has:** No request logging middleware
- **Recommendation:** Add structured request logging for observability

### 2.2 Panic Recovery with RFC 7807 Response
- **ACM has:** `rfc7807RecoveryMiddleware` (REQ-HTTP-070) that catches panics and returns a structured `application/problem+json` response with `type=INTERNAL`
- **K8s has:** Recovery that logs but doesn't return a structured error response
- **Recommendation:** Return RFC 7807 `application/problem+json` on panic recovery

### 2.3 Extended Validation Error Handlers
- **ACM handles:** 6 oapi-codegen error types (`RequiredParamError`, `RequiredHeaderError`, `UnescapedCookieParamError`, `UnmarshalingParamError`, `TooManyValuesForParamError`, `InvalidParamFormatError`)
- **K8s handles:** 3 types
- **Recommendation:** Add handlers for `UnescapedCookieParamError`, `UnmarshalingParamError`, `TooManyValuesForParamError`

### 2.4 4xx Non-Retryable Distinction
- **ACM keeps:** 4xx as non-retryable since a bad payload won't fix itself on retry
- **K8s retries:** Everything including 4xx
- **Recommendation:** Consider distinguishing 4xx (non-retryable) from 5xx (retryable) to avoid infinite retry loops on permanent client errors

### 2.5 `writeRFC7807()` Helper
- **ACM pattern:** Centralized helper to reduce error response boilerplate
- **Recommendation:** Consider adopting to reduce duplication in error response paths

---

## 3. Recommendations for ACM Cluster SP

Items being fixed as part of this alignment:

### 3.1 HTTP Server Timeouts (Security — S2/C2)
- **Issue:** No `ReadTimeout`, `WriteTimeout`, `IdleTimeout` on `http.Server`
- **Risk:** Slowloris attacks, connection exhaustion
- **Fix:** Set timeouts from `SP_SERVER_READ_TIMEOUT`, `SP_SERVER_WRITE_TIMEOUT`, `SP_SERVER_IDLE_TIMEOUT`

### 3.2 Middleware Order (S1)
- **Issue:** Logging middleware was outermost; recovery middleware was inner
- **Fix:** Recovery middleware must be outermost so panics in logging are also caught

### 3.3 Readiness Check (S4)
- **Issue:** `waitForReady()` accepted any HTTP response (even 500) as "ready"
- **Fix:** Verify `resp.StatusCode == http.StatusOK`

### 3.4 Infinite Retry (R1)
- **Issue:** Registration gave up after `SP_REGISTRATION_RETRY_MAX` attempts
- **Fix:** Retry indefinitely (matching K8s SP). Keep 4xx as non-retryable

### 3.5 sync.Once on Start() (R4)
- **Issue:** Calling `Start()` twice would launch duplicate goroutines
- **Fix:** Wrap in `sync.Once`

### 3.6 Done() Channel (R5)
- **Issue:** No way to observe when the registration goroutine exits
- **Fix:** Add `Done() <-chan struct{}` for tests and shutdown coordination

### 3.7 Error Body in Messages (R6)
- **Issue:** Registration error messages didn't include response body
- **Fix:** Include truncated body (max 200 chars) for debugging

---

## 4. Aligned Common Patterns

These patterns are consistent across both SPs and should remain so:

| Pattern | Details |
|---------|---------|
| Router | Chi v5 |
| Code generation | oapi-codegen (strict server + types) |
| OpenAPI validation | kin-openapi middleware |
| Ready callback | `WithOnReady()` pattern for post-startup tasks |
| Readiness probe | Poll `/health` at 50ms intervals, 5s timeout |
| Error format | RFC 7807 `application/problem+json` |
| Config loading | `caarlos0/env` |
| Test framework | Ginkgo v2 + Gomega |
| Env var prefix | `SP_` |

---

## 5. Open Design Decisions

These divergences are intentional but warrant project-level discussion:

### 5.1 Endpoint Registration Path
- **K8s SP:** Appends `PostPath()` suffix to the configured endpoint
- **ACM SP:** Uses `SP_PROVIDER_ENDPOINT` as-is (DCM may call other paths like `/health`)
- **Decision needed:** Should `endpoint` include resource path suffix or be base URL only?

### 5.2 Retry Strategy
- **K8s SP:** Infinite retry for all errors (including 4xx)
- **ACM SP:** Infinite retry for retryable errors, immediate failure for 4xx
- **Tradeoffs:** K8s approach is simpler but wastes resources on permanent failures; ACM approach is more efficient but requires correct 4xx classification

### 5.3 Shared SP Common Spec
- Both SPs share significant HTTP/registration/health/error patterns
- **Proposal:** Extract a shared "DCM SP Common Spec" for patterns that should be consistent across all service providers

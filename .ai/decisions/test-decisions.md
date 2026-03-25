# Test Decisions

This document records test design and strategy decisions for the ACM Cluster Service Provider test suite.

**Related Spec:** `.ai/specs/acm-cluster-sp.spec.md`
**Related Test Plans:** `.ai/test-plans/acm-cluster-sp.unit-tests.md`, `.ai/test-plans/acm-cluster-sp.integration-tests.md`

---

## TD-001: Zero-value Resources Rejected by Middleware

**Spec References:** REQ-API-170
**Test Cases:** TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018

### Decision

Zero-value resource strings like `"0GB"` are rejected at the OpenAPI validation middleware level, not in handler code.

### Rationale

The OpenAPI spec defines `pattern: '^[1-9][0-9]*(MB|GB|TB)$'` on memory and storage fields, which rejects `"0GB"` before the request reaches the handler. No handler-level validation needed.

---

## TD-002: Version Matching Strategy

**Spec References:** DD-001, REQ-ACM-030, REQ-ACM-031, REQ-ACM-032
**Test Cases:** TC-KV-UT-004, TC-REG-UT-001, TC-REG-UT-012, TC-REG-UT-013

### Decision

`version="1.30"` must match a K8s minor version in the compatibility matrix exactly; the SP translates to OCP for ClusterImageSet lookup.

### Rationale

Enforces DD-001 at the test level. Tests verify both the matrix lookup and the ClusterImageSet existence check.

---

## TD-003: Partial Create Failure Triggers Rollback

**Spec References:** REQ-ACM-170
**Test Cases:** TC-KV-UT-017, TC-KV-UT-028, TC-BM-UT-008

### Decision

When HostedCluster creation succeeds but NodePool creation fails, the orphaned HostedCluster is deleted before returning the error.

### Rationale

Ensures atomicity — callers never see a half-created cluster. Tests verify both the rollback and the double-failure scenario.

---

## TD-004: Base Domain Shared Across Platforms

**Spec References:** REQ-API-380
**Test Cases:** TC-KV-UT-006, TC-KV-UT-020, TC-BM-UT-010, TC-BM-UT-011

### Decision

`base_domain` is shared across both platforms (HyperShift HostedCluster requires `dns.baseDomain` regardless of platform type). Optional at startup; validated at request time. Request `provider_hints.acm.base_domain` overrides config default.

### Rationale

HyperShift requires `baseDomain` for all platform types. Testing both platforms with and without the config default ensures no platform-specific gaps.

---

## TD-005: Status Change Semantics

**Spec References:** REQ-MON-040
**Test Cases:** TC-MON-UT-009

### Decision

CloudEvents are published only when the DCM-mapped status changes, not on every K8s condition update.

### Rationale

Avoids flooding NATS with redundant events when K8s conditions update without changing the DCM status.

---

## TD-006: NATS Publish Failure Strategy

**Spec References:** REQ-MON-160
**Test Cases:** TC-MON-UT-010

### Decision

Retry with configurable interval (`SP_NATS_PUBLISH_RETRY_INTERVAL=2s`) and max attempts (`SP_NATS_PUBLISH_RETRY_MAX=3`). Log and drop on exhaustion.

### Rationale

Bounded retries prevent blocking the event pipeline. Dropped events are recoverable via periodic resync (REQ-MON-135).

---

## TD-007: Console URI Construction Pattern

**Spec References:** REQ-API-390
**Test Cases:** TC-KV-UT-019, TC-KV-UT-010, TC-KV-UT-027, TC-BM-UT-007

### Decision

`console_uri` is constructed from `SP_CONSOLE_URI_PATTERN` template (default: `https://console-openshift-console.apps.{name}.{base_domain}`) when READY or UNAVAILABLE. Pattern is configurable since it may change across HyperShift versions.

### Rationale

Configurable pattern avoids hardcoding OpenShift console URL format.

---

## TD-008: Health Critical Dependencies

**Spec References:** REQ-HLT-070, REQ-HLT-080
**Test Cases:** TC-HLT-UT-001, TC-HLT-UT-002, TC-HLT-UT-003

### Decision

K8s API connectivity and HyperShift CRD existence are MUST (critical) health checks. Platform-specific checks (KubeVirt, BareMetal) remain SHOULD (non-critical).

### Rationale

K8s API and HyperShift CRD are prerequisites for any cluster operation. Platform checks may be unavailable in mixed environments.

---

## TD-009: Empty Page Token Treated as Absent

**Spec References:** REQ-API-290
**Test Cases:** TC-HDL-LST-UT-008

### Decision

An empty string `page_token=""` returns the first page, not a 400 error.

### Rationale

Defensive: clients may accidentally pass empty strings. Treating as absent provides a better developer experience.

---

## TD-010: Max Page Size Upper Limit Correction

**Spec References:** REQ-API-270
**Test Cases:** TC-HDL-LST-UT-002, TC-HDL-LST-UT-007

### Decision

Upper limit is 100 (per REQ-API-270), not 1000 as originally in the test plan.

### Rationale

Spec says 1-100; original test plan had 1-1000 which contradicted the spec.

---

## TD-011: Middleware-Validated TCs Reclassified to Integration

**Spec References:** REQ-HTTP-090, REQ-API-170, REQ-API-210, IMPL-001
**Test Cases:** TC-HDL-GET-UT-004, TC-HDL-DEL-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018

### Decision

TCs that test OpenAPI spec patterns enforced by validation middleware (e.g., `ClusterIdPath` regex, memory/storage regex) are reclassified from unit to integration scope.

### Rationale

The generated `StrictServerInterface` has no 400 response types for GetCluster/DeleteCluster, confirming these validations cannot be returned by the handler. Tests must go through the full HTTP middleware stack.

---

## TD-012: Metadata Name Validation via OpenAPI

**Spec References:** REQ-API-175
**Test Cases:** (structural verification)

### Decision

`metadata.name` format validation is handled by the OpenAPI spec pattern and validation middleware. No handler code needed.

### Rationale

Already defined in OpenAPI spec, enforced by validation middleware before the handler runs.

---

## TD-013: OpenAPI Middleware Validation Scope

**Spec References:** REQ-HTTP-090
**Test Cases:** TC-HDL-CRT-UT-004, TC-HDL-CRT-UT-006, TC-HDL-CRT-UT-017, TC-HDL-CRT-UT-018

### Decision

The OpenAPI validation middleware (`RequestErrorHandlerFunc`) handles pattern, enum, min/max, and required field validation. Handler-level tests that exercise middleware-overlapping behavior (e.g., `service_type` check) are valid because the handler implementation itself performs those checks — only format/pattern validations are middleware-only.

### Rationale

Clarifies the boundary between middleware and handler validation. Prevents confusion about which layer is responsible for each validation.

---

## TD-014: Utility Functions Tested Transitively

**Spec References:** (multiple)
**Test Cases:** TC-STS-UT-xxx, TC-ERR-UT-xxx

### Decision

Pure utility functions (format conversion, status mapping, error mapping) are tested through consuming service/handler tests, not standalone. TC IDs remain for requirement coverage mapping.

### Rationale

Testing utility functions in isolation adds coupling to internal signatures. Transitive coverage through the calling layer is sufficient and more maintainable.

---
id: IMPL-0013
title: "CNPG Pooler and TCPRoute Helm Templates"
status: Draft
author: Donald Gifford
created: 2026-04-04
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0013: CNPG Pooler and TCPRoute Helm Templates

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-04

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Values Schema and Template Helpers](#phase-1-values-schema-and-template-helpers)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: CNPG Pooler Template](#phase-2-cnpg-pooler-template)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: TCPRoute Template](#phase-3-tcproute-template)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Helm Unit Tests](#phase-4-helm-unit-tests)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: NOTES.txt, Docs, and Final Verification](#phase-5-notestxt-docs-and-final-verification)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Add CNPG Pooler (PgBouncer) and Gateway API TCPRoute templates to the Helm chart,
enabling optional connection pooling and external database access for the CNPG
PostgreSQL cluster.

**Implements:** DESIGN-0005

## Scope

### In Scope

- `cnpg.pooler` values schema nested under existing `cnpg` section
- CNPG `Pooler` CR template (`postgresql.cnpg.io/v1`)
- Gateway API `TCPRoute` template (`gateway.networking.k8s.io/v1alpha2`)
- Template helper for pooler naming
- Helm unit tests for both new templates
- NOTES.txt updates with pooler connection info
- Chart README update via `helm-docs`

### Out of Scope

- Modifying the app's database connection to route through the pooler
- TLS termination configuration at the Gateway level
- Gateway listener configuration (managed separately from this chart)
- Read-only pooler for read replicas
- Kustomize manifests in `deploy/` (Helm chart only)

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Values Schema and Template Helpers

Add the `cnpg.pooler` configuration block to `values.yaml` and the naming helper
to `_helpers.tpl`. This establishes the schema that templates depend on.

#### Tasks

- [x] Add `cnpg.pooler` section to `values.yaml` (after `cnpg.resources`, inside
      the `cnpg:` block):
  - [x] `pooler.enabled: false`
  - [x] `pooler.type: rw` (rw or ro)
  - [x] `pooler.instances: 1`
  - [x] `pooler.pgbouncer.poolMode: transaction`
  - [x] `pooler.pgbouncer.defaultPoolSize: 25`
  - [x] `pooler.pgbouncer.maxClientConnections: 100`
  - [x] `pooler.pgbouncer.parameters: {}` (extra pgbouncer.ini key-value pairs)
  - [x] `pooler.monitoring.enablePodMonitor: false`
  - [x] `pooler.tcpRoute.enabled: false`
  - [x] `pooler.tcpRoute.annotations: {}`
  - [x] `pooler.tcpRoute.parentRefs` with default Gateway reference
- [x] Add `server-price-tracker.cnpgPoolerName` helper to `_helpers.tpl`:
  - Pattern: `{fullname}-db-pooler` (e.g., `spt-server-price-tracker-db-pooler`)
  - Follow the existing `cnpgClusterName` helper pattern
- [x] Verify: `helm lint charts/server-price-tracker/` passes with no errors
- [x] Verify: `helm template test charts/server-price-tracker/` renders without
      errors (pooler disabled by default, no new output expected)

#### Success Criteria

- `values.yaml` has complete `cnpg.pooler` block with all fields documented
- `_helpers.tpl` has `cnpgPoolerName` helper
- `helm lint` passes
- `helm template` with defaults renders cleanly (no pooler resources)

---

### Phase 2: CNPG Pooler Template

Create the Pooler CR template that renders when both `cnpg.enabled` and
`cnpg.pooler.enabled` are true.

#### Tasks

- [x] Create `charts/server-price-tracker/templates/cnpg-pooler.yaml`:
  - [x] Guard: `{{- if and .Values.cnpg.enabled .Values.cnpg.pooler.enabled }}`
  - [x] apiVersion: `postgresql.cnpg.io/v1`
  - [x] kind: `Pooler`
  - [x] metadata.name: use `cnpgPoolerName` helper
  - [x] metadata.labels: use standard `server-price-tracker.labels`
  - [x] spec.cluster.name: use existing `cnpgClusterName` helper
  - [x] spec.type: from `cnpg.pooler.type`
  - [x] spec.instances: from `cnpg.pooler.instances`
  - [x] spec.pgbouncer.poolMode: from `cnpg.pooler.pgbouncer.poolMode`
  - [x] spec.pgbouncer.parameters: render `default_pool_size` and
        `max_client_conn` from values, plus any extra `parameters` map entries
  - [x] spec.monitoring.enablePodMonitor: from
        `cnpg.pooler.monitoring.enablePodMonitor`
- [x] Verify: `helm template test charts/server-price-tracker/ --set cnpg.enabled=true --set cnpg.pooler.enabled=true`
      renders a valid Pooler CR
- [x] Verify: `helm lint` still passes

#### Success Criteria

- `cnpg-pooler.yaml` renders a `Pooler` kind with apiVersion
  `postgresql.cnpg.io/v1`
- Pooler references the correct CNPG cluster name
- Template does not render when either `cnpg.enabled` or `cnpg.pooler.enabled`
  is false
- All PgBouncer parameters are configurable via values
- `helm lint` passes

---

### Phase 3: TCPRoute Template

Create the TCPRoute template that renders when the pooler is enabled and
`cnpg.pooler.tcpRoute.enabled` is true.

#### Tasks

- [x] Create `charts/server-price-tracker/templates/cnpg-pooler-tcproute.yaml`:
  - [x] Add comment: `# TCPRoute is experimental (v1alpha2). Tested with Gateway API v1.4.1.`
  - [x] Guard:
        `{{- if and .Values.cnpg.enabled .Values.cnpg.pooler.enabled .Values.cnpg.pooler.tcpRoute.enabled }}`
  - [x] apiVersion: `gateway.networking.k8s.io/v1alpha2`
  - [x] kind: `TCPRoute`
  - [x] metadata.name: use `cnpgPoolerName` helper
  - [x] metadata.labels: use standard `server-price-tracker.labels`
  - [x] metadata.annotations: from `cnpg.pooler.tcpRoute.annotations` (optional)
  - [x] spec.parentRefs: from `cnpg.pooler.tcpRoute.parentRefs`
  - [x] spec.rules: single rule with backendRef pointing to the pooler service:
    - group: `""`
    - kind: `Service`
    - name: use `cnpgPoolerName` helper (CNPG auto-creates a Service matching
      the Pooler name)
    - port: `5432`
    - weight: `1`
- [x] Verify: `helm template test charts/server-price-tracker/ --set cnpg.enabled=true --set cnpg.pooler.enabled=true --set cnpg.pooler.tcpRoute.enabled=true`
      renders both Pooler and TCPRoute
- [x] Verify: `helm lint` still passes

#### Success Criteria

- `cnpg-pooler-tcproute.yaml` renders a `TCPRoute` kind with apiVersion
  `gateway.networking.k8s.io/v1alpha2`
- backendRef points to the pooler service name on port 5432
- parentRefs come from values
- Annotations rendered when provided
- Template does not render when any of the three enable flags is false
- `helm lint` passes

---

### Phase 4: Helm Unit Tests

Write comprehensive unit tests for both new templates following the existing
test patterns in `tests/cnpg-cluster_test.yaml` and `tests/ingress_test.yaml`.

#### Tasks

- [ ] Create `charts/server-price-tracker/tests/cnpg-pooler_test.yaml` with
      Pooler tests:
  - [ ] Test: does not render when `cnpg.pooler.enabled: false` (default)
  - [ ] Test: does not render when `cnpg.enabled: false` even if
        `cnpg.pooler.enabled: true`
  - [ ] Test: renders Pooler kind with correct apiVersion
        (`postgresql.cnpg.io/v1`)
  - [ ] Test: correct pooler name
        (`RELEASE-NAME-server-price-tracker-db-pooler`)
  - [ ] Test: references CNPG cluster name
        (`RELEASE-NAME-server-price-tracker-db`)
  - [ ] Test: default values (type: rw, instances: 1, poolMode: transaction)
  - [ ] Test: configurable instances count
  - [ ] Test: configurable poolMode, defaultPoolSize, maxClientConnections
  - [ ] Test: extra pgbouncer parameters rendered when provided
  - [ ] Test: monitoring.enablePodMonitor toggle
  - [ ] Test: standard labels present (isSubset check)
- [ ] Add TCPRoute tests to the same `cnpg-pooler_test.yaml` file (follows
      `ingress_test.yaml` pattern of grouping related networking resources):
  - [ ] Test: TCPRoute does not render when `cnpg.pooler.tcpRoute.enabled: false`
  - [ ] Test: TCPRoute does not render when `cnpg.pooler.enabled: false` even if
        `cnpg.pooler.tcpRoute.enabled: true`
  - [ ] Test: TCPRoute does not render when `cnpg.enabled: false`
  - [ ] Test: renders TCPRoute kind with correct apiVersion
        (`gateway.networking.k8s.io/v1alpha2`)
  - [ ] Test: correct parentRefs from values
  - [ ] Test: backendRef points to pooler service on port 5432
  - [ ] Test: annotations rendered when provided
  - [ ] Test: standard labels present
- [ ] Verify: `make helm-unittest` passes with all new tests

#### Success Criteria

- All Pooler tests pass (11 test cases minimum)
- All TCPRoute tests pass (8 test cases minimum)
- `make helm-unittest` passes with 0 failures
- Tests cover both enabled and disabled states for all conditional guards
- Existing tests are not broken by the new values schema

---

### Phase 5: NOTES.txt, Docs, and Final Verification

Update post-install notes, regenerate chart docs, and run full verification.

#### Tasks

- [ ] Update `templates/NOTES.txt`:
  - [ ] Add pooler section when `cnpg.pooler.enabled` is true
  - [ ] Show pooler service name and port-forward command
  - [ ] Show TCPRoute info when `cnpg.pooler.tcpRoute.enabled` is true
  - [ ] Maintain correct numbering with existing conditional sections (CNPG,
        Ollama)
- [ ] Add explicit `cnpg.pooler.enabled: false` to `ci/ci-values.yaml` for
      documentation clarity
- [ ] Run `make helm-docs` to regenerate chart README with new values
- [ ] Run full verification:
  - [ ] `make helm-lint`
  - [ ] `make helm-unittest`
  - [ ] `make helm-template` (default values)
  - [ ] `make helm-template-ci` (CI values)
  - [ ] `helm template test charts/server-price-tracker/ --set cnpg.enabled=true --set cnpg.pooler.enabled=true --set cnpg.pooler.tcpRoute.enabled=true`
  - [ ] `make lint-yaml-charts`
- [ ] Update DESIGN-0005 status from `Draft` to `Implemented`

#### Success Criteria

- NOTES.txt shows pooler connection info when enabled
- NOTES.txt renders cleanly with all combinations: pooler only, pooler + tcproute,
  pooler + cnpg + ollama
- `make helm-docs` succeeds (chart README reflects new values)
- `make helm-lint` passes
- `make helm-unittest` passes with 0 failures
- `make helm-template` and `make helm-template-ci` render without errors
- `make lint-yaml-charts` passes
- DESIGN-0005 status is `Implemented`

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `charts/server-price-tracker/values.yaml` | Modify | Add `cnpg.pooler` section |
| `charts/server-price-tracker/templates/_helpers.tpl` | Modify | Add `cnpgPoolerName` helper |
| `charts/server-price-tracker/templates/cnpg-pooler.yaml` | Create | CNPG Pooler CR template |
| `charts/server-price-tracker/templates/cnpg-pooler-tcproute.yaml` | Create | Gateway API TCPRoute template |
| `charts/server-price-tracker/templates/NOTES.txt` | Modify | Add pooler connection info |
| `charts/server-price-tracker/tests/cnpg-pooler_test.yaml` | Create | Pooler and TCPRoute unit tests |
| `charts/server-price-tracker/README.md` | Modify | Regenerated by `helm-docs` |
| `docs/design/0005-cnpg-pooler-with-gateway-tcproute.md` | Modify | Status → Implemented |

## Testing Plan

- [ ] Helm unit tests for Pooler template (11+ test cases)
- [ ] Helm unit tests for TCPRoute template (8+ test cases)
- [ ] `helm lint` passes with new templates
- [ ] `helm template` renders correctly for all enable/disable combinations:
  - Default (both disabled)
  - Pooler only (`cnpg.enabled=true`, `cnpg.pooler.enabled=true`)
  - Pooler + TCPRoute (all three enabled)
  - Pooler without CNPG (`cnpg.enabled=false`, `cnpg.pooler.enabled=true`) — should render nothing
- [ ] CI values (`ci/ci-values.yaml`) still work (cnpg disabled, no pooler rendered)
- [ ] yamllint passes on new template files

## Dependencies

- CNPG operator must be installed in the target cluster for the Pooler CR to be
  reconciled (not required for Helm template rendering or testing)
- Gateway API CRDs (`gateway.networking.k8s.io/v1alpha2`) must be installed for
  TCPRoute to be accepted by the API server
- Cilium must be configured as the Gateway controller with TCPRoute support enabled
- A Gateway resource with a TCP listener must exist (managed separately)

## Open Questions

All resolved:

- **TCPRoute API stability:** Add a comment in the template noting that
  `gateway.networking.k8s.io/v1alpha2` is experimental and tested with Gateway API
  v1.4.1 only. This is the first experimental Gateway API resource in the chart.
- **Pooler test file organization:** Group Pooler and TCPRoute tests in the same
  file (`cnpg-pooler_test.yaml`), consistent with how `ingress_test.yaml` groups
  Ingress and HTTPRoute tests together.
- **ci-values.yaml:** Add explicit `cnpg.pooler.enabled: false` for documentation
  clarity.

## References

- [DESIGN-0005: CNPG Pooler with Gateway TCPRoute](../design/0005-cnpg-pooler-with-gateway-tcproute.md)
- [IMPL-0003: Helm Chart Docs and Unit Tests](0003-helm-chart-docs-and-unit-tests.md) (existing test patterns)
- [CloudNativePG Pooler Documentation](https://cloudnative-pg.io/documentation/current/connection_pooling/)
- [CNPG Pooler API Reference](https://cloudnative-pg.io/documentation/current/cloudnative-pg.v1/#postgresql-cnpg-io-v1-Pooler)
- [Gateway API TCPRoute Spec](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.TCPRoute)

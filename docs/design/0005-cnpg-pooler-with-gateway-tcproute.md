---
id: DESIGN-0005
title: "CNPG Pooler with Gateway TCPRoute"
status: Implemented
author: Donald Gifford
created: 2026-04-04
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0005: CNPG Pooler with Gateway TCPRoute

**Status:** Implemented
**Author:** Donald Gifford
**Date:** 2026-04-04

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [CNPG Pooler CRD](#cnpg-pooler-crd)
  - [Gateway API TCPRoute](#gateway-api-tcproute)
  - [Helm Chart Integration](#helm-chart-integration)
- [API / Interface Changes](#api--interface-changes)
  - [values.yaml Configuration](#valuesyaml-configuration)
  - [Template Helpers](#template-helpers)
  - [New Templates](#new-templates)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add an optional CNPG Pooler (PgBouncer) to the Helm chart, nested under the existing `cnpg`
configuration, with a companion Gateway API TCPRoute for exposing the pooled PostgreSQL
connection outside the Kubernetes cluster. Both are disabled by default.

## Goals and Non-Goals

### Goals

- Deploy a CNPG-managed PgBouncer connection pooler in front of the PostgreSQL cluster
- Expose the pooler externally via Gateway API TCPRoute for ad-hoc queries, data tools, and
  cross-cluster services
- Follow existing Helm chart patterns (conditional rendering, standard labels, helper functions)
- Provide comprehensive helm-unittest coverage for new templates

### Non-Goals

- Modifying the application's database connection to route through the pooler (the app connects
  directly to the CNPG `-rw` service today; routing through the pooler is a separate decision)
- TLS termination at the Gateway level (can be added later via Gateway listener config)
- Read-only pooler for read replicas (single-instance cluster today; `type: ro` is supported
  in the schema for future use)

## Background

The CNPG PostgreSQL cluster (`DESIGN-0003`) runs inside the Kubernetes cluster with no
connection pooling and no external access path. Connection pooling via PgBouncer reduces
connection overhead and protects PostgreSQL from connection exhaustion. External access is
needed for:

- Ad-hoc SQL queries from developer machines
- External data tools (DBeaver, pgAdmin, Metabase)
- Other services outside the cluster that need database access

CloudNativePG provides a native `Pooler` CRD that the operator manages — it deploys PgBouncer
pods, creates a Service, and handles credential rotation automatically. The Pooler references
the CNPG Cluster by name, inheriting its authentication configuration.

PostgreSQL uses a binary TCP protocol, not HTTP. Gateway API provides `TCPRoute` (not
`HTTPRoute`) for L4 TCP forwarding. The Cilium Gateway controller supports `TCPRoute` via the
`gateway.networking.k8s.io/v1alpha2` API group.

## Detailed Design

### CNPG Pooler CRD

The Pooler CR is a lightweight resource that tells the CNPG operator to deploy PgBouncer:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Pooler
metadata:
  name: {fullname}-db-pooler
spec:
  cluster:
    name: {fullname}-db          # references existing CNPG Cluster
  type: rw                       # rw (read-write) or ro (read-only)
  instances: 1
  pgbouncer:
    poolMode: transaction
    parameters:
      default_pool_size: "25"
      max_client_conn: "100"
  monitoring:
    enablePodMonitor: false
```

The CNPG operator automatically:

- Creates a Deployment with PgBouncer pods
- Creates a Service named after the Pooler resource (`{fullname}-db-pooler`)
- Configures PgBouncer to authenticate against the CNPG cluster's credentials
- Handles credential rotation when the cluster secrets change

### Gateway API TCPRoute

The TCPRoute forwards TCP traffic from a Gateway listener to the Pooler Service:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: {fullname}-db-pooler
spec:
  parentRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: internal
      namespace: gateway
      sectionName: postgres      # optional: specific listener
  rules:
    - backendRefs:
        - group: ""
          kind: Service
          name: {fullname}-db-pooler
          port: 5432
          weight: 1
```

The Gateway must have a listener configured for TCP on the desired port. This is outside the
scope of this chart (the Gateway is managed separately).

### Helm Chart Integration

The pooler configuration nests under the existing `cnpg` section, following the same pattern
as `cnpg.monitoring`, `cnpg.managed`, etc. The guard condition requires both `cnpg.enabled`
and `cnpg.pooler.enabled` — you cannot deploy a pooler without a cluster.

Resource naming follows the established convention:

| Resource | Name Pattern | Example |
|----------|-------------|---------|
| CNPG Cluster | `{fullname}-db` | `spt-server-price-tracker-db` |
| CNPG Secret | `{fullname}-db-app` | `spt-server-price-tracker-db-app` |
| **Pooler** | `{fullname}-db-pooler` | `spt-server-price-tracker-db-pooler` |
| **Pooler Service** | `{fullname}-db-pooler` | (auto-created by CNPG operator) |
| **TCPRoute** | `{fullname}-db-pooler` | `spt-server-price-tracker-db-pooler` |

## API / Interface Changes

### values.yaml Configuration

Added under `cnpg:` (after `cnpg.resources`):

```yaml
cnpg:
  # ... existing fields ...
  pooler:
    enabled: false
    type: rw                        # rw or ro
    instances: 1
    pgbouncer:
      poolMode: transaction          # session, transaction, statement
      defaultPoolSize: 25
      maxClientConnections: 100
      parameters: {}                # extra pgbouncer.ini key-value pairs
    monitoring:
      enablePodMonitor: false
    tcpRoute:
      enabled: false
      annotations: {}
      parentRefs:
        - group: gateway.networking.k8s.io
          kind: Gateway
          name: internal
          namespace: gateway
        # sectionName: postgres     # optional: Gateway listener section
```

### Template Helpers

New helper in `_helpers.tpl`:

```
server-price-tracker.cnpgPoolerName  →  {fullname}-db-pooler
```

### New Templates

| Template | Kind | Condition |
|----------|------|-----------|
| `cnpg-pooler.yaml` | `Pooler` (postgresql.cnpg.io/v1) | `cnpg.enabled && cnpg.pooler.enabled` |
| `cnpg-pooler-tcproute.yaml` | `TCPRoute` (gateway.networking.k8s.io/v1alpha2) | `cnpg.enabled && cnpg.pooler.enabled && cnpg.pooler.tcpRoute.enabled` |

NOTES.txt updated with pooler connection instructions when enabled.

## Data Model

No database schema changes. The Pooler is a Kubernetes-level resource only.

## Testing Strategy

New helm-unittest file `tests/cnpg-pooler_test.yaml`:

**Pooler tests:**

- Does not render when `cnpg.pooler.enabled: false`
- Does not render when `cnpg.enabled: false` (even if `cnpg.pooler.enabled: true`)
- Renders Pooler kind with correct apiVersion (`postgresql.cnpg.io/v1`)
- Correct pooler name (`{release}-server-price-tracker-db-pooler`)
- References CNPG cluster name correctly
- Configurable: instances, poolMode, defaultPoolSize, maxClientConnections
- Extra pgbouncer parameters rendered when provided
- PodMonitor toggle

**TCPRoute tests:**

- Does not render when `cnpg.pooler.tcpRoute.enabled: false`
- Does not render when `cnpg.pooler.enabled: false` (even if tcpRoute is true)
- Renders TCPRoute kind with correct apiVersion (`gateway.networking.k8s.io/v1alpha2`)
- Correct parentRefs from values
- backendRef points to pooler service on port 5432
- Annotations applied when set

**Verification commands:**

```bash
make helm-unittest    # unit tests
make helm-lint        # lint
make helm-test        # full test suite (lint + unit)
```

## Migration / Rollout Plan

1. Merge chart changes — no impact since both `cnpg.pooler.enabled` and
   `cnpg.pooler.tcpRoute.enabled` default to `false`
2. Enable the pooler in the dev overlay first:
   ```yaml
   cnpg:
     pooler:
       enabled: true
   ```
3. Verify the CNPG operator creates the Pooler Deployment and Service
4. Test connectivity via port-forward: `kubectl port-forward svc/{pooler-name} 5432:5432`
5. Configure a Gateway listener for TCP on the desired port (separate from this chart)
6. Enable the TCPRoute:
   ```yaml
   cnpg:
     pooler:
       tcpRoute:
         enabled: true
         parentRefs:
           - group: gateway.networking.k8s.io
             kind: Gateway
             name: internal
             namespace: gateway
   ```
7. Test external connectivity via the Gateway's external IP/hostname

## Open Questions

All resolved:

- **Pool mode:** `transaction` — more efficient for the web-style workload; `session` available
  as override via values.
- **TCPRoute, not HTTPRoute:** PostgreSQL uses a binary TCP protocol. HTTPRoute is L7 HTTP
  only and would reject the PG handshake as malformed. TCPRoute does L4 raw TCP forwarding.
  `sectionName` kept as an optional commented field for targeting a specific Gateway listener.
- **Port:** Hardcoded to 5432 (PostgreSQL standard). No need to make configurable.

## References

- [CloudNativePG Pooler Documentation](https://cloudnative-pg.io/documentation/current/connection_pooling/)
- [CNPG Pooler API Reference](https://cloudnative-pg.io/documentation/current/cloudnative-pg.v1/#postgresql-cnpg-io-v1-Pooler)
- [Gateway API TCPRoute](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.TCPRoute)
- [Cilium Gateway API Support](https://docs.cilium.io/en/latest/network/servicemesh/gateway-api/gateway-api/)
- DESIGN-0003: Helm Chart Testing and Releasing CI/CD

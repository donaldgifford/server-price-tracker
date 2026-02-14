# server-price-tracker

![Version: 0.1.2](https://img.shields.io/badge/Version-0.1.2-informational?style=flat-square)
![AppVersion: 0.1.2](https://img.shields.io/badge/AppVersion-0.1.2-informational?style=flat-square)
![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

eBay server hardware deal tracker with LLM extraction, scoring, and Discord
alerts.

## Prerequisites

- Kubernetes 1.27+
- Helm 3.x
- **Optional:** [CloudNativePG operator](https://cloudnative-pg.io/) -- required
  when `cnpg.enabled=true`
- **Optional:** GPU node with NVIDIA device plugin -- required when
  `ollama.enabled=true`
- **Optional:** [Prometheus Operator](https://prometheus-operator.dev/) --
  required when `serviceMonitor.enabled=true`

## Usage

### Add the Helm repo

```bash
helm repo add spt https://donaldgifford.github.io/server-price-tracker/
helm repo update
```

### Install the chart

```bash
helm install spt spt/server-price-tracker -f values.yaml
```

### Dependencies

The chart can optionally deploy or integrate with:

| Dependency | Enabled by | Notes |
|------------|------------|-------|
| CNPG PostgreSQL cluster | `cnpg.enabled=true` | Requires the CNPG operator to be installed first |
| Ollama LLM backend | `ollama.enabled=true` | Deploys a StatefulSet with GPU support |
| Prometheus ServiceMonitor | `serviceMonitor.enabled=true` | Requires the Prometheus Operator CRDs |

### Uninstall

```bash
helm uninstall spt
```

### Upgrade

```bash
helm upgrade spt spt/server-price-tracker -f values.yaml
```

## Configuration

Key `values.yaml` parameters grouped by section. See
[values.yaml](values.yaml) for the full reference.

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image repository | `ghcr.io/donaldgifford/server-price-tracker` |
| `image.tag` | Image tag (defaults to chart appVersion) | `""` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |

### Application Config

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.server.port` | API server listen port | `8080` |
| `config.llm.backend` | LLM backend (`ollama`, `anthropic`, `openai_compat`) | `ollama` |
| `config.llm.ollama.model` | Ollama model name | `mistral:7b-instruct-v0.3-q5_K_M` |
| `config.scoring.weights.*` | Composite score weights (must sum to 1.0) | see values.yaml |
| `config.schedule.ingestion_interval` | eBay polling interval | `30m` |
| `config.logging.level` | Log level (`debug`, `info`, `warn`, `error`) | `info` |

### Secrets

| Parameter | Description | Default |
|-----------|-------------|---------|
| `secret.create` | Create a Secret from `secret.values` | `true` |
| `secret.existingSecret` | Use an existing Secret instead | `""` |
| `secret.values.*` | Secret key-value pairs (DB creds, API keys, etc.) | see values.yaml |

### Migration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `migration.enabled` | Run database migration as init container | `true` |
| `migration.resources` | Resource requests/limits for migration container | see values.yaml |

### Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `livenessProbe` | Liveness probe config (set to `null` to disable) | httpGet `/healthz` |
| `readinessProbe` | Readiness probe config (set to `null` to disable) | httpGet `/readyz` |

### Ingress / HTTPRoute

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Kubernetes Ingress | `false` |
| `httpRoute.enabled` | Enable Gateway API HTTPRoute | `false` |
| `httpRoute.parentRefs` | Gateway parent references | see values.yaml |

### CNPG (CloudNativePG)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cnpg.enabled` | Deploy a CNPG PostgreSQL Cluster | `false` |
| `cnpg.instances` | Number of PostgreSQL instances | `1` |
| `cnpg.imageName` | PostgreSQL container image | `ghcr.io/cloudnative-pg/postgresql:17.2` |
| `cnpg.storage.size` | PVC storage size | `10Gi` |
| `cnpg.storage.storageClass` | Storage class (empty = default) | `""` |

### Ollama

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ollama.enabled` | Deploy an Ollama StatefulSet | `false` |
| `ollama.model` | Model to pull on startup | `mistral:7b-instruct-v0.3-q5_K_M` |
| `ollama.gpu.enabled` | Request GPU resources | `true` |
| `ollama.persistence.size` | PVC storage size for model data | `30Gi` |

### ServiceMonitor

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceMonitor.enabled` | Create a Prometheus ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `serviceMonitor.path` | Metrics endpoint path | `/metrics` |

### Autoscaling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `autoscaling.enabled` | Enable HPA | `false` |
| `autoscaling.minReplicas` | Minimum replicas | `1` |
| `autoscaling.maxReplicas` | Maximum replicas | `100` |

### Tests

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tests.connection.enabled` | Enable Helm test hook for connectivity | `true` |

## Workarounds & Known Issues

- **CNPG operator must be pre-installed** -- The CNPG CRDs must exist in the
  cluster before setting `cnpg.enabled=true`. Install the operator first:
  `helm install cnpg cloudnative-pg/cloudnative-pg`.
- **Disable connection test with stub images** -- When using CI stub images
  (e.g., `nginx:alpine`), set `tests.connection.enabled=false` since the stub
  won't expose `/healthz`.
- **chart-releaser + checkout@v6** -- The `chart-releaser-action` requires
  `persist-credentials: true` on `actions/checkout@v6` (see
  [helm/chart-releaser-action#231](https://github.com/helm/chart-releaser-action/issues/231)).

## Further Information

- [Project README](../../README.md)
- [Design Document](../../docs/DESIGN.md)
- [Deployment Strategy](../../docs/DEPLOYMENT_STRATEGY.md)
- [GitHub Repository](https://github.com/donaldgifford/server-price-tracker)

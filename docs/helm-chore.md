# Plan: Helm Chart Docs, Unit Tests & CI Linting

## Context

The Helm chart is deployed and the release pipeline is working (v0.1.2 verified). Now we need to improve chart quality and developer experience:
1. Add a `README.md` for the chart with prereqs, usage, configuration, and known issues
2. Add `helm-unittest` tests for template logic and conditional resources
3. Add broader linting to CI (helm lint, yamllint, yamlfmt, markdownlint-cli2) for both charts and the repo in general

The user is separately handling docs/ restructuring, CODEOWNERS, renovate, and PR/issue templates.

---

## Phase 1: Chart README

### File: `charts/server-price-tracker/README.md`

Sections:
1. **Header** -- Name, short description, badges (chart version, app version)
2. **Prerequisites** -- Kubernetes 1.27+, Helm 3.x, optional: CNPG operator, Ollama, Prometheus Operator
3. **Usage**
   - **Add the Helm repo**: `helm repo add spt https://donaldgifford.github.io/server-price-tracker/`
   - **Install**: `helm install spt spt/server-price-tracker -f values.yaml`
   - **Dependencies**: CNPG operator (if `cnpg.enabled`), Ollama (if `ollama.enabled`), Prometheus Operator (if `serviceMonitor.enabled`)
   - **Uninstall**: `helm uninstall spt`
   - **Upgrade**: `helm upgrade spt spt/server-price-tracker -f values.yaml`
4. **Configuration** -- Table of key values.yaml parameters grouped by section (image, config, secret, migration, probes, ingress/httproute, cnpg, ollama, serviceMonitor, autoscaling, tests). Not an exhaustive dump -- link to `values.yaml` for the full reference.
5. **Workarounds & Known Issues**
   - CNPG operator must be installed before enabling `cnpg.enabled`
   - `tests.connection.enabled` should be `false` when using stub images
   - `chart-releaser-action` requires `persist-credentials: true` with `actions/checkout@v6`
6. **Further Information** -- Links to project README, docs/DESIGN.md, docs/DEPLOYMENT_STRATEGY.md, GitHub repo

### File: `charts/server-price-tracker/.helmignore` -- Add entries

Add `tests/`, `README.md`, and `ci/` to `.helmignore` so unit tests and docs aren't packaged in the chart tarball.

---

## Phase 2: Helm Unit Tests (helm-unittest)

### Tool: `helm-unittest/helm-unittest` (Helm plugin)

helm-unittest is a Helm plugin, not a standalone binary. Installation is via `helm plugin install`. For CI, we'll use the `d3adb5/helm-unittest-action@v2` GitHub Action. Locally, devs run `helm plugin install https://github.com/helm-unittest/helm-unittest.git`.

### Test directory: `charts/server-price-tracker/tests/`

Convention: one test file per template, named `<template>_test.yaml`.

### Test files to create (8):

#### 1. `deployment_test.yaml`

- Renders a Deployment with correct name, labels, selector labels
- Default image uses `.Chart.AppVersion` when `image.tag` is empty
- Custom `image.tag` overrides appVersion
- Container args are `["serve", "--config", "/etc/spt/config.yaml"]`
- Container port matches `config.server.port`
- Config volume mounted at `/etc/spt` (readOnly)
- envFrom references the secret
- Liveness and readiness probes render by default, can be nulled out
- Migration init container renders when `migration.enabled=true`, absent when `false`
- CNPG env vars render when `cnpg.enabled=true`
- Replicas not set when `autoscaling.enabled=true`
- `checksum/config` annotation present (config reload on change)

#### 2. `service_test.yaml`

- Renders a Service of type ClusterIP on port 8080
- Selector labels match deployment

#### 3. `configmap_test.yaml`

- Renders a ConfigMap with config data
- Config includes server, database, ebay, llm sections

#### 4. `secret_test.yaml`

- Renders when `secret.create=true`
- Does not render when `secret.create=false`
- Uses `existingSecret` name when `secret.create=false`

#### 5. `cnpg-cluster_test.yaml`

- Does not render when `cnpg.enabled=false` (default)
- Renders CNPG Cluster when `cnpg.enabled=true`
- Correct instances, imageName, storage size
- Bootstrap database and owner set correctly
- Storage class rendered when specified

#### 6. `ollama_test.yaml`

- StatefulSet and Service do not render when `ollama.enabled=false` (default)
- StatefulSet renders with correct model, resources when enabled
- PVC storage size and class correct

#### 7. `servicemonitor_test.yaml`

- Does not render when `serviceMonitor.enabled=false` (default)
- Renders ServiceMonitor with correct path, interval when enabled

#### 8. `ingress_test.yaml`

- Does not render when `ingress.enabled=false` (default)
- Renders Ingress with hosts/paths when enabled
- HTTPRoute renders when `httpRoute.enabled=true`

---

## Phase 3: CI Linting Jobs

### Approach

Add two new jobs to `.github/workflows/ci.yml`. These are separate from the existing `lint` job (Go-specific golangci-lint) and `helm-test` job (chart-testing with kind).

### New job: `lint-repo`

Runs in parallel (no `needs`). Uses `jdx/mise-action@v2` -- a GitHub Action that reads `mise.toml` and installs all tools in one step. Versions stay in sync with local dev (single source of truth).

Steps:

1. `actions/checkout@v6`
2. `jdx/mise-action@v2` -- installs yamllint, markdownlint-cli2, yamlfmt, actionlint from `mise.toml`
3. `azure/setup-helm@v4` -- for helm lint
4. **yamllint**: `yamllint -c .yamllint.yml . -e charts/` (root config, excludes charts/)
5. **yamllint charts**: `yamllint -c charts/.yamllint.yml charts/` (chart-specific config)
6. **yamlfmt check**: `yamlfmt -lint .` (checks formatting without modifying)
7. **markdownlint-cli2**: `markdownlint-cli2 '**/*.md' '#node_modules'`
8. **actionlint**: `actionlint`
9. **helm lint**: `helm lint charts/server-price-tracker/`

### New job: `helm-unittest`

Runs in parallel (no `needs`). Uses `d3adb5/helm-unittest-action@v2`.

Steps:

1. `actions/checkout@v6`
2. `d3adb5/helm-unittest-action@v2` with `charts: charts/server-price-tracker`, `flags: --color`

### Makefile targets (DONE)

Helm and linting Make targets are already set up across two domain makefiles:

**`scripts/makefiles/helm.mk`** -- Helm-specific targets:

- `helm-lint`, `helm-template`, `helm-template-ci`, `helm-package`
- `helm-unittest`, `helm-test` (lint + unittest)
- `helm-ct-lint`, `helm-ct-list-changed`, `helm-ct-install`
- `helm-docs`, `helm-diff-check`, `helm-cr-package`

**`scripts/makefiles/docs.mk`** -- Repo-wide linting targets:

- `lint-yaml`, `lint-yaml-charts`, `lint-yaml-fmt`
- `lint-md`, `lint-actions`, `lint-all`

Both are included in the root `Makefile`.

---

## Files Summary

### Create (9)

| File | Purpose |
|------|---------|
| `charts/server-price-tracker/README.md` | Chart documentation |
| `charts/server-price-tracker/tests/deployment_test.yaml` | Deployment template tests |
| `charts/server-price-tracker/tests/service_test.yaml` | Service template tests |
| `charts/server-price-tracker/tests/configmap_test.yaml` | ConfigMap template tests |
| `charts/server-price-tracker/tests/secret_test.yaml` | Secret template tests |
| `charts/server-price-tracker/tests/cnpg-cluster_test.yaml` | CNPG Cluster template tests |
| `charts/server-price-tracker/tests/ollama_test.yaml` | Ollama StatefulSet/Service tests |
| `charts/server-price-tracker/tests/servicemonitor_test.yaml` | ServiceMonitor template tests |
| `charts/server-price-tracker/tests/ingress_test.yaml` | Ingress + HTTPRoute tests |

### Modify (2)

| File | Change |
|------|--------|
| `charts/server-price-tracker/.helmignore` | Add `tests/`, `README.md`, `ci/` |
| `.github/workflows/ci.yml` | Add `lint-repo` and `helm-unittest` jobs |

### Already Done

| File | Status |
|------|--------|
| `scripts/makefiles/helm.mk` | Created -- Helm development, testing, ct, and tools targets |
| `scripts/makefiles/docs.mk` | Created -- Repo-wide linting targets |
| `Makefile` | Updated -- includes `helm.mk` and `docs.mk` |
| `mise.toml` | Updated -- added helm, helm-cr, helm-ct, helm-diff, helm-docs |
| `CLAUDE.md` | Updated -- all new tools and make targets documented |

---

## Verification

```bash
# Chart README exists and renders
cat charts/server-price-tracker/README.md

# Helm unit tests pass
make helm-unittest

# Helm lint passes
make helm-lint

# Yamllint passes (both configs)
make lint-yaml
make lint-yaml-charts

# YAML formatting check
make lint-yaml-fmt

# Markdownlint passes
make lint-md

# Actionlint passes
make lint-actions

# All linters at once
make lint-all

# CI workflow is valid
actionlint .github/workflows/ci.yml

# .helmignore excludes tests from packaging
make helm-package && tar -tzf server-price-tracker-*.tgz | grep -c test  # 0
```

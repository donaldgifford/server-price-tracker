# Helm Chart Docs, Unit Tests & CI Linting -- Implementation Guide

Phased implementation of chart documentation, helm-unittest integration, and
repo-wide CI linting as described in [helm-chore.md](helm-chore.md).

---

## Phase 1: Chart README & Helmignore

Add chart documentation and ensure tests/docs are excluded from packaged chart
artifacts.

### Tasks

- [x] Create `charts/server-price-tracker/README.md` with the following
      sections:
  - [x] Header with chart name, description, badges (version, appVersion)
  - [x] Prerequisites section (Kubernetes 1.27+, Helm 3.x, optional operators)
  - [x] Usage section:
    - [x] Adding the Helm repo (`helm repo add spt ...`)
    - [x] Installing the chart (`helm install ...`)
    - [x] Dependencies (CNPG operator, Ollama, Prometheus Operator -- when each
          is needed)
    - [x] Uninstalling (`helm uninstall ...`)
    - [x] Upgrading (`helm upgrade ...`)
  - [x] Configuration table -- key `values.yaml` parameters grouped by section
        (image, config, secret, migration, probes, ingress/httproute, cnpg,
        ollama, serviceMonitor, autoscaling, tests)
  - [x] Workarounds & Known Issues section
  - [x] Further Information section with links to project docs
- [x] Update `charts/server-price-tracker/.helmignore` -- add `tests/`,
      `README.md`, `ci/`
- [x] Verify: `helm package charts/server-price-tracker/` produces a `.tgz` that
      does NOT contain `tests/`, `README.md`, or `ci/`
- [x] Verify: `helm lint charts/server-price-tracker/` still passes

### Success Criteria

- `charts/server-price-tracker/README.md` exists with all required sections
- `.helmignore` includes `tests/`, `README.md`, `ci/`
- `helm package` output excludes test files, README, and CI values
- `helm lint` passes with no errors

---

## Phase 2: Helm Unit Tests

Add `helm-unittest` tests for all chart templates to validate template logic,
conditional rendering, and value overrides.

### Tasks

- [x] Create `charts/server-price-tracker/tests/` directory
- [x] Create `charts/server-price-tracker/tests/deployment_test.yaml`:
  - [x] Test: renders a Deployment with correct kind, name, labels
  - [x] Test: default image uses `.Chart.AppVersion` when `image.tag` is empty
  - [x] Test: custom `image.tag` overrides appVersion
  - [x] Test: container args are `["serve", "--config", "/etc/spt/config.yaml"]`
  - [x] Test: container port matches `config.server.port` (8080)
  - [x] Test: config volume mounted at `/etc/spt` (readOnly)
  - [x] Test: envFrom references the secret
  - [x] Test: liveness and readiness probes render by default
  - [x] Test: probes can be nulled out (`livenessProbe: null`)
  - [x] Test: migration init container renders when `migration.enabled=true`
  - [x] Test: migration init container absent when `migration.enabled=false`
  - [x] Test: CNPG env vars (DB_HOST, DB_USER, DB_PASSWORD, DB_NAME) render when
        `cnpg.enabled=true`
  - [x] Test: replicas field absent when `autoscaling.enabled=true`
  - [x] Test: `checksum/config` annotation present
- [x] Create `charts/server-price-tracker/tests/service_test.yaml`:
  - [x] Test: renders a Service of type ClusterIP
  - [x] Test: service port is 8080
  - [x] Test: selector labels match deployment
- [x] Create `charts/server-price-tracker/tests/configmap_test.yaml`:
  - [x] Test: renders a ConfigMap
  - [x] Test: config data includes server, database, ebay, llm sections
- [x] Create `charts/server-price-tracker/tests/secret_test.yaml`:
  - [x] Test: renders Secret when `secret.create=true`
  - [x] Test: does not render when `secret.create=false`
  - [x] Test: secret type is Opaque
- [x] Create `charts/server-price-tracker/tests/cnpg-cluster_test.yaml`:
  - [x] Test: does not render when `cnpg.enabled=false` (default)
  - [x] Test: renders CNPG Cluster when `cnpg.enabled=true`
  - [x] Test: correct apiVersion (`postgresql.cnpg.io/v1`), kind (`Cluster`)
  - [x] Test: instances, imageName, storage size match values
  - [x] Test: bootstrap database and owner set correctly
  - [x] Test: storageClass rendered when specified
- [x] Create `charts/server-price-tracker/tests/ollama_test.yaml`:
  - [x] Test: StatefulSet does not render when `ollama.enabled=false` (default)
  - [x] Test: Service does not render when `ollama.enabled=false`
  - [x] Test: StatefulSet renders with correct model, resources when enabled
  - [x] Test: PVC storage size and storageClass correct
- [x] Create `charts/server-price-tracker/tests/servicemonitor_test.yaml`:
  - [x] Test: does not render when `serviceMonitor.enabled=false` (default)
  - [x] Test: renders ServiceMonitor when enabled
  - [x] Test: correct path (`/metrics`) and interval (`30s`)
- [x] Create `charts/server-price-tracker/tests/ingress_test.yaml`:
  - [x] Test: Ingress does not render when `ingress.enabled=false` (default)
  - [x] Test: Ingress renders with hosts/paths when enabled
  - [x] Test: HTTPRoute does not render when `httpRoute.enabled=false` (default)
  - [x] Test: HTTPRoute renders when `httpRoute.enabled=true`
- [x] Install helm-unittest plugin locally:
      `helm plugin install https://github.com/helm-unittest/helm-unittest.git`
- [x] Verify: `make helm-unittest` -- all tests pass (49/49)
- [x] Verify: `make helm-lint` still passes (tests don't break linting)

### Success Criteria

- 8 test files exist in `charts/server-price-tracker/tests/`
- `make helm-unittest` passes with 0 failures
- Every conditional template (cnpg, ollama, serviceMonitor, ingress, httproute,
  secret, migration) has both enabled and disabled test cases
- Deployment test covers: image tag override, probes, init container, CNPG env
  vars, autoscaling replicas
- `make helm-lint` still passes

---

## Phase 3: Makefile Targets (DONE)

Helm and linting Make targets have been set up across two domain makefiles.

### Tasks

- [x] Create `scripts/makefiles/helm.mk` with targets:
  - [x] `helm-lint` -- `helm lint charts/server-price-tracker/`
  - [x] `helm-unittest` -- `helm unittest charts/server-price-tracker/`
  - [x] `helm-template` -- `helm template test charts/server-price-tracker/`
  - [x] `helm-template-ci` -- render with CI values
  - [x] `helm-package` -- package chart into .tgz
  - [x] `helm-test` -- lint + unittest combined
  - [x] `helm-ct-lint`, `helm-ct-list-changed`, `helm-ct-install` -- chart-testing targets
  - [x] `helm-docs`, `helm-diff-check`, `helm-cr-package` -- helm tools targets
- [x] Create `scripts/makefiles/docs.mk` with repo-wide linting targets:
  - [x] `lint-yaml` -- yamllint repo YAML (excludes charts/)
  - [x] `lint-yaml-charts` -- yamllint chart YAML (relaxed rules)
  - [x] `lint-yaml-fmt` -- yamlfmt formatting check
  - [x] `lint-md` -- markdownlint-cli2
  - [x] `lint-actions` -- actionlint
  - [x] `lint-all` -- all linters combined (Go + YAML + Markdown + Actions + Helm)
- [x] Update root `Makefile` -- include `helm.mk` and `docs.mk`
- [x] Add Helm tools to `mise.toml` -- helm, helm-cr, helm-ct, helm-diff, helm-docs
- [x] Install helm-unittest Helm plugin locally
- [x] Update `CLAUDE.md` with all new tools and make targets

### Success Criteria

- [x] `scripts/makefiles/helm.mk` exists with Helm targets
- [x] `scripts/makefiles/docs.mk` exists with repo linting targets
- [x] Root `Makefile` includes both `helm.mk` and `docs.mk`
- [x] All Make targets execute successfully
- [x] `make help` shows the new targets

---

## Phase 4: CI Workflow -- `lint-repo` Job

Add repo-wide linting to the CI pipeline using `jdx/mise-action@v2` to install
tools from `mise.toml`.

### Tasks

- [x] Add `lint-repo` job to `.github/workflows/ci.yml` (no `needs` -- runs in
      parallel)
- [x] Job steps:
  - [x] `actions/checkout@v6`
  - [x] `jdx/mise-action@v2` -- installs yamllint, markdownlint-cli2, yamlfmt,
        actionlint
  - [x] `azure/setup-helm@v4` -- for helm lint
  - [x] `make lint-yaml` -- lint repo YAML (excludes charts/)
  - [x] `make lint-yaml-charts` -- lint chart YAML (relaxed rules)
  - [x] `make lint-yaml-fmt` -- check YAML formatting (no modify)
  - [x] `make lint-md` -- lint all markdown
  - [x] `make lint-actions` -- validate all GitHub Actions workflow files
  - [x] `make helm-lint` -- validate Helm chart
- [x] Verify: `mise exec -- actionlint .github/workflows/ci.yml` passes

### Success Criteria

- `actionlint` reports no errors for `ci.yml`
- `lint-repo` job is independent (no `needs`) and runs in parallel with existing
  jobs
- All 6 lint steps (yamllint x2, yamlfmt, markdownlint, actionlint, helm lint)
  are present
- `jdx/mise-action@v2` is used to install tools from `mise.toml` (single source
  of truth)

---

## Phase 5: CI Workflow -- `helm-unittest` Job

Add Helm unit testing to the CI pipeline.

### Tasks

- [x] Add `helm-unittest` job to `.github/workflows/ci.yml` (no `needs` -- runs
      in parallel)
- [x] Job steps:
  - [x] `actions/checkout@v6`
  - [x] `d3adb5/helm-unittest-action@v2` with
        `charts: charts/server-price-tracker` and `flags: --color`
- [x] Verify: `mise exec -- actionlint .github/workflows/ci.yml` passes

### Success Criteria

- `actionlint` reports no errors for `ci.yml`
- `helm-unittest` job is independent (no `needs`) and runs in parallel with
  existing jobs
- `d3adb5/helm-unittest-action@v2` is configured with correct chart path and
  color output
- Unit tests will run on every push to `main` and every PR targeting `main`

---

## Phase 6: Final Verification & Cleanup

Run full verification suite and update project documentation.

### Tasks

- [ ] Run full local verification:
  - [ ] `make helm-lint`
  - [ ] `make helm-unittest`
  - [ ] `make helm-template` -- default values
  - [ ] `make helm-template-ci` -- CI values
  - [ ] `make lint-yaml`
  - [ ] `make lint-yaml-charts`
  - [ ] `make lint-yaml-fmt`
  - [ ] `make lint-md`
  - [ ] `make lint-actions`
- [x] Update `CLAUDE.md` if needed with new targets/tools
- [ ] Fix any lint issues found during full verification run

### Success Criteria

- All lint tools pass with no errors (`make lint-all`)
- All helm unit tests pass (`make helm-unittest`)
- `make helm-template` and `make helm-template-ci` render correctly
- `make lint-actions` passes across all workflow files
- No regressions in existing `make helm-lint` or `make helm-template` output
- All changes committed on the feature branch

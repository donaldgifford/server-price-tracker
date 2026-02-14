# Helm Chart Docs, Unit Tests & CI Linting -- Implementation Guide

Phased implementation of chart documentation, helm-unittest integration, and
repo-wide CI linting as described in [helm-chore.md](helm-chore.md).

---

## Phase 1: Chart README & Helmignore

Add chart documentation and ensure tests/docs are excluded from packaged chart
artifacts.

### Tasks

- [ ] Create `charts/server-price-tracker/README.md` with the following
      sections:
  - [ ] Header with chart name, description, badges (version, appVersion)
  - [ ] Prerequisites section (Kubernetes 1.27+, Helm 3.x, optional operators)
  - [ ] Usage section:
    - [ ] Adding the Helm repo (`helm repo add spt ...`)
    - [ ] Installing the chart (`helm install ...`)
    - [ ] Dependencies (CNPG operator, Ollama, Prometheus Operator -- when each
          is needed)
    - [ ] Uninstalling (`helm uninstall ...`)
    - [ ] Upgrading (`helm upgrade ...`)
  - [ ] Configuration table -- key `values.yaml` parameters grouped by section
        (image, config, secret, migration, probes, ingress/httproute, cnpg,
        ollama, serviceMonitor, autoscaling, tests)
  - [ ] Workarounds & Known Issues section
  - [ ] Further Information section with links to project docs
- [ ] Update `charts/server-price-tracker/.helmignore` -- add `tests/`,
      `README.md`, `ci/`
- [ ] Verify: `helm package charts/server-price-tracker/` produces a `.tgz` that
      does NOT contain `tests/`, `README.md`, or `ci/`
- [ ] Verify: `helm lint charts/server-price-tracker/` still passes

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

- [ ] Create `charts/server-price-tracker/tests/` directory
- [ ] Create `charts/server-price-tracker/tests/deployment_test.yaml`:
  - [ ] Test: renders a Deployment with correct kind, name, labels
  - [ ] Test: default image uses `.Chart.AppVersion` when `image.tag` is empty
  - [ ] Test: custom `image.tag` overrides appVersion
  - [ ] Test: container args are `["serve", "--config", "/etc/spt/config.yaml"]`
  - [ ] Test: container port matches `config.server.port` (8080)
  - [ ] Test: config volume mounted at `/etc/spt` (readOnly)
  - [ ] Test: envFrom references the secret
  - [ ] Test: liveness and readiness probes render by default
  - [ ] Test: probes can be nulled out (`livenessProbe: null`)
  - [ ] Test: migration init container renders when `migration.enabled=true`
  - [ ] Test: migration init container absent when `migration.enabled=false`
  - [ ] Test: CNPG env vars (DB_HOST, DB_USER, DB_PASSWORD, DB_NAME) render when
        `cnpg.enabled=true`
  - [ ] Test: replicas field absent when `autoscaling.enabled=true`
  - [ ] Test: `checksum/config` annotation present
- [ ] Create `charts/server-price-tracker/tests/service_test.yaml`:
  - [ ] Test: renders a Service of type ClusterIP
  - [ ] Test: service port is 8080
  - [ ] Test: selector labels match deployment
- [ ] Create `charts/server-price-tracker/tests/configmap_test.yaml`:
  - [ ] Test: renders a ConfigMap
  - [ ] Test: config data includes server, database, ebay, llm sections
- [ ] Create `charts/server-price-tracker/tests/secret_test.yaml`:
  - [ ] Test: renders Secret when `secret.create=true`
  - [ ] Test: does not render when `secret.create=false`
  - [ ] Test: secret type is Opaque
- [ ] Create `charts/server-price-tracker/tests/cnpg-cluster_test.yaml`:
  - [ ] Test: does not render when `cnpg.enabled=false` (default)
  - [ ] Test: renders CNPG Cluster when `cnpg.enabled=true`
  - [ ] Test: correct apiVersion (`postgresql.cnpg.io/v1`), kind (`Cluster`)
  - [ ] Test: instances, imageName, storage size match values
  - [ ] Test: bootstrap database and owner set correctly
  - [ ] Test: storageClass rendered when specified
- [ ] Create `charts/server-price-tracker/tests/ollama_test.yaml`:
  - [ ] Test: StatefulSet does not render when `ollama.enabled=false` (default)
  - [ ] Test: Service does not render when `ollama.enabled=false`
  - [ ] Test: StatefulSet renders with correct model, resources when enabled
  - [ ] Test: PVC storage size and storageClass correct
- [ ] Create `charts/server-price-tracker/tests/servicemonitor_test.yaml`:
  - [ ] Test: does not render when `serviceMonitor.enabled=false` (default)
  - [ ] Test: renders ServiceMonitor when enabled
  - [ ] Test: correct path (`/metrics`) and interval (`30s`)
- [ ] Create `charts/server-price-tracker/tests/ingress_test.yaml`:
  - [ ] Test: Ingress does not render when `ingress.enabled=false` (default)
  - [ ] Test: Ingress renders with hosts/paths when enabled
  - [ ] Test: HTTPRoute does not render when `httpRoute.enabled=false` (default)
  - [ ] Test: HTTPRoute renders when `httpRoute.enabled=true`
- [ ] Install helm-unittest plugin locally:
      `helm plugin install https://github.com/helm-unittest/helm-unittest.git`
- [ ] Verify: `helm unittest charts/server-price-tracker/` -- all tests pass
- [ ] Verify: `helm lint charts/server-price-tracker/` still passes (tests don't
      break linting)

### Success Criteria

- 8 test files exist in `charts/server-price-tracker/tests/`
- `helm unittest charts/server-price-tracker/` passes with 0 failures
- Every conditional template (cnpg, ollama, serviceMonitor, ingress, httproute,
  secret, migration) has both enabled and disabled test cases
- Deployment test covers: image tag override, probes, init container, CNPG env
  vars, autoscaling replicas
- `helm lint` still passes

---

## Phase 3: Makefile Targets

Add Helm and linting Make targets for local development convenience.

### Tasks

- [ ] Create `scripts/makefiles/helm.mk` with targets:
  - [ ] `helm-lint` -- `helm lint charts/server-price-tracker/`
  - [ ] `helm-unittest` -- `helm unittest charts/server-price-tracker/`
  - [ ] `helm-template` -- `helm template test charts/server-price-tracker/`
  - [ ] `lint-yaml` -- run yamllint with both root and chart configs, run
        yamlfmt check
  - [ ] `lint-md` -- run markdownlint-cli2
- [ ] Update root `Makefile` -- add `include scripts/makefiles/helm.mk`
- [ ] Verify: `make helm-lint` passes
- [ ] Verify: `make helm-unittest` passes
- [ ] Verify: `make helm-template` renders without errors
- [ ] Verify: `make lint-yaml` runs yamllint and yamlfmt
- [ ] Verify: `make lint-md` runs markdownlint-cli2

### Success Criteria

- `scripts/makefiles/helm.mk` exists with all 5 targets
- Root `Makefile` includes `helm.mk`
- All Make targets execute successfully
- `make help` shows the new targets

---

## Phase 4: CI Workflow -- `lint-repo` Job

Add repo-wide linting to the CI pipeline using `jdx/mise-action@v2` to install
tools from `mise.toml`.

### Tasks

- [ ] Add `lint-repo` job to `.github/workflows/ci.yml` (no `needs` -- runs in
      parallel)
- [ ] Job steps:
  - [ ] `actions/checkout@v6`
  - [ ] `jdx/mise-action@v2` -- installs yamllint, markdownlint-cli2, yamlfmt,
        actionlint
  - [ ] `azure/setup-helm@v4` -- for helm lint
  - [ ] `yamllint -c .yamllint.yml . -e charts/` -- lint repo YAML (excludes
        charts/)
  - [ ] `yamllint -c charts/.yamllint.yml charts/` -- lint chart YAML (relaxed
        rules)
  - [ ] `yamlfmt -lint .` -- check YAML formatting (no modify)
  - [ ] `markdownlint-cli2 '**/*.md' '#node_modules'` -- lint all markdown
  - [ ] `actionlint` -- validate all GitHub Actions workflow files
  - [ ] `helm lint charts/server-price-tracker/` -- validate Helm chart
- [ ] Verify: `mise exec -- actionlint .github/workflows/ci.yml` passes

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

- [ ] Add `helm-unittest` job to `.github/workflows/ci.yml` (no `needs` -- runs
      in parallel)
- [ ] Job steps:
  - [ ] `actions/checkout@v6`
  - [ ] `d3adb5/helm-unittest-action@v2` with
        `charts: charts/server-price-tracker` and `flags: --color`
- [ ] Verify: `mise exec -- actionlint .github/workflows/ci.yml` passes

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
  - [ ] `helm lint charts/server-price-tracker/`
  - [ ] `helm unittest charts/server-price-tracker/`
  - [ ] `helm template test charts/server-price-tracker/` -- default values
  - [ ] `helm template test charts/server-price-tracker/ --values charts/server-price-tracker/ci/ci-values.yaml`
        -- CI values
  - [ ] `yamllint -c .yamllint.yml . --exclude charts/`
  - [ ] `yamllint -c charts/.yamllint.yml charts/`
  - [ ] `markdownlint-cli2 '**/*.md'`
  - [ ] `mise exec -- actionlint`
- [ ] Update `CLAUDE.md` if needed with new targets/tools
- [ ] Fix any lint issues found during full verification run

### Success Criteria

- All lint tools pass with no errors
- All helm unit tests pass
- `helm template` renders correctly with both default and CI values
- `actionlint` passes across all workflow files
- No regressions in existing `helm lint` or `helm template` output
- All changes committed on the chore branch

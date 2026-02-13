# Deployment Implementation Plan

Phased implementation of Helm chart testing and releasing CI/CD, as described in [DEPLOYMENT_STRATEGY.md](DEPLOYMENT_STRATEGY.md).

---

## Phase 1: Chart Test Infrastructure

Prepare the chart for CI testing by adding config files and making the test-connection pod conditional.

### Tasks

- [ ] Add `tests.connection.enabled` key (default `true`) to `charts/server-price-tracker/values.yaml`
- [ ] Wrap `charts/server-price-tracker/templates/tests/test-connection.yaml` in `{{- if .Values.tests.connection.enabled }}` / `{{- end }}` conditional guard
- [ ] Create `ct.yaml` at repo root with `chart-dirs: [charts]`, `target-branch: main`, `validate-maintainers: false`, and `lint-conf` pointing to `charts/.yamllint.yml`
- [ ] Create `charts/.yamllint.yml` — inherits `default`, disables `empty-values`, sets `line-length.max: 150`
- [ ] Create `charts/.yamlfmt.yml` — mirrors root `.yamlfmt.yml` but with `max_line_length: 150`
- [ ] Create `charts/server-price-tracker/ci/ci-values.yaml` with:
  - `image.repository: nginx`, `image.tag: alpine`
  - `migration.enabled: false`
  - `livenessProbe: null`, `readinessProbe: null`
  - `tests.connection.enabled: false`
  - `cnpg.enabled: false`, `ollama.enabled: false`
  - `config.notifications.discord.enabled: false`
- [ ] Verify: `helm lint charts/server-price-tracker/` passes
- [ ] Verify: `helm template test charts/server-price-tracker/ --values charts/server-price-tracker/ci/ci-values.yaml` renders without errors
- [ ] Verify: default template includes test-connection pod, `--set tests.connection.enabled=false` excludes it

### Success Criteria

- `helm lint` passes with no errors
- `helm template` with CI values renders a valid Deployment using `nginx:alpine`, no init containers, no probes, no test-connection pod
- `helm template` with default values still includes probes, migration init container, and test-connection pod
- All new config files (`ct.yaml`, `charts/.yamllint.yml`, `charts/.yamlfmt.yml`, `ci/ci-values.yaml`) exist and are well-formed

---

## Phase 2: CI Workflow — `helm-test` Job

Add chart linting and install testing to the CI pipeline.

### Tasks

- [ ] Add `helm-test` job to `.github/workflows/ci.yml` (no `needs` — runs in parallel with existing jobs)
- [ ] Job steps:
  - [ ] `actions/checkout@v6` with `fetch-depth: 0`
  - [ ] `azure/setup-helm@v4`
  - [ ] `actions/setup-python@v5` with `python-version: "3.12"`
  - [ ] `helm/chart-testing-action@v2`
  - [ ] `ct list-changed --config ct.yaml` — set output `changed=true` if charts changed
  - [ ] `ct lint --config ct.yaml` — gated on `changed == 'true'`
  - [ ] `helm/kind-action@v1` — gated on `changed == 'true'`
  - [ ] `ct install --config ct.yaml` — gated on `changed == 'true'`
- [ ] Verify: `actionlint` passes on `ci.yml`

### Success Criteria

- `actionlint` reports no errors for `ci.yml`
- The `helm-test` job is independent (no `needs`) and runs in parallel with `lint`, `test-go`, `build`, etc.
- All `ct lint`, `kind`, and `ct install` steps are gated behind the `list-changed` output so they are skipped when no chart files changed
- Python setup step is present (required by yamllint/yamale which `ct lint` uses internally)

---

## Phase 3: Release Workflow — `helm-release` Job

Add automated chart versioning and publishing to the release pipeline.

### Tasks

- [ ] Add `helm-release` job to `.github/workflows/release.yml`
- [ ] Set dependency chain: `needs: [bump-version, docker]`
- [ ] Set condition: `if: needs.bump-version.outputs.skipped != 'true'`
- [ ] Set permissions: `contents: write`
- [ ] Job steps:
  - [ ] `actions/checkout@v6` with `ref: main`, `fetch-depth: 0`, `token: ${{ secrets.GITHUB_TOKEN }}`
  - [ ] Configure git user as `github-actions[bot]`
  - [ ] `mikefarah/yq@v4` — install yq
  - [ ] Update Chart.yaml: strip `v` prefix from tag, set `version` and `appVersion`
  - [ ] `cat charts/server-price-tracker/Chart.yaml` — print for debug visibility
  - [ ] Commit Chart.yaml with message `chore: bump chart version to <tag> [skip ci]`
  - [ ] `git pull --rebase origin main && git push origin main`
  - [ ] `azure/setup-helm@v4`
  - [ ] `helm/chart-releaser-action@v1` with `charts_dir: charts` and `CR_TOKEN` env var
- [ ] Verify: `actionlint` passes on `release.yml`

### Success Criteria

- `actionlint` reports no errors for `release.yml`
- The `helm-release` job runs after `docker` in the dependency chain: `bump-version → release → docker → helm-release`
- The version update step correctly strips the `v` prefix (e.g. `v1.2.3` → `1.2.3`)
- The commit message includes `[skip ci]` to prevent workflow re-triggering
- The `git pull --rebase` handles potential race conditions from concurrent pushes
- `chart-releaser-action` is configured with `charts_dir: charts` and `CR_TOKEN`

---

## Phase 4: Labeler & Final Verification

Add PR labeling for chart changes and run full verification suite.

### Tasks

- [ ] Add `helm` label entry to `.github/labeler.yml` matching `charts/**`
- [ ] Run full verification:
  - [ ] `helm lint charts/server-price-tracker/`
  - [ ] `helm template test charts/server-price-tracker/` — default values
  - [ ] `helm template test charts/server-price-tracker/ --values charts/server-price-tracker/ci/ci-values.yaml` — CI values
  - [ ] `helm template test charts/server-price-tracker/ --set tests.connection.enabled=false` — confirm test pod excluded
  - [ ] `actionlint` — validates all workflow files
- [ ] Update `CLAUDE.md` if needed with new CI/deployment details

### Success Criteria

- `.github/labeler.yml` includes `helm` label for `charts/**` glob
- `helm lint` passes
- `helm template` renders correctly with both default and CI values
- `actionlint` passes with no errors across all workflow files
- All changes committed

---

## Phase 5: Post-Merge Manual Setup

One-time steps performed after the implementation is merged to `main`. These are not automated.

### Tasks

- [ ] Create orphan `gh-pages` branch:
  ```bash
  git checkout --orphan gh-pages
  git rm -rf .
  git commit --allow-empty -m "chore: initialize gh-pages for Helm chart repo"
  git push origin gh-pages
  git checkout main
  ```
- [ ] Enable GitHub Pages in repository settings: Source → `gh-pages` branch, `/ (root)`
- [ ] Verify Helm repo URL is accessible: `https://donaldgifford.github.io/server-price-tracker/`
- [ ] Test full release cycle: merge a PR with a `patch` label, verify:
  - [ ] `bump-version` creates a new tag
  - [ ] `release` publishes Go binaries
  - [ ] `docker` pushes multi-arch images to GHCR
  - [ ] `helm-release` updates Chart.yaml, publishes chart to GitHub Pages
  - [ ] `helm repo add spt https://donaldgifford.github.io/server-price-tracker/ && helm repo update && helm search repo spt` shows the chart

### Success Criteria

- `gh-pages` branch exists with `index.yaml` maintained by chart-releaser
- GitHub Pages serves the Helm repo at the expected URL
- A full release produces matching versions: Go binary tag, Docker image tag, and Helm chart version all correspond to the same semver

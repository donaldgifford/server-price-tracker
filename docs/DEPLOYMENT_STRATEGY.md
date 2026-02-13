# Helm Chart Testing & Releasing CI/CD

## Context

The Helm chart at `charts/server-price-tracker/` is complete but has no CI testing or release automation. We need to integrate `helm/chart-testing-action` for PR validation and `helm/chart-releaser-action` for publishing charts to a GitHub Pages-based Helm repo. The release flow auto-bumps Chart.yaml version/appVersion to match the app release tag from `pr-semver-bump` — no manual version bumps required in PRs.

## How the Tools Work

- **chart-testing (`ct`)**: Detects changed charts via git diff against target branch. Runs `yamllint` + `helm lint` for validation, and optionally `helm install` + `helm test` in a kind cluster. Auto-discovers CI values files from `<chart>/ci/*.yaml`.
- **chart-releaser (`cr`)**: Reads `version` from Chart.yaml, compares against existing GitHub releases named `<chart>-<version>`. If new, packages the chart, creates a GitHub release with the `.tgz`, and updates `index.yaml` on `gh-pages`.
- **chart-releaser does NOT auto-update Chart.yaml** — the release workflow updates version/appVersion via `yq` before running it.

## Files to Create (4)

### 1. `ct.yaml` (repo root)

Chart-testing config. Points to chart dir and a chart-specific yamllint config (the root `.yamllint.yml` has `empty-values: forbid-in-block-mappings` which rejects legitimate `values.yaml` entries like `tag: ""`).

### 2. `charts/.yamllint.yml`

Relaxed yamllint for chart YAML — disables `empty-values` rule, bumps line-length to 150 to match project golines setting. Everything else inherits from the `default` ruleset.

### 3. `charts/.yamlfmt.yml`

Chart-specific yamlfmt config mirroring the relaxed lint rules — bumps `max_line_length` to 150 to match the chart yamllint config and project golines setting.

### 4. `charts/server-price-tracker/ci/ci-values.yaml`

Values override for `ct install` in kind. Uses `nginx:alpine` as a stub image (the real app image won't exist in CI), disables migration init container, liveness/readiness probes, and test-connection pod. CNPG and Ollama are already disabled by default. `ct install` auto-discovers files in `<chart>/ci/*.yaml` — no extra flags needed.

## Files to Modify (5)

### 1. `.github/workflows/ci.yml` — Add `helm-test` job

New job running in parallel (no `needs` on existing jobs):

1. `actions/checkout@v6` with `fetch-depth: 0` (full git history for diff)
2. `azure/setup-helm@v4`
3. `actions/setup-python@v5` (required by yamllint/yamale)
4. `helm/chart-testing-action@v2`
5. `ct list-changed --config ct.yaml` — gates all subsequent steps; no-op when charts unchanged
6. `ct lint --config ct.yaml` — runs yamllint + helm lint + schema validation
7. `helm/kind-action@v1` — creates ephemeral k8s cluster (only if charts changed)
8. `ct install --config ct.yaml` — deploys chart in kind, runs `helm test`

### 2. `.github/workflows/release.yml` — Add `helm-release` job

New job after `docker` in the dependency chain: `bump-version → release → docker → helm-release`

1. Checkout `main` (not the tag — we need to commit the version bump back)
2. Configure git as `github-actions[bot]`
3. `mikefarah/yq@v4` to update Chart.yaml
4. Strip `v` prefix from tag (e.g. `v1.2.3` → `1.2.3`), set both `version` and `appVersion`
5. Commit with `[skip ci]` message to prevent re-triggering workflows
6. `git pull --rebase origin main` then push (handles race if another commit landed)
7. `azure/setup-helm@v4`
8. `helm/chart-releaser-action@v1` with `charts_dir: charts` and `CR_TOKEN`

chart-releaser creates a GitHub release named `server-price-tracker-1.2.3` (distinct from the GoReleaser release at tag `v1.2.3`) and updates `index.yaml` on `gh-pages`.

### 3. `charts/server-price-tracker/values.yaml` — Add `tests.connection.enabled`

New key (default `true`) so CI values can disable the test-connection pod. The pod hits `/healthz` which the nginx stub won't serve, causing `helm test` to fail without this toggle.

### 4. `charts/server-price-tracker/templates/tests/test-connection.yaml` — Conditional guard

Wrap entire content in `{{- if .Values.tests.connection.enabled }}` / `{{- end }}`.

### 5. `.github/labeler.yml` — Add `helm` label

```yaml
helm:
  - changed-files:
      - any-glob-to-any-file: "charts/**"
```

## Version Strategy

Chart.yaml version stays at `0.1.0` in source. On each app release:

1. `pr-semver-bump` creates tag `v1.2.3`
2. GoReleaser builds binaries, Docker builds images
3. `helm-release` job updates Chart.yaml `version` and `appVersion` to `1.2.3`, commits with `[skip ci]`, then chart-releaser publishes

No manual Chart.yaml bumps required in PRs — the release workflow handles all version synchronization.

## One-Time Manual Setup (post-merge)

1. Create orphan `gh-pages` branch:

   ```bash
   git checkout --orphan gh-pages
   git rm -rf .
   git commit --allow-empty -m "chore: initialize gh-pages for Helm chart repo"
   git push origin gh-pages
   git checkout main
   ```

2. Enable GitHub Pages: Settings → Pages → Source: `gh-pages` branch, `/ (root)`

3. Helm repo URL: `https://donaldgifford.github.io/server-price-tracker/`

## Verification

```bash
# Validate chart still lints after values.yaml change
helm lint charts/server-price-tracker/

# Validate CI values render correctly with stub image
helm template test charts/server-price-tracker/ --values charts/server-price-tracker/ci/ci-values.yaml

# Validate test-connection conditional
helm template test charts/server-price-tracker/ | grep -c 'test-connection'                                      # > 0
helm template test charts/server-price-tracker/ --set tests.connection.enabled=false | grep -c 'test-connection'  # 0

# Validate workflow syntax (actionlint via mise)
actionlint
```

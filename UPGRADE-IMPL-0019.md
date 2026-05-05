# Upgrading to IMPL-0019 (OTel + Langfuse + judge worker)

Operator runbook for upgrading an existing `server-price-tracker` install
to the IMPL-0019 release (PR #51, merge commit `7fafcd0`). Assumes:

- Clickhouse is up and reachable from the cluster (the OTel Collector
  ships traces here; spt itself never talks to Clickhouse).
- Langfuse is up and reachable from the cluster, and you've created
  a project + API key pair.
- An OTel Collector is deployed in-cluster with an OTLP/gRPC receiver
  on `:4317` and a Clickhouse exporter wired up.

The IMPL-0019 code is **disabled by default** — every observability
subtree (`otel`, `langfuse`, `judge`) defaults to `enabled: false`. A
plain image upgrade with no config changes is byte-identical to today.
This means **you can roll the image first, then turn flags on one at a
time**, with the option to disable any subtree at any moment.

Delete this file when the upgrade is complete.

---

## Pre-flight checks

```bash
# Confirm Langfuse is reachable from a cluster pod
kubectl run -it --rm langfuse-probe --image=curlimages/curl --restart=Never -- \
  curl -sS https://langfuse.example.com/api/public/health
# Expect: {"status":"OK"} (or similar 200)

# Confirm OTel Collector is reachable on :4317
kubectl run -it --rm otel-probe --image=curlimages/curl --restart=Never -- \
  curl -sS http://otel-collector.observability:4317
# Expect: gRPC handshake content (not a connection refused)
```

Capture the current state for rollback:

```bash
kubectl get configmap server-price-tracker-config -o yaml > /tmp/spt-config.before.yaml
kubectl get deployment server-price-tracker -o yaml > /tmp/spt-deploy.before.yaml
helm get values server-price-tracker -n spt > /tmp/spt-values.before.yaml
psql ... -c "SELECT version FROM schema_migrations ORDER BY version;" > /tmp/migrations.before.txt
```

---

## Step 0 — chart gap (IMPORTANT, read first)

The Helm chart's `templates/configmap.yaml` does **not yet** render the
`observability:` subtree. A chart-side PR is needed before the new
config keys can flow through `helm upgrade`. Two paths:

**Path A — Wait for the chart-side PR.** A follow-up will land
templates/configmap.yaml rendering for `observability.{otel,langfuse,judge}`
plus values defaults. If you want zero local patches, wait for that
release before continuing.

**Path B — Local patch to unblock now.** Append the block below to
`templates/configmap.yaml` (last line is `format: {{ .Values.config.logging.format }}`):

```yaml
    {{- with .Values.config.observability }}
    observability:
      {{- with .otel }}
      otel:
        enabled: {{ .enabled }}
        endpoint: {{ .endpoint | quote }}
        service_name: {{ .service_name | quote }}
        insecure: {{ .insecure }}
        timeout: {{ .timeout }}
      {{- end }}
      {{- with .langfuse }}
      langfuse:
        enabled: {{ .enabled }}
        endpoint: {{ .endpoint | quote }}
        public_key: {{ .public_key | quote }}
        secret_key: {{ .secret_key | quote }}
        buffer_size: {{ .buffer_size }}
        timeout: {{ .timeout }}
        {{- with .model_costs }}
        model_costs:
          {{- toYaml . | nindent 10 }}
        {{- end }}
      {{- end }}
      {{- with .judge }}
      judge:
        enabled: {{ .enabled }}
        backend: {{ .backend | quote }}
        model: {{ .model | quote }}
        interval: {{ .interval }}
        lookback: {{ .lookback }}
        batch_size: {{ .batch_size }}
        daily_budget_usd: {{ .daily_budget_usd }}
      {{- end }}
    {{- end }}
```

Then add to `values.yaml` under `config:`:

```yaml
config:
  # ... existing keys ...
  observability:
    otel:
      enabled: false
      endpoint: ""
      service_name: "server-price-tracker"
      insecure: false
      timeout: 10s
    langfuse:
      enabled: false
      endpoint: ""
      public_key: ""
      secret_key: ""
      buffer_size: 1000
      timeout: 10s
      model_costs: {}
    judge:
      enabled: false
      backend: ""
      model: ""
      interval: 15m
      lookback: 6h
      batch_size: 50
      daily_budget_usd: 10.0
```

**Validate with `helm template`** before applying so you don't ship a
broken YAML render:

```bash
helm template server-price-tracker ./charts/server-price-tracker \
  --values your-overrides.yaml | grep -A 30 'observability:'
```

If you use Kustomize (`deploy/`), the same gap applies to
`deploy/base/configmap.yaml` — append the keys directly to the inline
config there.

---

## Step 1 — stage the Langfuse secret

Langfuse public+secret keys land via env vars referenced by
`os.ExpandEnv` in the config loader. The Helm chart's secret pattern
is `{fullname}-secrets`.

```bash
kubectl create secret generic server-price-tracker-secrets \
  --from-literal=LANGFUSE_PUBLIC_KEY='pk-lf-...' \
  --from-literal=LANGFUSE_SECRET_KEY='sk-lf-...' \
  --dry-run=client -o yaml | kubectl apply -f -
# OR if you use sealed-secrets / external-secrets, route via your
# normal pipeline. The two keys must end up in the same secret the
# deployment already references via envFrom.
```

Confirm the deployment picks them up — restart will happen on Step 2:

```bash
kubectl describe deployment server-price-tracker | grep -A2 envFrom
```

---

## Step 2 — roll the image with all flags OFF

```bash
helm upgrade server-price-tracker ./charts/server-price-tracker \
  --namespace spt \
  --values your-overrides.yaml \
  --version <new-chart-version>   # appVersion bumps via release workflow
```

Behaviour at this point: byte-identical to today. New code is on disk,
new migrations applied (next step), but no OTel traffic, no Langfuse
calls, no judge cron entry registered.

### Verify migrations 012 + 013 applied

The init container runs `server-price-tracker migrate` against
`/etc/spt/config.yaml`. Watch its logs:

```bash
kubectl logs -n spt -l app.kubernetes.io/name=server-price-tracker -c migrate --tail=50
# Expect lines like:
#   applying migration 012_add_trace_ids.sql
#   applying migration 013_add_judge_scores.sql
#   applied 2 migrations
```

Then confirm in Postgres:

```sql
SELECT version, applied_at
FROM schema_migrations
ORDER BY version DESC
LIMIT 5;
-- Expect 013_add_judge_scores.sql at the top.
```

Confirm the new columns + table exist:

```sql
\d extraction_queue
-- expect new column: trace_id text NULL

\d alerts
-- expect new column: trace_id text NULL

\d judge_scores
-- expect: alert_id (PK), score, reason, model, input_tokens,
-- output_tokens, cost_usd, judged_at, plus judge_scores_judged_at_idx
```

If migrations did **not** apply (init container failed, network blip,
etc.), apply manually in this order:

```bash
# Port-forward the DB (via the chart's CNPG cluster, if you use it)
kubectl port-forward svc/server-price-tracker-db-rw 5433:5432 -n spt

# Apply 012 first (depends on no later migration)
psql -h localhost -p 5433 -U tracker -d server_price_tracker \
  -f migrations/012_add_trace_ids.sql

# Then 013
psql -h localhost -p 5433 -U tracker -d server_price_tracker \
  -f migrations/013_add_judge_scores.sql

# Record both as applied so the next pod restart's init container is a no-op
psql -h localhost -p 5433 -U tracker -d server_price_tracker <<'SQL'
INSERT INTO schema_migrations (version) VALUES
  ('012_add_trace_ids.sql'),
  ('013_add_judge_scores.sql')
ON CONFLICT DO NOTHING;
SQL
```

### Smoke-test the disabled-mode rollout

```bash
# /healthz still 200, /metrics still emits the existing series, no new
# OTLP traffic, no calls to Langfuse.
kubectl port-forward svc/server-price-tracker 8080:8080 -n spt &
curl -sS http://localhost:8080/healthz   # 200
curl -sS http://localhost:8080/readyz    # 200
curl -sS http://localhost:8080/metrics | grep -c "spt_"   # ≥30 series

# Confirm no OTLP traffic from the pod yet:
kubectl logs -n observability -l app=otel-collector --tail=200 | grep -c "server-price-tracker"
# Expect 0 (or unchanged from baseline).
```

Stop here if anything looks wrong. The image is rolled but no new
behaviour is active — easy rollback is `helm rollback`.

---

## Step 3 — enable OTel (traces + the new histograms)

Edit your overrides:

```yaml
config:
  observability:
    otel:
      enabled: true
      endpoint: "otel-collector.observability:4317"
      service_name: "server-price-tracker"
      insecure: true   # in-cluster plaintext; flip to false if your Collector terminates TLS
      timeout: 10s
```

```bash
helm upgrade server-price-tracker ./charts/server-price-tracker \
  --namespace spt --values your-overrides.yaml
```

Watch boot logs:

```bash
kubectl logs -n spt -l app.kubernetes.io/name=server-price-tracker --tail=100 \
  | grep -iE "otel|tracer|meter"
# Expect:
#   "otel exporter initialised" with endpoint=otel-collector.observability:4317
#   "tracer provider set"
#   "meter provider set"
# NO errors about "connection refused" or "context deadline".
```

Verify spans land in the Collector:

```bash
kubectl logs -n observability -l app=otel-collector --tail=200 \
  | grep -E "service.name.*server-price-tracker"
```

Verify in Clickhouse:

```sql
-- Adapt to your Collector's table name; default is otel_traces
SELECT
  ServiceName,
  SpanName,
  count() AS spans,
  max(Timestamp) AS latest
FROM otel_traces
WHERE ServiceName = 'server-price-tracker'
  AND Timestamp > now() - INTERVAL 5 MINUTE
GROUP BY ServiceName, SpanName
ORDER BY spans DESC;
-- Expect rows for: ingestion.cycle, extraction.queue.claim,
-- extract.classify, extract.attributes, alert.evaluate
```

Verify the new histograms emit:

```bash
curl -sS http://localhost:8080/metrics | grep -E "spt_extraction_duration|spt_alert_eval_duration"
# Expect ≥2 series each (one per ComponentType)
```

### Troubleshooting OTel

- **No spans in Collector:** check `kubectl describe pod` for env vars,
  confirm `OTEL_EXPORTER_OTLP_ENDPOINT` is *not* also set (would override
  config), confirm DNS for `otel-collector.observability:4317` resolves
  inside the pod.
- **`rpc error: code = Unimplemented`:** Collector is up but doesn't
  have the OTLP receiver wired. Add `otlp:` under `receivers:` and a
  `traces` pipeline.
- **TLS errors with `insecure: true`:** Collector is terminating TLS
  but config says plaintext. Flip `insecure: false` and supply a CA.
- **Spans appear but `trace_id` column in `extraction_queue`/`alerts`
  is always NULL:** the propagation path is gated on `otel.enabled`;
  confirm a fresh pod after the toggle.

---

## Step 4 — enable Langfuse

Add to overrides:

```yaml
config:
  observability:
    langfuse:
      enabled: true
      endpoint: "https://langfuse.example.com"
      public_key: "${LANGFUSE_PUBLIC_KEY}"
      secret_key: "${LANGFUSE_SECRET_KEY}"
      buffer_size: 1000
      timeout: 10s
      model_costs: {}   # leave empty unless you run an in-house model Langfuse can't price
```

```bash
helm upgrade server-price-tracker ./charts/server-price-tracker \
  --namespace spt --values your-overrides.yaml
```

Watch boot logs:

```bash
kubectl logs -n spt -l app.kubernetes.io/name=server-price-tracker --tail=100 \
  | grep -iE "langfuse"
# Expect:
#   "langfuse client initialised" endpoint=https://langfuse.example.com
#   "langfuse buffered drain started"
# NO "missing public_key" / "missing secret_key" / "401 Unauthorized" errors.
```

Wait for the next ingestion tick (≤15 min), then check Langfuse UI:

- Project → Traces — expect new traces with name like `extract.classify`
  and `extract.attributes`.
- Each trace should have at least one Generation child with
  `model = <ollama/anthropic backend>` and token usage populated.

Verify the buffer is healthy:

```bash
curl -sS http://localhost:8080/metrics | grep "spt_langfuse_buffer"
# spt_langfuse_buffer_depth      should sit near 0 in steady state
# spt_langfuse_buffer_drops_total  should stay at 0
# spt_langfuse_writes_total{result="success"} should grow
# spt_langfuse_writes_total{result="error"}   should stay at 0 (or near)
```

### Verify operator dismissal scoring

The dismiss-as-Langfuse-score path is wired automatically. From the
alert review UI (`/alerts`):

1. Dismiss an alert.
2. Click the **Trace ↗** link — opens Langfuse to the underlying trace.
3. Confirm a new score `name = "operator_dismissed", value = 1.0`
   appears on the trace.
4. Restore the alert — confirm a follow-up `value = 0.0` score lands
   on the same trace.

### Troubleshooting Langfuse

- **`spt_langfuse_buffer_drops_total > 0`:** the buffer is overflowing.
  Either Langfuse is slow/down, or write volume exceeds drain
  throughput. Check `spt_langfuse_write_duration_seconds` p95; if it's
  >1s, Langfuse-side latency is the issue.
- **`writes_total{result="error"}` climbing:** check pod logs for
  `langfuse buffered write failed` (debug-level by default — bump
  `logging.level: debug` to see them); usually 401 (bad keys) or 5xx
  (Langfuse outage).
- **Traces appear but no Generations:** the LangfuseBackend wraps each
  `LLMBackend.Generate` call; if Generations are empty, the extract
  path isn't routing through the wrapper. Confirm with
  `kubectl logs ... | grep "langfuse backend wrapping"` at boot.
- **No traces appear at all:** confirm `otel.enabled: true` first
  (Langfuse traces piggyback on OTel trace IDs); without OTel, the
  trace_id passed to Langfuse is empty and the writes get dropped.

---

## Step 5 — cold-start the judge examples (REQUIRED before enabling judge)

The judge ships with an empty `pkg/judge/examples.json`. Without
operator-curated few-shot examples, the LLM has no rubric and produces
poor verdicts. Run the bootstrap CLI **before** flipping
`judge.enabled: true`.

This step runs locally against the live DB (port-forwarded). Allow ~30
minutes of operator time to label.

```bash
# 1. Generate a stratified sample from the live alerts table.
go run ./tools/judge-bootstrap \
  --config configs/config.dev.yaml \
  --output /tmp/judge-candidates.json \
  --samples-per-bucket 10
# Writes ~30 candidate alerts across deal/edge/noise buckets,
# interleaved by component type. Each row has empty label + verdict.

# 2. Hand-label each row in /tmp/judge-candidates.json:
#    - "label": one of "deal" | "edge" | "noise"
#    - "verdict.score": 0.0 ≤ x ≤ 1.0
#    - "verdict.reason": one-sentence justification (≤ 80 chars)

# 3. Validate + apply.
go run ./tools/judge-bootstrap \
  --apply /tmp/judge-candidates.json \
  --output pkg/judge/examples.json
# Validates ranges, dedupes, writes the new examples.json into the repo.

# 4. Rebuild the image (examples.json is embedded via go:embed) and roll
#    it via the standard release workflow before continuing.
```

If you skip this step, the judge will run with zero examples and grade
every alert at ~0.5 (the prompt rubric is intact, the few-shot is
empty). Detectable via Grafana: `JudgeScoreDistribution` heatmap will
collapse to a single band.

---

## Step 6 — enable the judge worker

```yaml
config:
  observability:
    judge:
      enabled: true
      backend: ""              # "" inherits from llm.backend
      model: ""                # "" inherits from selected backend
      interval: 15m
      lookback: 6h
      batch_size: 50
      daily_budget_usd: 10.0   # hard cap; tune up if you see budget_exhausted on most days
```

```bash
helm upgrade server-price-tracker ./charts/server-price-tracker \
  --namespace spt --values your-overrides.yaml
```

Watch boot logs:

```bash
kubectl logs -n spt -l app.kubernetes.io/name=server-price-tracker --tail=100 \
  | grep -iE "judge"
# Expect:
#   "judge worker registered" interval=15m lookback=6h budget=10
```

Trigger one tick manually to validate end-to-end without waiting for
the cron:

```bash
curl -sS -X POST http://localhost:8080/api/v1/judge/run
# Expect: {"judged":N,"budget_exhausted":false}
# where N > 0 if there are un-scored alerts in the lookback window.
```

Verify in Postgres:

```sql
SELECT alert_id, score, reason, model, cost_usd, judged_at
FROM judge_scores
ORDER BY judged_at DESC
LIMIT 10;
-- Expect 1 row per alert in the last 6h, with non-empty reason.
```

Verify in Langfuse:

- Each judged trace should now have a Score `name = "judge_alert_quality"`
  with the verdict score + reason.

Verify cost tracking:

```sql
SELECT model,
       COUNT(*)                          AS verdicts,
       ROUND(SUM(cost_usd)::numeric, 4)  AS spend_usd
FROM judge_scores
WHERE judged_at >= date_trunc('day', NOW())
GROUP BY model
ORDER BY spend_usd DESC;
```

### Troubleshooting judge

- **`budget_exhausted=true` on the first tick:** likely the budget is
  too low for the lookback window. With `lookback: 6h` and
  `batch_size: 50`, expect ~50 verdicts/tick × 4 ticks/day × per-call
  cost. Tune `daily_budget_usd` up or `batch_size` down.
- **Worker logs `judge budget recheck failed; halting tick`:** Postgres
  is unhealthy. The worker halts intentionally rather than risk
  unbounded spend. Check DB health, then retry via
  `POST /api/v1/judge/run`.
- **All verdicts cluster at 0.5:** examples.json is empty (Step 5
  skipped) or the prompt template was edited. Re-run the bootstrap.
- **`Generation` records in Langfuse but no `Score` records on the
  same traces:** Langfuse is up but the score write is failing
  silently. Check `spt_langfuse_writes_total{result="error"}`.
- **Judge column in `/alerts` UI stays empty after a tick:** the
  alerts table query joins `judge_scores` on `alert_id` — if
  `judged_at < alert.created_at` (clock skew), the row is filtered
  out. Compare timestamps directly.

---

## Step 7 — bootstrap the regression dataset (operator workflow, not blocking)

Independent of the live observability stack — this is the per-PR
classifier accuracy gate (`make test-regression`). One-time setup:

```bash
# 1. Pull a stratified sample from the live DB (one per ComponentType
#    × condition × baseline-bucket) into an editable golden file.
go run ./tools/dataset-bootstrap \
  --config configs/config.dev.yaml \
  --output testdata/golden_classifications.json \
  --total 200

# 2. Audit the file in place. Each row has expected_component +
#    expected_product_key pre-filled from the current LLM labels —
#    your job is to flip the wrong ones.

# 3. Upload to Langfuse as a dataset (idempotent, title-hash IDs).
#    First create the empty dataset in the Langfuse UI; copy its ID.
go run ./tools/dataset-upload \
  --config configs/config.dev.yaml \
  --dataset testdata/golden_classifications.json \
  --langfuse-dataset-id <ds-id-from-langfuse-ui>

# 4. Test the gate locally.
make test-regression
# Expect a per-component accuracy table. Track the numbers in the
# next PR description.
```

`docs/OPERATIONS.md §8` is the long-form runbook for this workflow.

---

## Step 8 — refresh Grafana dashboards

The dashgen tool re-emits `deploy/grafana/data/spt-overview.json` with
an Observability row added. If you import dashboards via Grafana
provisioning:

```bash
# Option A: re-apply the Grafana ConfigMap that mounts the dashboard JSON
kubectl create configmap spt-grafana-dashboard \
  --from-file=spt-overview.json=deploy/grafana/data/spt-overview.json \
  -n monitoring \
  --dry-run=client -o yaml | kubectl apply -f -

# Option B: manual import via Grafana UI
# Settings → Dashboards → Import → upload spt-overview.json
```

The new row contains:

- **JudgeScoreDistribution** — heatmap of `spt_judge_score_bucket` by
  component type. Should drift over weeks as the dataset matures.
- **JudgeVsOperatorAgreement** — judge "noise" rate vs operator
  dismissal rate. Divergence indicates judge drift.
- **JudgeCostByModel** — cumulative `spt_judge_cost_usd_total`. Should
  trend ~`(daily_budget × days)`.
- **PipelineStageVolume** — proxy for OTel span volume across
  ingestion / extraction / alert eval / notification.

Re-import the Prometheus recording rules at the same time:

```bash
kubectl create configmap spt-recording-rules \
  --from-file=spt-recording-rules.yaml=deploy/prometheus/spt-recording-rules.yaml \
  -n monitoring \
  --dry-run=client -o yaml | kubectl apply -f -
```

---

## Step 9 — calendar the 7-day Collector tail-sampling review

`docs/OPERATIONS.md §8` ("7-day Collector tail-sampling review
checklist") describes the review. Set a reminder for **2026-05-12**
(7 days from rollout). The checklist is a decision matrix on whether
to tighten / loosen the Collector's `tail_sampling` config based on
observed trace volume in Clickhouse. Mechanical review — the runbook
walks through the queries.

---

## Rollback

The disabled-default design makes most rollback cheap. Pick the level
that matches what's broken.

### Rollback level 1: disable a single subtree

Flip the offending flag in your overrides; `helm upgrade` again. New
pod boots with that subtree off, no other behaviour changes.

```yaml
# Disable judge but keep otel + langfuse:
config:
  observability:
    judge:
      enabled: false
```

### Rollback level 2: full chart rollback

```bash
helm history server-price-tracker -n spt
helm rollback server-price-tracker <previous-revision> -n spt
```

The new image is still deployed, but with the previous values config —
all observability subtrees back to default-off.

### Rollback level 3: image rollback

```bash
helm upgrade server-price-tracker ./charts/server-price-tracker \
  --namespace spt --values your-overrides.yaml \
  --set image.tag=<previous-app-version>
```

The previous image is back. **Note**: migrations 012 and 013 stay
applied — they only add columns/tables and the previous code ignores
them. No down-migration needed.

### Rollback level 4: drop the new tables/columns

Only if you want a clean uninstall. Reversal SQL (run in this order):

```sql
-- 013 down
DROP TABLE IF EXISTS judge_scores;

-- 012 down
ALTER TABLE alerts DROP COLUMN IF EXISTS trace_id;
ALTER TABLE extraction_queue DROP COLUMN IF EXISTS trace_id;

-- Remove the migration records so a future re-deploy will re-apply
DELETE FROM schema_migrations
WHERE version IN (
  '012_add_trace_ids.sql',
  '013_add_judge_scores.sql'
);
```

---

## What to watch in the first 24 hours

- `spt_langfuse_buffer_drops_total` — should be **0**. Non-zero means
  the buffer is overflowing under load; investigate Langfuse-side
  latency.
- `spt_langfuse_writes_total{result="error"}` — should be **near 0**
  in steady state. A persistent error rate means Langfuse is
  rejecting writes (auth issue, project mis-config, schema drift).
- `spt_judge_budget_exhausted_total` — should be **0 most days**. ≥1
  per day means the daily budget is too tight for current alert
  volume.
- `spt_extraction_duration_seconds` p95 — should match pre-upgrade
  extraction latency (the OTel instrumentation adds <1ms overhead).
  A jump means the OTel SDK is back-pressuring on the extract path.
- `kubectl logs -n spt -l app.kubernetes.io/name=server-price-tracker | grep -iE "panic|fatal|error.*observ"` — should be empty.
- Langfuse → Traces — should grow at the same rate as Discord alerts.
  Stalled trace count means the OTel propagation is broken.

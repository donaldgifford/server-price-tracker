# Operations Guide

How to set up, configure, and run Server Price Tracker from scratch
through production operation.

## Prerequisites

- Kubernetes cluster (Talos Linux or similar)
- Helm 3.x
- PostgreSQL (via CNPG or external)
- Ollama instance (GPU node recommended) or Anthropic API key
- eBay Developer account with production keyset
- Discord webhook URL (for deal alerts)

## 1. eBay Developer Account Setup

### Create Production Keys

1. Go to [https://developer.ebay.com/my/keys](https://developer.ebay.com/my/keys)
2. Create or select an application
3. Under **Production**, generate a keyset
4. Note the **App ID (Client ID)** and **Cert ID (Client Secret)**

The application uses OAuth 2.0 client credentials grant (application
tokens, not user tokens). No user consent flow is needed — the Browse
API only requires application-level access.

### Sandbox vs. Production

| Setting | Sandbox | Production |
|---------|---------|------------|
| `EBAY_APP_ID` | Sandbox App ID | Production App ID |
| `EBAY_CERT_ID` | Sandbox Cert ID | Production Cert ID |
| `EBAY_TOKEN_URL` | `https://api.sandbox.ebay.com/identity/v1/oauth2/token` | *(leave empty — defaults to prod)* |
| `EBAY_BROWSE_URL` | `https://api.sandbox.ebay.com/buy/browse/v1/item_summary/search` | *(leave empty — defaults to prod)* |

**To switch from sandbox to production:** Replace the App ID and Cert ID
with production credentials and clear (or remove) `EBAY_TOKEN_URL` and
`EBAY_BROWSE_URL`. The code defaults to production URLs when these are
empty.

### API Rate Limits

eBay Browse API (production):
- **5,000 calls/day** for the `search` endpoint
- The `max_calls_per_cycle` config controls how many API calls are made
  per ingestion cycle (default: 50)
- With 15-minute ingestion intervals, that's ~4,800 calls/day at max
  (96 cycles x 50 calls)
- Adjust `max_calls_per_cycle` or `ingestion_interval` if you have
  more/fewer watches

## 2. Database Setup

### Option A: CNPG (CloudNativePG) via Helm Chart

Set in your Helm values:

```yaml
cnpg:
  enabled: true
  instances: 1
  bootstrap:
    database: spt
    owner: spt
  storage:
    size: 10Gi
    storageClass: your-storage-class
```

The chart automatically wires `DB_HOST`, `DB_USER`, `DB_PASSWORD`, and
`DB_NAME` from the CNPG-generated secret. No manual secret configuration
needed for database credentials when using CNPG.

### Option B: External PostgreSQL

Set in your Helm values:

```yaml
cnpg:
  enabled: false

secret:
  create: true
  values:
    DB_HOST: "your-postgres-host"
    DB_NAME: "spt"
    DB_USER: "spt"
    DB_PASSWORD: "your-password"
```

### Run Migrations

Migrations run automatically via the init container when `migration.enabled: true`
(the default). The init container runs `server-price-tracker migrate --config
/etc/spt/config.yaml` before the main container starts.

To run migrations manually against a local database:

```bash
# Local dev
make migrate

# Or directly
go run ./cmd/server-price-tracker migrate --config configs/config.dev.yaml
```

The migration (`migrations/001_initial_schema.sql`) creates all tables:
watches, listings, price_baselines, alerts, and associated indexes.

## 3. Helm Deployment

### Add the Chart Repo

```bash
helm repo add spt https://donaldgifford.github.io/server-price-tracker/
helm repo update
```

### Create Values Override

Create a `values-prod.yaml` with your configuration:

```yaml
image:
  tag: "v0.3.0"  # or the version you want to deploy

# -- Use an existing secret instead of creating one from values.
# This is recommended for production — manage secrets via
# sealed-secrets, external-secrets-operator, or similar.
secret:
  create: false
  existingSecret: "spt-secrets"

# -- Or create the secret from values (simpler but secrets in Helm values).
# secret:
#   create: true
#   values:
#     DB_HOST: ""
#     DB_NAME: "spt"
#     DB_USER: "spt"
#     DB_PASSWORD: ""
#     EBAY_APP_ID: "your-production-app-id"
#     EBAY_CERT_ID: "your-production-cert-id"
#     EBAY_TOKEN_URL: ""        # empty = production default
#     EBAY_BROWSE_URL: ""       # empty = production default
#     DISCORD_WEBHOOK_URL: "https://discord.com/api/webhooks/..."
#     ANTHROPIC_API_KEY: ""     # only if using anthropic backend

config:
  server:
    host: "0.0.0.0"
    port: 8080

  ebay:
    marketplace: EBAY_US
    max_calls_per_cycle: 50

  llm:
    backend: ollama
    ollama:
      # Endpoint is auto-resolved if ollama.enabled=true in values
      model: "mistral:7b-instruct-v0.3-q5_K_M"

  schedule:
    ingestion_interval: 15m
    baseline_interval: 6h

  notifications:
    discord:
      enabled: true

  logging:
    level: info
    format: json

cnpg:
  enabled: true
  instances: 1
  storage:
    size: 10Gi
    storageClass: your-storage-class

ollama:
  enabled: true
  model: "mistral:7b-instruct-v0.3-q5_K_M"
  gpu:
    enabled: true
    count: 1
  nodeSelector:
    nvidia.com/gpu.present: "true"
  persistence:
    enabled: true
    size: 30Gi

migration:
  enabled: true

httpRoute:
  enabled: true
  parentRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: internal
      namespace: gateway
  hostnames:
    - spt.yourdomain.dev
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
```

### Install

```bash
helm install spt spt/server-price-tracker -f values-prod.yaml -n spt --create-namespace
```

### Upgrade

```bash
helm upgrade spt spt/server-price-tracker -f values-prod.yaml -n spt
```

## 4. Verify Deployment

### Health Checks

```bash
# Liveness (process running)
curl https://spt.yourdomain.dev/healthz
# {"status":"ok"}

# Readiness (database reachable)
curl https://spt.yourdomain.dev/readyz
# {"status":"ready"}
```

### OpenAPI Spec and Docs

```bash
# Full OpenAPI 3.1 spec
curl https://spt.yourdomain.dev/openapi.json

# Interactive docs UI
open https://spt.yourdomain.dev/docs
```

### Metrics

```bash
curl https://spt.yourdomain.dev/metrics
```

## 5. Initialize the System

Once deployed and healthy, initialize the system in this order:

### Step 1: Create Watches

Watches define what to search for on eBay. Each watch has a search
query, component type, and optional filters.

```bash
# Using the spt CLI
spt watches create \
  --server https://spt.yourdomain.dev \
  --name "DDR4 ECC 32GB" \
  --query "DDR4 ECC REG 32GB server RAM" \
  --component-type ram \
  --score-threshold 70

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/watches \
  -H "Content-Type: application/json" \
  -d '{
    "name": "DDR4 ECC 32GB",
    "search_query": "DDR4 ECC REG 32GB server RAM",
    "component_type": "ram",
    "score_threshold": 70,
    "enabled": true
  }'
```

Component types: `ram`, `drive`, `server`, `cpu`, `nic`, `gpu`,
`workstation`, `desktop`, `other`.

#### Example: GPU watch with cold-start threshold

GPU listings hit a fresh baseline; the bucket is empty on first deploy.
While `priceScore` falls back to neutral 50 (composite ~55), seed the
watch with a low threshold and bump it once the baseline matures
(≥10 samples per product key, typically ~1 week):

```bash
spt watches create \
  --server https://spt.yourdomain.dev \
  --name "NVIDIA Tesla P40" \
  --query "NVIDIA Tesla P40 24GB" \
  --component-type gpu \
  --score-threshold 65   # bump to 80 after baseline matures

# Check baseline maturity:
psql -c "SELECT product_key, sample_count FROM price_baselines \
         WHERE product_key LIKE 'gpu:%' ORDER BY sample_count DESC;"

# Once a key has sample_count ≥ 10:
spt watches update --id <id> --score-threshold 80
```

#### Example: Workstation + desktop watches with cold-start threshold

Workstations and desktops follow the same cold-start dynamics as GPUs
(DESIGN-0015 / IMPL-0018). Seed each chassis line with a low threshold,
bump per-watch as its product key matures.

```bash
# Workstation watches — Dell Precision T-series, Pro Max, Lenovo
# ThinkStation P-series, HP Z-series.
spt watches create --type workstation --threshold 65 \
  --name "Dell Precision T-series" --query "Dell Precision T7920 OR T7820 OR T5820"

spt watches create --type workstation --threshold 65 \
  --name "Lenovo ThinkStation P-series" --query "Lenovo ThinkStation P620 OR P520 OR P340"

spt watches create --type workstation --threshold 65 \
  --name "HP Z-series" --query "HP Z8 G4 OR Z6 G4 OR Z4 G4"

# Desktop watches — Dell OptiPlex / Pro, Lenovo ThinkCentre, HP EliteDesk.
spt watches create --type desktop --threshold 65 \
  --name "Dell OptiPlex" --query "Dell OptiPlex 7080 OR 7090 OR 7000"

spt watches create --type desktop --threshold 65 \
  --name "Lenovo ThinkCentre M-series" --query "Lenovo ThinkCentre M920 OR M720"

spt watches create --type desktop --threshold 65 \
  --name "HP EliteDesk" --query "HP EliteDesk 800 G6 OR 800 G7"

# Check baseline maturity:
psql -c "SELECT product_key, sample_count FROM price_baselines \
         WHERE product_key LIKE 'workstation:%' OR product_key LIKE 'desktop:%' \
         ORDER BY sample_count DESC;"

# Once a watch's matched product_key has sample_count ≥ 10:
spt watches update <id> --threshold 80
```

### Step 2: Trigger Initial Ingestion

The scheduler runs ingestion automatically on the configured interval
(default 15m), but you can trigger it immediately:

```bash
spt ingest --server https://spt.yourdomain.dev

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/ingest
```

This will:
1. Fetch listings from eBay for each enabled watch
2. Extract structured attributes via LLM (classify component, parse
   specs)
3. Generate product keys for baseline grouping
4. Store new listings in the database

### Step 3: Refresh Baselines

After ingestion has populated some listings, compute price baselines:

```bash
spt baselines refresh --server https://spt.yourdomain.dev

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/baselines/refresh
```

Baselines need `min_baseline_samples` (default: 10) listings per product
key before they activate. Until then, the price factor in scoring
defaults to a neutral 50.

### Step 4: Rescore Listings

After baselines are computed, rescore all existing listings:

```bash
spt rescore --server https://spt.yourdomain.dev

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/rescore
```

### Step 5: Verify Data

```bash
# List all watches
spt watches list --server https://spt.yourdomain.dev

# List top-scored listings
spt listings list --server https://spt.yourdomain.dev --order-by score --limit 10

# Filter by component type
spt listings list --server https://spt.yourdomain.dev --component-type ram --min-score 70
```

## 6. Ongoing Operations

### Automated Schedule

Once watches are created and the service is running, everything is
automated:

| Task | Interval | Config Key |
|------|----------|------------|
| Ingestion (fetch + extract + score) | 15m | `schedule.ingestion_interval` |
| Baseline recomputation | 6h | `schedule.baseline_interval` |
| Deal alerts (Discord) | Per ingestion | `notifications.discord` |

Watches are polled in a staggered fashion (default 30s apart) to avoid
eBay API bursts.

### Test Extraction

Test the LLM extraction pipeline without ingesting:

```bash
spt extract --server https://spt.yourdomain.dev \
  --title "Samsung 32GB DDR4 2666MHz ECC REG Server RAM M393A4K40CB2-CTD"

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/extract \
  -H "Content-Type: application/json" \
  -d '{"title": "Samsung 32GB DDR4 2666MHz ECC REG Server RAM M393A4K40CB2-CTD"}'
```

### Test eBay Search

Search eBay directly without storing results:

```bash
spt search --server https://spt.yourdomain.dev \
  --query "Dell PowerEdge R730" --limit 5

# Or via curl
curl -X POST https://spt.yourdomain.dev/api/v1/search \
  -H "Content-Type: application/json" \
  -d '{"query": "Dell PowerEdge R730", "limit": 5}'
```

### Reextract Listings With Quality Issues

The `/api/v1/reextract` endpoint re-runs extraction on listings whose
`component_type IS NOT NULL` but have missing or stale attributes:

```bash
curl -sX POST https://spt.yourdomain.dev/api/v1/reextract \
  -H 'content-type: application/json' \
  -d '{"component_type":"ram","limit":50}' | jq
```

This **will not** re-queue listings that were never extracted at all
(`component_type IS NULL`) — those need a manual SQL backfill. See
`docs/SQL_HELPERS.md` for the queries:

```sql
-- Identify the stuck listings
SELECT id, title, first_seen_at FROM listings
WHERE component_type IS NULL AND active = true
ORDER BY first_seen_at DESC;

-- Push them onto the queue
INSERT INTO extraction_queue (listing_id, priority)
SELECT id, 1 FROM listings WHERE component_type IS NULL AND active = true
ON CONFLICT DO NOTHING;
```

After the worker drains the queue, listings that still have NULL
`component_type` are unrecoverable — typically misclassified accessories
(e.g., drive caddies). Soft-deactivate them with
`UPDATE listings SET active = false WHERE id = '<uuid>';`.

### Watch Management

```bash
# Disable a watch (stops ingestion for it)
spt watches disable --server https://spt.yourdomain.dev <watch-id>

# Enable a watch
spt watches enable --server https://spt.yourdomain.dev <watch-id>

# Delete a watch
spt watches delete --server https://spt.yourdomain.dev <watch-id>

# Update a watch in place — only the flags you pass are changed.
# Tighten a threshold:
spt watches update --server https://spt.yourdomain.dev <watch-id> --threshold 80

# Add an attribute filter without dropping existing filters:
spt watches update --server https://spt.yourdomain.dev <watch-id> \
  --add-filter "attr:capacity_gb=eq:32"

# Replace the entire filter block (drops everything not in this command):
spt watches update --server https://spt.yourdomain.dev <watch-id> \
  --filter "attr:capacity_gb=eq:64" \
  --filter "price_max=500"

# Clear all filters:
spt watches update --server https://spt.yourdomain.dev <watch-id> --clear-filters
```

`--filter`, `--add-filter`, and `--clear-filters` are mutually
exclusive in a single invocation. `--add-filter` only merges the
`attr:`-prefixed keys into the existing `attribute_filters` map;
use `--filter` if you also want to change standard fields like
`price_max` or `seller_min_feedback`.

### Alert Review UI

The embedded `/alerts` page (DESIGN-0010) is a server-rendered table of
pending alerts with search, filter, dismiss/restore, and per-alert
detail views. Toggle with `config.web.enabled` (Helm value default
`true`).

```yaml
config:
  web:
    enabled: true
    # Absolute URL prefix used to deep-link from Discord summary
    # embeds back to /alerts. Empty = link omitted from embeds.
    alerts_url_base: "https://spt.yourdomain.dev"
```

**No built-in auth.** The page is unauthenticated. For production
expose it through one of:

- a Cilium HTTPRoute filter that requires a header / token,
- an oauth2-proxy sidecar in front of the deployment,
- network policy restricting the route to a private namespace.

`web.enabled: false` removes the entire `/alerts` route group (404
from the Echo router).

#### Summary mode (`notify.discord.summary_only`)

Phase 6 of IMPL-0015 added a `summary_only` flag on the Discord
config. When `true`:

- Each scheduler tick produces **one** Discord embed regardless of
  pending alert count (count + top score + per-component breakdown).
- The embed hyperlinks back to `<alerts_url_base>/alerts` if
  configured.
- Every pending alert is marked notified; the `/alerts` page becomes
  the queue.

When summary mode is on, bookmark `/alerts?status=undismissed` so your
queue view persists — the URL stays honest regardless of server
config (per IMPL-0015 Q7 resolution).

```yaml
config:
  notifications:
    discord:
      enabled: true
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      summary_only: true
```

#### Trace deep-links and dismissal scoring (IMPL-0019)

When `observability.langfuse.enabled: true` and the alert has a
`trace_id` (every alert created after migration 012 does), each row in
the `/alerts` table renders a **Trace ↗** button alongside the existing
**eBay ↗** link, deep-linking into the Langfuse trace viewer. The
detail page at `/alerts/{id}` renders the same button next to
Retry/Dismiss/Restore. Empty `observability.langfuse.endpoint`
suppresses the button across the UI — no degraded experience for users
with Langfuse off.

A separate JSON endpoint, `GET /api/v1/alerts/{id}/trace`, returns
`{"trace_url": "..."}` for programmatic consumers. Returns 404 when
Langfuse is disabled, the alert doesn't exist, or the alert predates
trace propagation.

Dismissing an alert (single or bulk) also fires a best-effort Langfuse
score:

- `name = "operator_dismissed"`, `value = 1.0`, attached to the
  alert's trace ID
- score writes go through the buffered Langfuse client — failures
  never fail a dismiss
- the same dismissals also drive the Phase 5 LLM-as-judge regression
  set when it ships, because operator-truth labels become the column
  the judge prompt is graded against

When `observability.judge.enabled: true` the alerts table renders an
extra **Judge** column. Phase 5 fills the cell; today the column
renders empty — the placeholder is here so the layout doesn't shift
mid-rollout.

#### Discord rate-limit observability

The Discord notifier now parses `X-RateLimit-*` headers on every
response and chunks batches into ≤10-embed POSTs. Operational signals:

- `spt_discord_rate_limit_remaining` — last observed remaining capacity
- `spt_discord_rate_limit_waits_total` — bucket-driven sleeps
- `spt_discord_429_total{global=…}` — 429s split by global flag
- `spt_discord_chunks_sent_total` — total POSTs sent
- `notifications.discord.inter_chunk_delay` — defensive sleep beyond
  bucket waits (default `0s`, set to e.g. `100ms` if a busy webhook
  trips global limits)

### Quota Monitoring

Check the current eBay API quota status:

```bash
spt quota --server https://spt.yourdomain.dev

# Or via curl
curl https://spt.yourdomain.dev/api/v1/quota
# {"daily_limit":5000,"daily_used":142,"remaining":4858,"reset_at":"2025-06-16T14:30:00Z"}
```

The quota endpoint reports:
- `daily_limit` — configured daily API call limit
- `daily_used` — calls used in the current rolling 24-hour window
- `remaining` — calls remaining before the limit is hit
- `reset_at` — when the current 24-hour window expires

#### Prometheus Metrics

The following eBay API metrics are exposed at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `spt_ebay_api_calls_total` | Counter | Total cumulative eBay API calls |
| `spt_ebay_daily_usage` | Gauge | Current daily call count within the rolling 24-hour window |
| `spt_ebay_daily_limit_hits_total` | Counter | Times the daily API limit was reached |

#### Grafana Alert Suggestions

- **Daily limit approaching:** Alert when `spt_ebay_daily_usage` exceeds
  80% of the configured daily limit (e.g., > 4000 for a 5000 limit)
- **Daily limit hit:** Alert on `rate(spt_ebay_daily_limit_hits_total[5m]) > 0`
- **Budget anomaly:** At the 12-hour mark, if daily usage is on pace to
  exceed the budget before the window resets

### LLM Token Metrics

Per-backend, per-model LLM token telemetry is exposed at `/metrics`. The
metrics record **billed tokens** — they include calls whose response failed
JSON parse or schema validation, since those calls were paid for. Use
`spt_extraction_failures_total` for the failed-call view.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `spt_extraction_tokens_total` | Counter | `backend`, `model`, `direction` | Total tokens billed. `direction` is `input` or `output`. |
| `spt_extraction_tokens_per_request` | Histogram | `backend`, `model` | Distribution of total tokens per LLM call (buckets 50–20000). |

`backend` matches the configured `config.llm.backend` value: `ollama`,
`anthropic`, or `openai_compat`. `model` echoes the active model
(`mistral:7b-instruct-v0.3-q5_K_M`, `claude-haiku-4-5-20251001`, etc.).

#### Useful PromQL

```promql
# Tokens per second by backend (stacked area panel)
sum by (backend) (rate(spt_extraction_tokens_total[5m]))

# Input vs output split by backend
sum by (backend, direction) (rate(spt_extraction_tokens_total[5m]))

# Estimated $/hour, Anthropic Haiku 4.5 pricing inlined
# ($1/MTok input, $5/MTok output)
(
  sum(rate(spt_extraction_tokens_total{backend="anthropic",direction="input"}[5m])) * 1.0  +
  sum(rate(spt_extraction_tokens_total{backend="anthropic",direction="output"}[5m])) * 5.0
) * 3600 / 1e6

# p95 prompt size per backend (catch prompt regressions)
histogram_quantile(0.95,
  sum by (le, backend) (rate(spt_extraction_tokens_per_request_bucket[5m]))
)
```

#### Backend Switching Verification

When switching `config.llm.backend` (e.g., Ollama → Anthropic for a
re-extraction pass), confirm the new series appears in `/metrics` after
the first successful extraction:

```bash
curl -s https://spt.yourdomain.dev/metrics | grep '^spt_extraction_tokens_total'
```

You should see series for the active backend value populating with
non-zero counts. This is the headline visualization for cost-comparing
local Ollama vs cloud backends in Grafana.

### Generate Postman Collection

Generate a Postman collection from the live server for API testing:

```bash
make postman SPT_SERVER_URL=https://spt.yourdomain.dev
```

Import `portman/postman_collection.json` into Postman. All 15 endpoints
are pre-configured with example payloads.

## 7. Troubleshooting

### eBay 502 Bad Gateway

The search endpoint returns 502 when eBay's API is unreachable. Check:
- Are `EBAY_APP_ID` and `EBAY_CERT_ID` set to **production** keys?
- Are `EBAY_TOKEN_URL` and `EBAY_BROWSE_URL` empty (or set to
  production URLs)?
- Can the pod reach `api.ebay.com` on port 443?

### Extraction Returns 500

The extract endpoint returns 500 when the LLM backend is unreachable:
- If using Ollama: is the Ollama pod running? Is the model pulled?
  Check `kubectl logs` on the Ollama StatefulSet.
- If using Anthropic: is `ANTHROPIC_API_KEY` set?
- Check `llm.timeout` — complex titles may need longer than the default
  30s (dev config uses 120s).

### Readiness Probe Failing

`/readyz` returns 503 when the database is unreachable:
- Check CNPG cluster status: `kubectl get cluster -n spt`
- Verify DB credentials in the secret match the CNPG-generated secret
- Check network policies if the pod can't reach the database

### No Alerts Firing

Alerts require all of:
1. A watch with `score_threshold` set
2. Listings with a composite score >= the threshold
3. Baselines with enough samples (default: 10 per product key)
4. `notifications.discord.enabled: true` with a valid webhook URL

Check listings have scores: `spt listings list --order-by score --limit 5`.
If all scores are 50, baselines haven't activated yet — need more
ingestion cycles.

## 8. OpenTelemetry, Clickhouse, and Langfuse (DESIGN-0016)

The application emits OTel traces and metrics over OTLP/gRPC to a
Collector that **must** be deployed and configured separately.
Clickhouse and Langfuse are also assumed to exist as platform
infrastructure (separate Helm charts, separate ownership). This
section describes the requirements `server-price-tracker` places
on those upstream components.

All three observability subtrees in `config.observability.*`
default to disabled — existing deployments are unaffected by the
upgrade until the operator opts in. See
`configs/config.example.yaml` for the full schema.

### Collector tail-sampling requirement

The Go application emits **100% of spans** (`AlwaysSample`).
Sampling decisions live entirely in the Collector. The `tail_sampling`
processor must be configured with at minimum these policies:

1. **Keep 100% of traces that produced an alert.** Match on the
   presence of any span with `name == alert.evaluate` and a
   `spt.alert.fired = true` attribute.
2. **Keep 100% of error traces.** Match on `status.code == ERROR`
   on any span in the trace.
3. **Keep 100% of extract spans.** Match on `name == extract.extract`
   or `name == extract.classify`. These carry the LLM-call
   observability we cannot lose.
4. **Sample N% of clean ingestion-only traces.** Operator-tunable;
   suggested 10% to start. Without this gate, ingestion traces
   (one per listing per cycle) dominate Clickhouse storage with
   low information density.

Example Collector snippet:

```yaml
processors:
  tail_sampling:
    decision_wait: 30s
    num_traces: 50000
    expected_new_traces_per_sec: 200
    policies:
      - name: keep-alerts
        type: string_attribute
        string_attribute:
          key: spt.alert.fired
          values: ["true"]
      - name: keep-errors
        type: status_code
        status_code:
          status_codes: [ERROR]
      - name: keep-extracts
        type: span_count
        span_count:
          min_spans: 1
          # combined with a span-name pattern processor upstream
      - name: sample-ingestion
        type: probabilistic
        probabilistic:
          sampling_percentage: 10
```

The exact config syntax depends on Collector version; the policy
intents above are what matters. Coordinate with the platform side
when standing up the Collector — IMPL-0019 Phase 7 reviews policy
effectiveness after 7 days of production data.

#### 7-day tail-sampling review checklist

Run this checklist exactly once, 7 days after the Phase 1-2 OTel
rollout reaches production. The output is the input to either
"keep current policy" or "hand off tweak to platform".

1. **Span-emission rate (sanity check).** The application is
   emitting 100% of spans regardless of sampling. Confirm
   ingestion is healthy:

   ```promql
   # Should be > 0 for every active stage.
   sum by (job) (rate(spt_ingestion_duration_seconds_count[1h]))
   sum by (job) (rate(spt_extraction_duration_seconds_count[1h]))
   sum by (job) (rate(spt_alerts_query_duration_seconds_count[1h]))
   ```

   If any stage is at 0 for the full 7 days, the corresponding
   span path isn't wired up — file a bug, don't tune sampling
   yet.

2. **Trace volume in Clickhouse.** Query the platform-side
   storage. Exact query depends on the Clickhouse schema your
   Collector exporter writes; the typical shape is:

   ```sql
   SELECT
     toStartOfDay(Timestamp) AS day,
     count() AS spans,
     sum(length(SpanAttributes)) AS attr_bytes
   FROM otel_traces
   WHERE Timestamp >= now() - INTERVAL 7 DAY
     AND ServiceName = 'server-price-tracker'
   GROUP BY day
   ORDER BY day;
   ```

   - **Healthy:** total spans/day stable or trending sideways.
   - **Concerning:** spans/day growing >2× day-over-day with no
     traffic increase — sampling policy is too permissive on
     ingestion. Bump `sample-ingestion` from 10% → 5%.
   - **Critical:** approaching Collector / Clickhouse storage
     budget (operator's own threshold). Cut probabilistic
     sampling further or raise `decision_wait` to coalesce more
     traces.

3. **Alert/error/extract retention (the high-value 100%
   policies).** Confirm those buckets are not accidentally being
   sampled out:

   ```sql
   -- Every alert in spt_alerts_created_total should have a trace.
   SELECT count(DISTINCT TraceId) AS alert_traces
   FROM otel_traces
   WHERE Timestamp >= now() - INTERVAL 7 DAY
     AND ServiceName = 'server-price-tracker'
     AND has(SpanAttributes, 'spt.alert.fired')
     AND SpanAttributes['spt.alert.fired'] = 'true';
   ```

   Compare against `sum(increase(spt_alerts_created_total[7d]))`
   (Prometheus). The Clickhouse count should equal the
   Prometheus counter. A gap means the `keep-alerts` policy is
   misconfigured — the trace fired but didn't make it past
   sampling.

4. **Judge-score plausibility.** Pull the per-bucket judge
   verdict counts:

   ```promql
   sum by (verdict) (increase(spt_judge_evaluations_total[7d]))
   ```

   Healthy: all three buckets (`deal`, `edge`, `noise`) have
   non-zero counts, with a distribution that roughly matches
   operator intuition — typically `noise` ≥ `edge` ≥ `deal`.
   Pathological signals:
   - All `noise` (judge thinks every alert is bad) → prompt
     issue; refresh `pkg/judge/examples.json`.
   - All `deal` (judge rubber-stamps everything) → prompt is
     missing the rubric; same refresh.
   - Zero `edge` for 7 days → judge isn't producing scores in
     the middle band, which suggests prompt is too binary.

5. **Manual judge validation.** Pick one alert flagged
   `verdict.score < 0.3` (judge says noise) by the worker.
   Compare against the operator's own intuition. If the judge
   was right, the score is doing what it was built for. If the
   judge was wrong, log it as an example to refresh.

6. **Decision matrix.**
   - All four checks green → policy is good; commit nothing.
   - Step 2 yellow/red → hand off probabilistic-sample tweak
     to platform side. Open a ticket against the Collector
     repo, not this one.
   - Step 3 gap → policy bug; same — open a Collector ticket
     citing the Prometheus / Clickhouse mismatch.
   - Steps 4-5 indicate prompt drift, not sampling — refresh
     `pkg/judge/examples.json` per the cold-start workflow
     above.

After running the checklist, paste the four numbers (alerts
traces, errors traces, extract spans, ingestion spans) into a
follow-up comment on the IMPL-0019 PR (or the rollout-tracking
issue) so future operators have a baseline to compare against.

### Enabling OTel in `server-price-tracker`

```yaml
observability:
  otel:
    enabled: true
    endpoint: "otel-collector.observability:4317"
    service_name: "server-price-tracker"
    insecure: false  # true only for local Collectors without TLS
    timeout: 10s
```

When enabled, the binary attaches `service.name`, `service.version`,
and `service.instance.id` (commit SHA) as resource attributes on
every span. The commit SHA is injected at build time via
`-ldflags "-X internal/version.CommitSHA=$(git rev-parse HEAD)"` —
running via `go run` falls back to the literal string `dev`.

### Disabled-mode guarantee

With `observability.otel.enabled: false` (the default), the binary
emits zero OTLP traffic and zero new log lines. The global OTel
tracer/meter providers remain as their no-op defaults, so any
future instrumentation calls (`otel.Tracer(...).Start(...)`)
become free non-operations. This is the explicit guarantee that
makes the IMPL-0019 phases shippable before Clickhouse or Langfuse
exist.

### LLM-as-judge worker (Phase 5)

The judge worker grades fired alerts retrospectively against an
operator-curated rubric so the operator can see, at a glance, which
alerts the LLM thought were genuine deals versus noise. Output goes
two places:

- `judge_scores` Postgres table — durable; one row per alert, ever.
  The alert detail page reads from here to render the score + reason
  next to the existing breakdown.
- Langfuse — best-effort; `Client.Score(traceID,
  "judge_alert_quality", value, reason)` lands on the same trace as
  the original extract generation, so a regression-set query in
  Langfuse can plot judge score versus operator dismissal over time.

Enable the worker:

```yaml
observability:
  judge:
    enabled: true
    backend: ""           # "" = inherit from llm.backend
    model: ""             # "" = inherit from selected backend
    interval: 15m
    lookback: 6h
    batch_size: 50
    daily_budget_usd: 10  # hard cap; tick exits early when reached
```

When enabled the binary registers a cron entry that runs every
`interval`. Each tick:

1. Looks at `SUM(cost_usd)` from `judge_scores` rows judged today
   (UTC). If ≥ `daily_budget_usd`, log a warning, increment
   `spt_judge_budget_exhausted_total`, and return.
2. Pulls up to `batch_size` un-judged alerts created within
   `lookback`. The query LEFT JOINs `price_baselines` so the prompt
   has the same percentile context the scorer used.
3. For each alert: calls the judge LLM, parses a strict
   `{score, reason}` JSON verdict, persists to `judge_scores`, posts
   a Langfuse score on the alert's trace.
4. Re-checks budget between calls so a long batch can't accidentally
   blow past the cap by one or two big-prompt verdicts.

Manual trigger:

```bash
spt judge run
# Judged 12 alerts.
# (or "Daily budget exhausted — remaining alerts will be picked up
# next run." when the cap was hit)
```

Or directly:

```bash
curl -X POST $SPT_API/api/v1/judge/run
{"judged":12,"budget_exhausted":false}
```

The endpoint returns 503 when `judge.enabled = false`; the CLI
surfaces that as a HTTP error.

#### Cold-start few-shot examples

The first run with an empty `pkg/judge/examples.json` works — the
prompt template renders cleanly with zero few-shot examples — but
verdict quality is materially lower until ~30 labelled examples are
in place. Workflow when bootstrapping or refreshing after a
ComponentType addition:

1. Pull a stratified sample of recent alerts from production
   (mix `dismissed` / `notified` / `active` so labels cover the
   distribution).
2. Manually label each as `deal` / `noise` / `edge` plus a short
   reason (≤80 chars).
3. Encode the set as JSON matching `pkg/judge.Example`:

   ```json
   [
     {
       "label": "deal",
       "alert": { ... AlertContext fields ... },
       "verdict": { "score": 0.92, "reason": "below P25 + full spec" }
     }
   ]
   ```

4. Save to `pkg/judge/examples.json`, commit, redeploy. The
   embedded JSON ships in the binary so the regression set stays
   versioned alongside the prompt.

A `tools/judge-bootstrap` interactive labeller is parked as a
follow-up — the manual workflow above gets the dataset built today
without blocking on the tool.

#### Budget knob

`daily_budget_usd: 10` is a deliberate ceiling, not an estimate of
spend. At Anthropic Haiku 4.5 rates (~$1/M input, $5/M output), a
typical 1.2k-token prompt + 25-token verdict costs ~$0.002 per
call; $10/day = 5,000 verdicts/day, comfortably above current
volume. Raise the cap if `spt_judge_budget_exhausted_total` is
incrementing during normal operation; lower it if a model upgrade
unexpectedly inflates per-verdict cost.

#### Disabled-mode guarantee

With `observability.judge.enabled: false`, no cron entry is
registered, the HTTP endpoint responds 503, and `judge_scores`
stays empty. The alert review UI hides the judge_score column
under the `JudgeEnabled` flag (Phase 4) so users not opted in see
the original lean table.

### Operator workflow (Phase 7)

The day-to-day routines once OTel + Langfuse + judge are all on.

#### Reading judge scores in the UI

The alert detail page (`/alerts/{id}`) renders a `Judge score`
row alongside the existing breakdown when `judge.enabled: true`.
Score is a 0.0–1.0 float; the inline reason is the LLM's one-line
rationale. Heuristic mapping:

- ≥ 0.7 — judge calls it a deal. If `notified=false`, the operator
  should reconsider their dismissal.
- 0.3–0.7 — edge call. The judge is uncertain; review the
  breakdown.
- < 0.3 — judge calls it noise. If the alert fired *and*
  `notified=true`, that's an alert-noise leak — the operator's
  pre-classifier or score curve is too generous.

The `Trace ↗` button on each alert deep-links to the Langfuse
trace for the original extract + classify call so the operator
can see exactly what the LLM saw.

#### Refreshing `examples.json` after a ComponentType addition

When a new ComponentType lands (e.g. workstation in IMPL-0018), the
judge has zero few-shot examples covering it and verdict quality
for that bucket craters until the operator backfills. Workflow:

1. After ~50 alerts have fired against the new ComponentType,
   manually classify a stratified sample of ~10–15 of them as
   `deal` / `noise` / `edge` with one-line reasons.
2. Append them to `pkg/judge/examples.json` (see "Cold-start
   few-shot examples" above for the exact JSON shape).
3. Commit, redeploy. The next judge tick uses the refreshed
   examples; existing `judge_scores` rows are untouched (judging
   is idempotent — no automatic re-judge of older alerts).
4. If the operator needs a forced re-judge of older rows, the
   manual SQL is `DELETE FROM judge_scores WHERE alert_id IN
   (...)` followed by `spt judge run` (within budget).

#### Weekly judge-vs-dismiss alignment report

Each week, pull a Langfuse trace export filtered on the past 7
days where both `operator_dismissed` and `judge_alert_quality`
scores are present on the same trace. The agreement rate is the
fraction of traces where one of:

- `operator_dismissed = 1` AND `judge_alert_quality < 0.3`
  (operator and judge both call it noise)
- `operator_dismissed = 0` AND `judge_alert_quality ≥ 0.7`
  (operator and judge both call it a deal)

Below 75% agreement is the trigger for refreshing
`pkg/judge/examples.json` or relabelling the dataset (next
section). The Grafana panel `JudgeVsOperatorAgreement` is the
near-real-time version of this report — the weekly export is the
audit record.

#### Quarterly dataset relabelling

Operator-curated truth drifts. Once a quarter:

1. Re-pull a stratified sample of the last quarter's alerts (~50
   listings across all enabled ComponentTypes).
2. Re-label using current operator intuition. If a label flipped
   relative to the original, update both
   `pkg/judge/examples.json` *and*
   `testdata/golden_classifications.json` (the regression dataset
   from Phase 6).
3. Commit, redeploy, run `make test-regression` to confirm the
   classifier accuracy didn't regress against the new truth set.
4. If accuracy drops > 5%, that's a signal the prompt is drifting
   away from operator intent — open a follow-up to retune
   `pkg/extract/prompts.go` rather than papering over it with new
   examples.

The full Phase 6 toolchain is now shipped:

- `tools/dataset-bootstrap` pulls a stratified sample from the
  live DB and pre-fills `expected_component` /
  `expected_product_key` from current LLM labels — operator
  audits in place.
- `tools/dataset-upload` POSTs one `DatasetItem` per row to
  Langfuse with deterministic title-hash IDs, idempotent under
  re-runs.
- `tools/regression-runner` runs the dataset against the
  configured backend (or `--backends` for side-by-side
  comparison) and, with `--langfuse-dataset-id <id>`, posts one
  `CreateDatasetRun` per backend tagged
  `classify_prompt:<sha>:<backend>`. Same title-hash algorithm
  as the upload tool, so runs and items align in the Langfuse
  UI without out-of-band coordination.

The first-time setup is a three-step operator pipeline:

```bash
# 1. Bootstrap candidates from the live DB.
go run ./tools/dataset-bootstrap --config configs/config.dev.yaml \
    --per-component 12 > testdata/golden_classifications.json
# 2. Audit + correct the JSON, then upload.
$EDITOR testdata/golden_classifications.json
go run ./tools/dataset-upload --config configs/config.dev.yaml \
    --langfuse-dataset-id <id-from-langfuse-ui>
# 3. Regression-test prompt-affecting PRs, with annotation.
go run ./tools/regression-runner --config configs/config.dev.yaml \
    --langfuse-dataset-id <id-from-langfuse-ui>
```

Quarterly relabelling is the same workflow with the
already-uploaded dataset ID.

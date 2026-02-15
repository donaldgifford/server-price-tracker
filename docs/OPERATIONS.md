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

Component types: `ram`, `drive`, `server`, `cpu`, `nic`, `other`.

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

### Watch Management

```bash
# Disable a watch (stops ingestion for it)
spt watches disable --server https://spt.yourdomain.dev <watch-id>

# Enable a watch
spt watches enable --server https://spt.yourdomain.dev <watch-id>

# Delete a watch
spt watches delete --server https://spt.yourdomain.dev <watch-id>
```

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

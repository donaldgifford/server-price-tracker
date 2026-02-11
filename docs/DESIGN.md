# eBay Deal Tracker — Design Document

## Overview

A Go service that monitors eBay listings for server hardware, extracts structured attributes via a local LLM, scores listings against historical baselines, and alerts on deals.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐     ┌────────────┐
│  eBay Browse │────▶│   Ingestion  │────▶│  LLM Extract │────▶│  Postgres  │
│     API      │     │    Loop      │     │  (llama.cpp) │     │            │
└─────────────┘     └──────────────┘     └──────────────┘     └─────┬──────┘
                                                                    │
                    ┌──────────────┐     ┌──────────────┐           │
                    │  Notify      │◀────│   Scorer /   │◀──────────┘
                    │  (ntfy/etc)  │     │  Alert Loop  │
                    └──────────────┘     └──────────────┘
```

## Scoring System

Each listing receives a composite score (0–100) derived from weighted sub-scores:

| Factor            | Weight | Description                                      |
|-------------------|--------|--------------------------------------------------|
| Price Percentile  | 40%    | Where this price falls vs. rolling baseline       |
| Seller Trust      | 20%    | Feedback score, %, top-rated status               |
| Condition         | 15%    | New/Like New/Used-Working/For Parts               |
| Quantity Value    | 10%    | Per-unit price for lots                           |
| Listing Quality   | 10%    | Has photos, specifics filled, description length  |
| Time Pressure     | 5%     | Auction ending soon, newly listed BIN             |

### Price Percentile Scoring

- p10 or below → 100 points (exceptional deal)
- p25           → 85
- p50 (median)  → 50
- p75           → 25
- p90+          → 0

Baselines computed per **normalized product key** (e.g., `ram:ddr4:ecc_reg:32gb:2666`) from
the last 90 days of sold listings.

### Seller Trust Scoring

- Feedback score: 0–100 → 0 pts, 100–500 → 40 pts, 500–5000 → 70 pts, 5000+ → 100 pts
- Feedback percentage: <95% → 0, 95–98% → 50, 98–99.5% → 80, 99.5%+ → 100
- Top-rated seller bonus: +20 pts (capped at 100)
- Final = avg of components, capped at 100

## Data Model

### watches (saved searches / alert configs)

```sql
CREATE TABLE watches (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,               -- "Cheap R640 RAM"
    search_query    TEXT NOT NULL,               -- eBay search string
    category_id     TEXT,                        -- eBay category filter
    component_type  TEXT NOT NULL,               -- ram | drive | server | cpu | nic | other
    filters         JSONB NOT NULL DEFAULT '{}', -- structured attribute filters
    score_threshold INTEGER NOT NULL DEFAULT 75, -- alert when score >= this
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Example filters:
```json
{
  "capacity_gb": {"min": 32, "max": 32},
  "memory_type": "DDR4",
  "ecc": true,
  "registered": true,
  "seller_min_feedback": 500,
  "seller_min_feedback_pct": 98.0,
  "price_max": 30.00,
  "condition": ["new", "used_working"]
}
```

### listings

```sql
CREATE TABLE listings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ebay_item_id    TEXT UNIQUE NOT NULL,
    title           TEXT NOT NULL,
    price           NUMERIC(10,2) NOT NULL,
    currency        TEXT NOT NULL DEFAULT 'USD',
    listing_type    TEXT NOT NULL,                -- auction | buy_it_now | best_offer
    condition_raw   TEXT,
    condition_norm  TEXT,                         -- new | like_new | used_working | for_parts
    seller_name     TEXT,
    seller_feedback_score   INTEGER,
    seller_feedback_pct     NUMERIC(5,2),
    seller_top_rated        BOOLEAN DEFAULT false,
    image_url       TEXT,
    item_url        TEXT NOT NULL,
    ebay_category   TEXT,
    shipping_cost   NUMERIC(10,2),
    quantity        INTEGER NOT NULL DEFAULT 1,
    -- LLM-extracted structured attributes
    component_type  TEXT,                         -- ram | drive | server | cpu | nic
    attributes      JSONB NOT NULL DEFAULT '{}',  -- extracted structured data
    extraction_confidence NUMERIC(3,2),           -- 0.00–1.00
    -- scoring
    score           INTEGER,                      -- 0–100 composite score
    score_breakdown JSONB,                        -- per-factor scores
    -- metadata
    listed_at       TIMESTAMPTZ,
    sold_at         TIMESTAMPTZ,
    sold_price      NUMERIC(10,2),
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_listings_component ON listings(component_type);
CREATE INDEX idx_listings_score ON listings(score DESC);
CREATE INDEX idx_listings_attrs ON listings USING GIN(attributes);
CREATE INDEX idx_listings_first_seen ON listings(first_seen_at);
```

### price_baselines

```sql
CREATE TABLE price_baselines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_key     TEXT UNIQUE NOT NULL,         -- e.g. "ram:ddr4:ecc_reg:32gb:2666"
    sample_count    INTEGER NOT NULL,
    p10             NUMERIC(10,2),
    p25             NUMERIC(10,2),
    p50             NUMERIC(10,2),
    p75             NUMERIC(10,2),
    p90             NUMERIC(10,2),
    mean            NUMERIC(10,2),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### alerts

```sql
CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    watch_id        UUID NOT NULL REFERENCES watches(id),
    listing_id      UUID NOT NULL REFERENCES listings(id),
    score           INTEGER NOT NULL,
    notified        BOOLEAN NOT NULL DEFAULT false,
    notified_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## LLM Extraction

### Component-Type Detection Prompt

First pass: classify the listing into a component type from the title alone (cheap/fast).

### Attribute Extraction Prompt (per component type)

Each component type has a tailored schema. Example for RAM:

```json
{
  "manufacturer": "Samsung",
  "capacity_gb": 32,
  "quantity": 4,
  "generation": "DDR4",
  "speed_mhz": 2666,
  "ecc": true,
  "registered": true,
  "form_factor": "RDIMM",
  "part_number": "M393A4K40CB2-CTD",
  "voltage": "1.2V",
  "rank": "2Rx4",
  "condition": "used_working",
  "confidence": 0.92
}
```

### Grammar-Constrained Output

Using llama.cpp's GBNF grammar support, force the LLM to output valid JSON matching the schema.
This eliminates parse failures and most hallucination for structured extraction.

## Product Key Generation

Normalized product keys for baseline grouping:

| Component | Key Format                                  | Example                       |
|-----------|---------------------------------------------|-------------------------------|
| RAM       | `ram:{gen}:{type}:{capacity}:{speed}`       | `ram:ddr4:ecc_reg:32gb:2666`  |
| Drive     | `drive:{interface}:{form}:{capacity}:{type}`| `drive:sas:2.5:1.2tb:10k`    |
| Server    | `server:{mfg}:{model}:{form}`              | `server:dell:r740xd:sff`     |
| CPU       | `cpu:{mfg}:{family}:{model}`               | `cpu:intel:xeon:gold_6130`    |
| NIC       | `nic:{speed}:{ports}:{type}`               | `nic:10gbe:2port:sfp+`       |

## Alert Flow

1. New listing ingested → LLM extracts attributes → stored
2. Product key generated from attributes
3. Price compared against `price_baselines` for that key
4. Composite score computed
5. Score checked against all matching `watches`
6. If score >= threshold AND filters match → create alert → notify

## Notification Targets

- **ntfy.sh** — dead simple, self-hostable, perfect for homelab
- Webhook (generic) — for future integrations
- Could add email/Discord later

## API / CLI

Minimal management interface:

```
tracker watch add --name "R640 RAM" --query "32GB DDR4 ECC R640" --component ram --threshold 80
tracker watch list
tracker watch remove <id>
tracker listings --component ram --min-score 70 --limit 20
tracker baselines --component ram
tracker run  # manual trigger
```

## Cron / Scheduling

- Ingestion: every 15 min per watch (staggered)
- Baseline refresh: every 6 hours
- Alert evaluation: triggered post-ingestion

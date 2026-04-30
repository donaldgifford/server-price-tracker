# SQL Helpers

Reference catalog of ad-hoc Postgres queries used to diagnose, observe, and
backfill `server-price-tracker`. Run these with `psql` against the `spt`
database (port-forward via `kubectl port-forward svc/server-price-tracker-db-rw 5433:5432`,
password from `kubectl get secret server-price-tracker-db-app -o jsonpath='{.data.password}' | base64 -d`).

> All queries assume the schema in `migrations/`. Update them when columns
> change; the listings table uses `first_seen_at` and `updated_at` (no
> `created_at`).

---

## Listings

### Count active vs. extracted

```sql
SELECT
  COUNT(*)                                                  AS total,
  COUNT(*) FILTER (WHERE active)                            AS active,
  COUNT(*) FILTER (WHERE component_type IS NULL)            AS unextracted,
  COUNT(*) FILTER (WHERE component_type IS NULL AND active) AS unextracted_active
FROM listings;
```

### Find active listings the LLM has never successfully extracted

```sql
SELECT id, title, first_seen_at, updated_at
FROM listings
WHERE component_type IS NULL AND active = true
ORDER BY first_seen_at DESC;
```

### Soft-deactivate a single listing (e.g., misclassified accessory)

```sql
UPDATE listings SET active = false WHERE id = '<uuid>';
```

### Backfill misclassified accessories (DESIGN-0011 / IMPL-0016 Phase 3)

The regex pre-classifier (`pkg/extract/preclassify.go`) only affects new
ingestions. To clean up historical rows the LLM already mis-bucketed as
`server` / `drive` / `ram`, run the UPDATE below after deploying the new
image so subsequent rescores pick up the corrected `component_type`.

Postgres uses `\y` for word boundaries in POSIX regex (not `\b`), and the
patterns mirror `accessoryPatterns` and `primaryComponentPatterns` in
`preclassify.go`. The `RETURNING` clause makes the change auditable.

```sql
UPDATE listings
SET component_type = 'other',
    extraction_confidence = 0.95,
    updated_at = now()
WHERE active = true
  AND (
    title ~* '\ybackplane\y'
    OR title ~* '\y(drive\s+)?(caddy|caddies|tray|trays|sled|sleds)\y'
    OR title ~* '\yrails?\y'
    OR title ~* '\ybezels?\y'
    OR title ~* '\y(mounting\s+)?brackets?\y'
    OR title ~* '\yrisers?\y'
    OR title ~* '\yheat[\s-]?sinks?\y'
    OR title ~* '\yfan\s+(assembly|kit|tray|module)\y'
    OR title ~* '\ycable\y'
    OR title ~* '\ygpu\s+riser\y'
  )
  AND title !~* '\yddr[2345]\y'
  AND title !~* '\y(rdimm|udimm|lrdimm|fbdimm|sodimm)\y'
  AND title !~* '\y(nvme|sas|sata|scsi)\y'
  AND title !~* '\yssd\y'
  AND title !~* '\yhdd\y'
  AND title !~* '\y(xeon|epyc|opteron|threadripper)\y'
  AND title !~* '\y(\d+gb|\d+tb)\y'
  AND title !~* '\y\d+u\y'
RETURNING id, component_type, title;
```

After the UPDATE, trigger `POST /api/v1/rescore` so the rescore reads the
corrected component types and recomputes scores against the right
baselines.

### See all listings of one type

```sql
SELECT id, title, score, price_cents / 100.0 AS price, updated_at
FROM listings
WHERE component_type = 'ram' AND active = true
ORDER BY score DESC NULLS LAST
LIMIT 25;
```

---

## Extraction Queue

The queue has no `status` column; state is derived from `claimed_at`,
`completed_at`, `error_text`.

### Queue health snapshot

```sql
SELECT
  COUNT(*) FILTER (WHERE completed_at IS NULL AND claimed_at IS NULL)     AS pending,
  COUNT(*) FILTER (WHERE completed_at IS NULL AND claimed_at IS NOT NULL) AS in_flight,
  COUNT(*) FILTER (WHERE completed_at IS NOT NULL AND error_text IS NULL) AS done_ok,
  COUNT(*) FILTER (WHERE completed_at IS NOT NULL AND error_text IS NOT NULL) AS done_err,
  MAX(attempts)                                                            AS max_attempts
FROM extraction_queue;
```

### Recent failures and what blocked them

```sql
SELECT l.title, q.error_text, q.completed_at
FROM extraction_queue q
JOIN listings l ON l.id = q.listing_id
WHERE q.error_text IS NOT NULL
ORDER BY q.completed_at DESC
LIMIT 20;
```

### Re-enqueue all active listings stuck at `component_type IS NULL`

The `/api/v1/reextract` endpoint only handles listings whose
`component_type IS NOT NULL` (quality issues). For truly unextracted
listings, push them onto the queue manually:

```sql
INSERT INTO extraction_queue (listing_id, priority)
SELECT id, 1
FROM listings
WHERE component_type IS NULL AND active = true
ON CONFLICT DO NOTHING;
```

The unique partial index `extraction_queue_listing_pending` makes this
idempotent — listings already pending will not be duplicated.

### Watch a backfill drain

After enqueueing, count rows that are still NULL after processing
(succeeded ones drop out of the join, so a falling count indicates
progress):

```sql
SELECT
  COUNT(*) FILTER (WHERE completed_at IS NULL) AS pending,
  COUNT(*) FILTER (WHERE completed_at IS NOT NULL AND error_text IS NULL) AS done_ok,
  COUNT(*) FILTER (WHERE completed_at IS NOT NULL AND error_text IS NOT NULL) AS done_err
FROM extraction_queue
WHERE listing_id IN (
  SELECT id FROM listings WHERE component_type IS NULL AND active = true
);
```

---

## Schema Migrations

### Inspect applied migrations

```sql
SELECT version, applied_at FROM schema_migrations ORDER BY version;
```

### Record a migration that was applied out-of-band

When a migration is run manually (e.g., via psql) outside the app's
embedded migrator, register it so the app's startup check sees it:

```sql
INSERT INTO schema_migrations (version)
VALUES ('008_listing_active_flag.sql')
ON CONFLICT DO NOTHING;
```

---

## Baselines

### Baseline coverage by product key

```sql
SELECT product_key, sample_size, p50_price_cents, updated_at
FROM price_baselines
ORDER BY sample_size DESC
LIMIT 20;
```

### Cold (low-sample) baselines

```sql
SELECT product_key, sample_size, updated_at
FROM price_baselines
WHERE sample_size < 5
ORDER BY updated_at DESC;
```

---

## Alerts

### Alerts fired in the last day

```sql
SELECT a.fired_at, l.title, l.score, w.name AS watch
FROM alerts a
JOIN listings l ON l.id = a.listing_id
JOIN watches w  ON w.id = a.watch_id
WHERE a.fired_at > NOW() - INTERVAL '1 day'
ORDER BY a.fired_at DESC;
```

---

## System State (read-only)

The `system_state` view aggregates counts used by `/api/v1/system/state`.
Recreated in migrations when filters change — never query the table; query the view.

```sql
SELECT * FROM system_state;
```

---

## Tips

- **Always filter by `active = true`** for operational queries. Inactive
  listings are kept for history but should not appear in current-state
  aggregates.
- **Migrations vs. ad-hoc DDL**: prefer adding a new file in `migrations/`
  and running `make migrate` over running raw DDL. If you must run DDL
  manually, follow with the `INSERT INTO schema_migrations` snippet above.
- **Queue completed != succeeded**: `completed_at IS NOT NULL` only means
  "the worker is done with it." Check `error_text` for actual outcome.

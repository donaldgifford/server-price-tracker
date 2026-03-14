---
id: DESIGN-0004
title: "Inactive Listings Lifecycle"
status: Implemented
author: Donald Gifford
created: 2026-03-14
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0004: Inactive Listings Lifecycle

**Status:** Implemented
**Author:** Donald Gifford
**Date:** 2026-03-14

## Overview

Add an `active` boolean column to listings so stale, unextracted listings can
be marked inactive without deleting data. Inactive listings are excluded from
system state metrics, baselines, scoring, and alert evaluation but remain
queryable for historical analysis. Listings automatically reactivate if eBay
returns them again during ingestion.

## Goals and Non-Goals

### Goals

- Clean up system state metrics by excluding ~2362 stale unextracted listings
- Improve baseline accuracy by excluding stale prices from computation
- Preserve historical data for potential future use (Marketplace Insights API)
- Auto-reactivate listings that reappear in eBay search results
- Provide a one-time migration to deactivate existing stale listings

### Non-Goals

- Automatic staleness detection / scheduled deactivation (future work)
- Separate "sold" or "expired" status tracking (requires Marketplace Insights)
- UI or CLI commands to manually toggle listing active state (can add later)
- Purging or archiving old inactive listings

## Background

The eBay Browse API returns only active listings. During early ingestion cycles,
extraction workers failed on many listings (Ollama timeouts, model not loaded),
leaving ~2362 listings with `component_type IS NULL`. These listings:

1. **Inflate metrics** — `listings_total`, `listings_unextracted`, and
   `listings_unscored` all include them, making system health look worse
2. **Starve baselines** — without `product_key` they can't contribute to
   baselines, and their presence in total counts deflates coverage percentages
3. **Are likely expired** — they no longer appear in eBay search results (the
   ingestion loop doesn't re-fetch them), so their prices are stale
4. **Cannot be re-extracted usefully** — the `reextract` endpoint only handles
   listings where `component_type IS NOT NULL` (quality issues), and the
   ingestion endpoint only enqueues listings eBay returns

The extraction queue is empty (`extraction_queue_depth: 0`) and these listings
are stuck permanently.

## Detailed Design

### Column Addition

Add `active BOOLEAN NOT NULL DEFAULT true` to the `listings` table. All
existing listings start as active; the migration then sets stale ones inactive.

### Deactivation Criteria (Migration)

The one-time migration marks listings inactive where:

```sql
UPDATE listings SET active = false
WHERE component_type IS NULL OR component_type = '';
```

This targets the ~2362 unextracted listings. Extracted listings remain active
regardless of age, since their data contributes to baselines.

### Reactivation on Upsert

The `queryUpsertListing` ON CONFLICT clause adds `active = true` so any
listing that reappears in eBay results is automatically reactivated:

```sql
ON CONFLICT (ebay_item_id) DO UPDATE SET
    ...existing fields...,
    active = true,
    updated_at = now()
```

### Query Filter Changes

All operational queries filter on `active = true`:

| Query | Change |
|-------|--------|
| `system_state` view | All listing counts add `WHERE active = true` |
| `recompute_baseline` function | Add `AND active = true` to WHERE clause |
| `queryListListingsCursor` | Add `WHERE active = true AND id > $1` |
| `queryListUnextractedListings` | Add `AND active = true` |
| `queryListUnscoredListings` | Add `AND active = true` |
| `queryListDistinctProductKeys` | Add `AND active = true` |
| `queryListIncompleteExtractions` | Add `AND active = true` |
| `queryListIncompleteExtractionsForType` | Add `AND active = true` |
| `listings_with_baseline` view | Add `WHERE active = true` |

### Queries NOT Filtered

Direct lookups remain unfiltered so inactive listings are still accessible:

| Query | Reason |
|-------|--------|
| `queryGetListingByID` | Direct lookup — may need inactive listing data |
| `queryGetListingByEbayID` | Direct lookup — used by upsert flow |

### Index

```sql
CREATE INDEX idx_listings_active ON listings(active) WHERE active = true;
```

Partial index covers the common case (active listings) efficiently.

## API / Interface Changes

### Domain Type

Add `Active bool` field to `domain.Listing` struct. Scan it in all listing
queries. Default to `true` when not present in scan results.

### No New Endpoints

No new API endpoints needed for MVP. Future work could add:
- `PATCH /api/v1/listings/:id/active` — toggle active state
- `GET /api/v1/listings?active=false` — query inactive listings
- `POST /api/v1/listings/deactivate-stale` — batch deactivation

## Data Model

### Schema Change

```sql
ALTER TABLE listings ADD COLUMN active BOOLEAN NOT NULL DEFAULT true;
CREATE INDEX idx_listings_active ON listings(active) WHERE active = true;
```

### Migration Data Update

```sql
UPDATE listings SET active = false
WHERE component_type IS NULL OR component_type = '';
```

## Testing Strategy

- **Unit tests**: Update mock expectations for `Active` field in listing scans
- **Store tests**: Verify upsert reactivates inactive listing (integration)
- **No new unit tests needed**: The `active` filter is SQL-level; Go tests mock
  the store. The migration is tested by running it against the dev database.
- **Verify via system state**: After migration, `listings_unextracted` and
  `listings_unscored` should drop to near-zero; `listings_total` should reflect
  only active listings

## Migration / Rollout Plan

### Migration 008: `008_listing_active_flag.sql`

1. Add column with default
2. Create partial index
3. Mark stale listings inactive
4. Recreate `system_state` view with active filters
5. Recreate `recompute_baseline` function with active filter
6. Recreate `listings_with_baseline` view with active filter

### Rollout Steps

1. Apply migration (via `make migrate` or direct SQL)
2. Verify system state shows reduced counts
3. Trigger baseline refresh (`POST /api/v1/baselines/refresh`)
4. Trigger rescore (`POST /api/v1/rescore`)
5. Verify baseline coverage improved

### Rollback

Set all listings back to active:

```sql
UPDATE listings SET active = true;
```

Then revert views/functions to remove the `active` filter.

## Open Questions

- Should we add a scheduled job to auto-deactivate listings not seen in N days?
- Should the API expose active/inactive filtering for listing queries?
- When Marketplace Insights API becomes available, should we reactivate old
  listings to backfill sold data?

## References

- DESIGN-0001: Server Price Tracker Architecture
- Migration 007: Baseline active price proxy
- eBay Browse API limitations (active listings only)

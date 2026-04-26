---
id: DESIGN-0008
title: "Add spt watches update CLI command"
status: Draft
author: Donald Gifford
created: 2026-04-25
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0008: Add spt watches update CLI command

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-25

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Command shape](#command-shape)
  - [Implementation outline](#implementation-outline)
  - [Why GET-then-PUT (vs PATCH)](#why-get-then-put-vs-patch)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add an `spt watches update <id>` CLI subcommand so operators can change
watch fields (especially `--filter` and `--threshold`) without dropping
to SQL or hand-crafting `curl PUT` payloads. Today the CLI only supports
`create / list / get / enable / disable / delete`, but the API already
exposes `PUT /api/v1/watches/{id}` with full update semantics — this is
purely a client-side gap.

## Goals and Non-Goals

### Goals

- One-shot CLI command to change any updatable watch field: name,
  search_query, category_id, component_type, filters, score_threshold,
  enabled.
- Reuse the existing `handlers.ParseFilters` parser so the `--filter`
  flag syntax matches `spt watches create`.
- Default behavior: only fields the user explicitly passed are changed
  (partial update). Other fields stay as they were.
- Predictable filter semantics: `--filter` flags **replace** the
  existing `attribute_filters` map by default; `--add-filter` flags
  merge into the existing map.

### Non-Goals

- Bulk updates (e.g., "set threshold = 70 on all RAM watches"). Out of
  scope; if needed later, add `spt watches update --where component=ram`.
- Renaming the watch ID. ID is server-generated and immutable.
- Schema changes to `watches` table or `WatchFilters` struct.
- Changes to `PUT /api/v1/watches/{id}` server semantics.

## Background

The original CLI surface (DESIGN-0001 / IMPL-0001) covered the watch
lifecycle but skipped `update` because watches were expected to be
mostly create-once. In practice, baseline tuning, filter refinement,
and threshold tweaks happen often — see PR #44 cleanup, where five RAM
watches needed `capacity_gb` filters added to stop a single 128GB
listing from firing alerts on the 16/32/64/128 GB watches.

Today the workaround is direct SQL on the `filters` JSONB column:

```sql
UPDATE watches SET filters = '{"attribute_filters": {"capacity_gb": {"eq": 32}}}'::jsonb
WHERE name = 'DDR4 ECC 32GB';
```

That works but: (a) requires DB access, (b) bypasses any server-side
validation, (c) is easy to typo on JSONB syntax. A CLI command closes
that gap.

## Detailed Design

### Command shape

```bash
spt watches update <id> \
  [--name "..."] \
  [--query "..."] \
  [--category "..."] \
  [--component ram|drive|server|cpu|nic|other] \
  [--threshold N] \
  [--enabled true|false] \
  [--filter "key=value" ...]      # replaces existing attribute_filters
  [--add-filter "key=value" ...]  # merges into existing attribute_filters
  [--clear-filters]               # explicit: empties attribute_filters
```

### Implementation outline

1. New file `cmd/spt/cmd/watches_update.go` (or extend `watches.go`).
2. The command:
   - Calls `client.GetWatch(ctx, id)` to fetch current state.
   - Applies any `--name / --query / --category / --component /
     --threshold / --enabled` flags that were explicitly set (use
     Cobra's `Changed()` to detect explicit-vs-default).
   - For filters:
     - If `--clear-filters` → set `Filters = WatchFilters{}`.
     - Else if any `--filter` was passed → parse with
       `handlers.ParseFilters`, **replace** entire `Filters` block.
     - Else if any `--add-filter` was passed → parse, then merge
       `AttributeFilters` map into the existing one (key-by-key
       overwrite).
     - Else leave filters untouched.
   - Calls `client.UpdateWatch(ctx, id, updated)` which `PUT`s the full
     body to `/api/v1/watches/{id}`.
3. The HTTP client method `UpdateWatch` already exists or follows the
   same pattern as `CreateWatch` in `internal/api/client/`.

### Why GET-then-PUT (vs PATCH)

The server endpoint is `PUT /api/v1/watches/{id}` and the input struct
uses `omitempty` on every field, which makes partial updates
ambiguous from the wire (e.g., `Enabled: false` looks identical to
"not set"). Adding a PATCH endpoint with explicit `*bool` /
`*string` types is doable but bigger scope. GET-then-PUT keeps the
server unchanged and makes the merge logic explicit on the CLI side
where Cobra already gives us "was this flag set?".

The race window (someone else updates between our GET and PUT) is
acceptable for an interactive admin tool — the next `get` shows the
final state.

## API / Interface Changes

- **Server:** none. `PUT /api/v1/watches/{id}` is unchanged.
- **CLI:** new subcommand `spt watches update <id>` with the flags
  listed above.
- **Client SDK** (`internal/api/client/`): add `UpdateWatch(ctx, id,
  *domain.Watch) error` if not already present.

## Data Model

No schema changes. `watches.filters` is already `JSONB NOT NULL DEFAULT
'{}'` and accommodates any `WatchFilters` payload.

## Testing Strategy

- **Unit (CLI)**: table-driven test of the merge/replace logic for
  filters — covers each of `--filter`, `--add-filter`, `--clear-filters`,
  and the no-filter-flag (preserve existing) case.
- **Unit (client)**: existing watch-handler tests cover `UpdateWatch`
  HTTP plumbing; add one client-side test that round-trips a partial
  update.
- **No new server-side tests** since the server contract is unchanged.

## Migration / Rollout Plan

- New CLI subcommand; backwards-compatible with all existing
  invocations.
- No feature flag needed — purely additive.
- Doc update: `docs/OPERATIONS.md` "Watch Management" section gains an
  update example; `USAGE.md` references the new command.

## Open Questions

1. **Filter semantics default — replace vs merge?** Current proposal:
   `--filter` replaces, `--add-filter` merges. Alternative: invert
   (merge by default, `--replace-filters` to wipe). Replace-by-default
   matches the `create` mental model and avoids surprise growth of
   the filter set; merge requires opt-in.
2. **`--enabled` flag interaction with `enable / disable` subcommands?**
   Both paths exist; `update --enabled false` would duplicate
   `disable`. Probably fine to leave both — one is more focused, the
   other is bundled. Document the overlap.
3. **`--clear-filters` vs `--filter ""`?** Empty string would be
   ambiguous; prefer the explicit `--clear-filters` boolean flag.
4. **Should `update` also output the new watch JSON (like `get`)?**
   Probably yes for confirmation; mirrors the existing pattern in
   `create`.

## References

- API endpoint: `internal/api/handlers/watches.go` (`UpdateWatch`,
  `UpdateWatchInput`).
- CLI starting point: `cmd/spt/cmd/watches.go`.
- Filter parser: `internal/api/handlers/filters.go` (`ParseFilters`).
- Original need: PR #44 cleanup where 5 RAM watches needed
  `capacity_gb` filters added.
- Related doc: `docs/SQL_HELPERS.md` — current SQL workaround.

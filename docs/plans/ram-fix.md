# Extraction Quality: PC4 Speed Normalization + Re-Extract Endpoint

## Context

RAM product keys have `speed_mhz: 0` for most listings (~2700 in the DB).
Root cause: the eBay Browse API `ItemSummary` doesn't include item specifics,
so extraction only sees the title. Many eBay RAM titles encode speed as PC4
module numbers (e.g., "PC4-21300" = 2666 MHz) which Mistral 7B can't reliably
convert. There's also no way to re-run extraction on existing listings.

This plan adds: a PC4 lookup table, an improved prompt, a post-extraction
normalization fallback, a re-extract endpoint/CLI, and extraction quality
metrics.

## Phase 1: PC4 Reference Table

**New file: `pkg/extract/pc4.go`**

- `var pc4SpeedTable map[string]int` — PC3/PC4/PC5 bandwidth → MHz:
  - PC3-10600→1333, PC3-12800→1600, PC3-14900→1866
  - PC4-17000→2133, PC4-19200→2400, PC4-21300→2666, PC4-23400→2933, PC4-25600→3200
  - PC5-38400→4800, PC5-44800→5600, PC5-51200→6400
- `PC4ToMHz(moduleNumber string) (int, bool)` — normalize input, strip prefix, look up speed
- `ExtractSpeedFromTitle(title string) (int, bool)` — regex `(?i)\bPC[345]-(\d{5,6})[A-Z]?\b` to find PC module numbers in title, return MHz via table

**New file: `pkg/extract/pc4_test.go`**

Table-driven tests: all table entries, suffix handling (PC4-21300V), case
insensitivity, unknown numbers, title extraction with real eBay title strings.

## Phase 2: Improve RAM Prompt

**Modify: `pkg/extract/prompts.go`**

Add a rule to `ramTmpl` in the Rules section:

```text
- "speed_mhz": Convert PC module numbers to MHz. Common mappings:
  PC3-10600=1333, PC3-12800=1600, PC3-14900=1866,
  PC4-17000=2133, PC4-19200=2400, PC4-21300=2666, PC4-23400=2933, PC4-25600=3200,
  PC5-38400=4800, PC5-44800=5600, PC5-51200=6400.
  Ignore any letter suffix (V, R, T, U, E). Always convert when present.
```

## Phase 3: Post-Extraction Normalization

**Add to: `pkg/extract/pc4.go`**

```go
// NormalizeRAMSpeed fills in speed_mhz from PC module numbers in the title
// when the LLM returned null/0. Modifies attrs in place.
func NormalizeRAMSpeed(title string, attrs map[string]any) bool
```

- If `speed_mhz` already set and non-zero → return true
- Call `ExtractSpeedFromTitle(title)` → if found, set `attrs["speed_mhz"]`
- Return whether speed is now present

**Modify: `pkg/extract/extractor.go`** — in `Extract()`, after `ValidateExtraction`
succeeds:

```go
if componentType == domain.ComponentRAM {
    NormalizeRAMSpeed(title, attrs)
}
```

Tests in `pc4_test.go`: NormalizeRAMSpeed with various title/attrs combos.

## Phase 4: Re-Extract Endpoint

### 4a. Store

**Modify: `internal/store/store.go`** — add to `Store` interface:

```go
ListIncompleteExtractions(ctx context.Context, componentType string, limit int) ([]domain.Listing, error)
CountIncompleteExtractions(ctx context.Context) (int, error)
CountIncompleteExtractionsByType(ctx context.Context) (map[string]int, error)
```

**Modify: `internal/store/queries.go`** — new SQL constants:

- `queryListIncompleteExtractions` — `WHERE component_type IS NOT NULL AND`
  RAM-specific: `(component_type = 'ram' AND product_key LIKE '%:0')`
  Extensible with `OR` for other types later. Optional `$1` for component type
  filter, `$2` for limit.
- `queryCountIncompleteExtractions` — count version
- `queryCountIncompleteExtractionsByType` — grouped by component_type

**Modify: `internal/store/postgres.go`** — implement the three methods.

### 4b. Engine

**Modify: `internal/engine/engine.go`** — add:

```go
func (eng *Engine) RunReExtraction(ctx context.Context, componentType string, limit int) (int, error)
```

Flow: query incomplete listings → for each: ClassifyAndExtract → ProductKey →
UpdateListingExtraction → ScoreListing. Log errors, continue, return success count.
Default limit 100.

### 4c. Handler

**New file: `internal/api/handlers/reextract.go`**

- `ReExtractor` interface: `RunReExtraction(ctx, componentType, limit) (int, error)`
- `ReExtractHandler` struct
- `ReExtractInput` with optional `component_type` and `limit` body fields
- `ReExtractOutput` with `re_extracted` count
- `POST /api/v1/reextract`, operation ID `reextract-listings`, tag `extract`

**New file: `internal/api/handlers/reextract_test.go`**

### 4d. Route registration

**Modify: `cmd/server-price-tracker/cmd/serve.go`** — in `registerRoutes`,
inside the `if eng != nil` block:

```go
reextractH := handlers.NewReExtractHandler(eng)
handlers.RegisterReExtractRoutes(humaAPI, reextractH)
```

### 4e. API Client

**Modify: `internal/api/client/listings.go`** — add `ReExtract(ctx, componentType, limit) (int, error)`.

### 4f. CLI Command

**New file: `cmd/spt/cmd/reextract.go`**

```text
spt reextract [--type ram] [--limit 50]
```

Follow rescore.go pattern. Register in root.go.

### 4g. Mocks

Run `make mocks` after Store interface changes.

## Phase 5: Extraction Quality Metrics

**Modify: `internal/metrics/metrics.go`** — add:

```go
ListingsIncompleteExtraction     // Gauge
ListingsIncompleteExtractionByType // GaugeVec, label: component_type
```

**Modify: `internal/engine/engine.go`** — in `SyncStateMetrics`, add calls to
`CountIncompleteExtractions` and `CountIncompleteExtractionsByType`.

**New file: `internal/api/handlers/extraction_stats.go`** — `GET /api/v1/extraction/stats`:

```json
{"total_incomplete": 42, "by_type": {"ram": 38, "drive": 4}}
```

Register in serve.go. Add CLI `spt extraction-stats` command (optional).

## Phase 6 (Optional): Scheduled Re-Extraction

Add optional cron job in scheduler.go, gated by
`cfg.Schedule.ReExtractionInterval`. Calls `eng.RunReExtraction(ctx, "", 100)`.
Add config field to `internal/config/config.go`.

## File Summary

| File | Action |
|------|--------|
| `pkg/extract/pc4.go` | New — PC4 table, ExtractSpeedFromTitle, NormalizeRAMSpeed |
| `pkg/extract/pc4_test.go` | New — table-driven tests |
| `pkg/extract/prompts.go` | Modify — add PC4 rule to RAM prompt |
| `pkg/extract/extractor.go` | Modify — call NormalizeRAMSpeed after validation |
| `internal/store/store.go` | Modify — add 3 interface methods |
| `internal/store/queries.go` | Modify — add 3 SQL constants |
| `internal/store/postgres.go` | Modify — implement 3 methods |
| `internal/engine/engine.go` | Modify — add RunReExtraction, extend SyncStateMetrics |
| `internal/api/handlers/reextract.go` | New — POST /api/v1/reextract handler |
| `internal/api/handlers/reextract_test.go` | New — handler tests |
| `internal/api/handlers/extraction_stats.go` | New — GET /api/v1/extraction/stats |
| `internal/api/client/listings.go` | Modify — add ReExtract method |
| `cmd/spt/cmd/reextract.go` | New — CLI command |
| `cmd/spt/cmd/root.go` | Modify — register reextract command |
| `cmd/server-price-tracker/cmd/serve.go` | Modify — register routes |
| `internal/metrics/metrics.go` | Modify — add incomplete extraction metrics |

## Verification

```bash
# Phase 1-3: Unit tests
go test ./pkg/extract/... -v -run TestPC4
go test ./pkg/extract/... -v -run TestNormalize

# Phase 4: Build and test re-extract
make build
spt reextract --type ram --limit 10

# Phase 5: Check metrics
curl -s localhost:8080/metrics | grep incomplete_extraction
curl -s localhost:8080/api/v1/extraction/stats

# Full suite
make test && make lint
```

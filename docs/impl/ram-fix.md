# Implementation: PC4 Speed Normalization, Re-Extract Endpoint, and Extraction Quality Metrics

## Context

RAM product keys have `speed_mhz: 0` for most listings (~2700 in the DB).
The root cause is threefold:

1. The eBay Browse API `ItemSummary` does not include item specifics, so
   the LLM only sees the listing title during ingestion
   (`engine.go:234` passes `nil` for item specifics).
2. Many eBay RAM titles encode speed as PC module numbers (e.g.,
   "PC4-21300" = 2666 MHz) which Mistral 7B cannot reliably convert —
   the RAM prompt at `prompts.go:42` gives examples like "2133, 2400"
   but does not explain PC4 bandwidth-to-MHz mappings.
3. There is no re-extraction mechanism. Once a listing is extracted with
   `speed_mhz: null`, it stays that way forever. Only `RescoreAll` exists
   for re-scoring; no equivalent for extraction.

The `speed_mhz` field is optional in both the prompt schema
(`prompts.go:42`) and validation (`validate.go:98-103`). When null,
`pkInt(attrs, "speed_mhz")` in `productkey.go:17` returns `0`, producing
keys like `ram:ddr4:ecc_reg:32gb:0`. This fragments baselines — DDR4
32GB ECC REG RAM at 2400, 2666, and "unknown" (0) all get different
product keys and separate baselines.

See `docs/plans/ram-fix.md` for the high-level design.

---

## Phase 1: PC4-to-MHz Reference Table and Lookup Functions

### Tasks

- [x] Create `pkg/extract/pc4.go` with:
  - [x] `var pc4SpeedTable` — unexported `map[string]int` mapping PC module
    bandwidth numbers to MHz speeds. Include all common server RAM variants:
    - DDR3: `10600`→1333, `12800`→1600, `14900`→1866
    - DDR4: `17000`→2133, `19200`→2400, `21300`→2666, `23400`→2933, `25600`→3200
    - DDR5: `38400`→4800, `44800`→5600, `51200`→6400
  - [x] `func PC4ToMHz(moduleNumber string) (int, bool)` — exported function
    that normalizes input (case-insensitive, strips `PC3-`/`PC4-`/`PC5-`
    prefix, strips trailing letter suffixes like V, R, T, U, E), extracts
    the numeric bandwidth portion, and looks it up in `pc4SpeedTable`.
    Return `(mhz, true)` on match, `(0, false)` on miss.
  - [x] `var pc4Regex` — compiled `regexp.Regexp` for
    `(?i)\bPC[345]-?(\d{5,6})[A-Z]?\b` to match PC module numbers in
    free-form title text.
  - [x] `var ddrSpeedRegex` — compiled `regexp.Regexp` for
    `(?i)\bDDR[345]-?(\d{4})\b` to match DDR speed designations like
    `DDR4-2666`, `DDR4 2666`, `DDR5-4800` in title text. This is the
    fallback when no PC module number is found.
  - [x] `func ExtractSpeedFromTitle(title string) (int, bool)` — exported
    function that first applies `pc4Regex` to the title, extracts the
    captured bandwidth number, and calls `PC4ToMHz` to convert. If no
    PC match, falls back to `ddrSpeedRegex` to extract the MHz speed
    directly (validate it's in the 800-8400 range). Returns
    `(mhz, true)` on match, `(0, false)` on miss. If multiple matches,
    take the first.
- [x] Create `pkg/extract/pc4_test.go` with table-driven tests:
  - [x] `TestPC4ToMHz` — test cases:
    - All DDR3 entries: `"PC3-10600"` → 1333, `"PC3-12800"` → 1600, `"PC3-14900"` → 1866
    - All DDR4 entries: `"PC4-17000"` → 2133, `"PC4-19200"` → 2400,
      `"PC4-21300"` → 2666, `"PC4-23400"` → 2933, `"PC4-25600"` → 3200
    - All DDR5 entries: `"PC5-38400"` → 4800, `"PC5-44800"` → 5600, `"PC5-51200"` → 6400
    - Suffix handling: `"PC4-21300V"` → 2666, `"PC4-19200R"` → 2400,
      `"PC4-25600T"` → 3200, `"PC4-17000U"` → 2133, `"PC4-25600E"` → 3200
    - Case insensitivity: `"pc4-21300"` → 2666, `"Pc4-19200"` → 2400
    - Missing prefix: `"21300"` → 2666 (if stripping prefix handles this),
      or `(0, false)` depending on implementation decision
    - Unknown bandwidth: `"PC4-99999"` → `(0, false)`
    - Empty string: `""` → `(0, false)`
    - Just prefix: `"PC4-"` → `(0, false)`
  - [x] `TestExtractSpeedFromTitle` — test cases:
    - `"Samsung 32GB DDR4 PC4-21300 ECC REG"` → 2666
    - `"Micron 16GB 2Rx4 PC4-2666V-RB1-11"` → 2666 (common Micron part numbering)
    - `"LOT OF 4 Samsung 32GB DDR4 ECC REG"` → `(0, false)` (no PC module number)
    - `"SK Hynix 64GB PC4-25600 DDR4-3200MHz"` → 3200
    - `"Samsung M393A4K40CB2-CTD 32GB PC4-21300"` → 2666
    - `"Kingston DDR3 8GB PC3-12800 1600MHz"` → 1600
    - `"Samsung DDR5 32GB PC5-38400"` → 4800
    - `"Server RAM 32GB"` → `(0, false)`
    - DDR speed fallback cases (no PC module number, but DDR speed present):
    - `"Hynix 32GB DDR4-2666 ECC RDIMM"` → 2666 (DDR speed regex)
    - `"Samsung 16GB DDR4-2400T ECC"` → 2400 (DDR speed with suffix)
    - `"Kingston 64GB DDR5-4800 ECC REG"` → 4800 (DDR5 speed)
    - `"Crucial DDR4 2933MHz 32GB"` → `(0, false)` (no PC or DDR-XXXX pattern,
      MHz alone is not matched — that's for the LLM)
- [x] Run `go test ./pkg/extract/... -v -run TestPC4`
- [x] Run `go test ./pkg/extract/... -v -run TestExtractSpeed`

### Success Criteria

- `PC4ToMHz` correctly converts all table entries including suffix variants
  and is case-insensitive
- `ExtractSpeedFromTitle` finds PC module numbers in realistic eBay listing
  titles and converts them to MHz, with DDR speed regex as fallback
- All tests pass, zero lint issues

### Files

- `pkg/extract/pc4.go` (new)
- `pkg/extract/pc4_test.go` (new)

---

## Phase 2: Improve RAM Extraction Prompt

### Tasks

- [x] Modify `pkg/extract/prompts.go` — update the `ramTmpl` const:
  - [x] Add a new rule after the existing `"quantity"` rule (line 29) and
    before the `"Only use null"` rule (line 30):
    ```
    - "speed_mhz": Convert PC module numbers to MHz speed. Common mappings:
      PC3-10600=1333, PC3-12800=1600, PC3-14900=1866,
      PC4-17000=2133, PC4-19200=2400, PC4-21300=2666, PC4-23400=2933, PC4-25600=3200,
      PC5-38400=4800, PC5-44800=5600, PC5-51200=6400.
      Ignore any letter suffix (V, R, T, U, E) after the number. Always convert when present.
    ```
  - [x] Update the `speed_mhz` schema line (line 42) to emphasize it should
    not be null when a PC module number is present:
    ```
    "speed_mhz": integer (e.g. 2133, 2400, 2666, 3200; derived from PC module number if present) | null,
    ```
- [x] Add test to `pkg/extract/prompts_test.go`:
  - [x] `TestRenderExtractPrompt_RAMContainsPC4Rules` — render a RAM
    extraction prompt and assert the output contains `"PC4-21300=2666"`
    and `"PC3-12800=1600"` to verify the mapping rules are present
- [x] Run `go test ./pkg/extract/... -v -run TestRenderExtract`

### Success Criteria

- The rendered RAM extraction prompt includes the full PC4-to-MHz
  conversion table as a rule the LLM can follow
- Existing extraction tests still pass (prompt change is additive)
- Zero lint issues

### Files

- `pkg/extract/prompts.go` (modify)
- `pkg/extract/prompts_test.go` (modify)

---

## Phase 3: Post-Extraction Speed Normalization

### Tasks

- [x] Add `NormalizeRAMSpeed` function to `pkg/extract/pc4.go`:
  ```go
  // NormalizeRAMSpeed fills in speed_mhz from PC module numbers in the
  // title when the LLM returned null or 0. Modifies attrs in place.
  // Returns true if speed_mhz is set (was already present or was recovered).
  func NormalizeRAMSpeed(title string, attrs map[string]any) bool
  ```
  - Check if `speed_mhz` is already set and non-zero in attrs using the
    same `float64`/`int` type-switch pattern from `pkInt` in
    `productkey.go:64-77`. If so, return `true` immediately.
  - Call `ExtractSpeedFromTitle(title)`. If found, set
    `attrs["speed_mhz"] = mhz` and return `true`.
  - Return `false` if speed could not be determined.
- [x] Modify `pkg/extract/extractor.go` — in the `Extract` method
  (line 100), after `ValidateExtraction` succeeds (line 130-134) and
  before the return on line 136, add:
  ```go
  // Normalize RAM speed from PC module numbers in title when LLM missed it.
  if componentType == domain.ComponentRAM {
      NormalizeRAMSpeed(title, attrs)
  }
  ```
  This runs after validation passes but before the caller computes the
  product key, ensuring the key includes the recovered speed.
- [x] Add normalization tests to `pkg/extract/pc4_test.go`:
  - [x] `TestNormalizeRAMSpeed` — table-driven test cases:
    - Title with PC4-21300, attrs missing `speed_mhz` → sets 2666, returns true
    - Title with PC4-25600V, attrs missing `speed_mhz` → sets 3200, returns true
    - Title with PC3-12800, attrs has `speed_mhz: nil` → sets 1600, returns true
    - Attrs already has `speed_mhz: 2400` → does not overwrite, returns true
    - Attrs has `speed_mhz: float64(2666)` → does not overwrite, returns true
    - Attrs has `speed_mhz: 0` → treated as unset, attempts extraction
    - Title has no PC module number, attrs missing `speed_mhz` → returns false
    - Title has no PC module number, attrs has `speed_mhz: 0` → returns false
- [x] Add extractor integration test to `pkg/extract/extractor_test.go`:
  - [x] `TestExtract_RAMNormalizesSpeed` — mock backend returns a valid RAM
    extraction with `speed_mhz: null`, title contains `PC4-21300`.
    Verify the returned attrs have `speed_mhz: 2666`. This confirms the
    normalization runs in the extraction pipeline.
- [x] Run `go test ./pkg/extract/... -v`
- [x] Run `make lint`

### Success Criteria

- `NormalizeRAMSpeed` correctly fills in speed from PC module numbers
  in the title when the LLM returns null/0
- Does not overwrite existing non-zero speed values
- The normalization is wired into the `Extract` pipeline so all new
  extractions benefit
- All existing extraction tests still pass

### Files

- `pkg/extract/pc4.go` (modify — add `NormalizeRAMSpeed`)
- `pkg/extract/extractor.go` (modify — add normalization call)
- `pkg/extract/pc4_test.go` (modify — add normalization tests)
- `pkg/extract/extractor_test.go` (modify — add pipeline test)

---

## Phase 4: Store Layer for Incomplete Extractions

### Tasks

- [ ] Modify `internal/store/store.go` — add three methods to the `Store`
  interface (in the `// Listings` section after `ListUnscoredListings`):
  ```go
  ListIncompleteExtractions(ctx context.Context, componentType string, limit int) ([]domain.Listing, error)
  CountIncompleteExtractions(ctx context.Context) (int, error)
  CountIncompleteExtractionsByType(ctx context.Context) (map[string]int, error)
  ```
- [ ] Modify `internal/store/queries.go` — add SQL constants in a new
  `// Extraction quality queries.` section after the Baseline queries:
  - [ ] `queryListIncompleteExtractions` — select listing columns (same as
    `queryListUnextractedListings` column list) with:
    ```sql
    WHERE component_type IS NOT NULL AND (
      (component_type = 'ram' AND (product_key LIKE '%:0' OR (attributes->>'speed_mhz') IS NULL))
      OR (component_type = 'drive' AND (product_key LIKE '%:unknown%'))
    )
    ORDER BY first_seen_at DESC
    LIMIT $1
    ```
    RAM: detects missing speed_mhz (product key ending `:0`).
    Drive: detects missing form_factor or type (product key containing `:unknown`).
  - [ ] `queryListIncompleteExtractionsForType` — same WHERE clause but
    add `AND component_type = $1` filter, limit as `$2`
  - [ ] `queryCountIncompleteExtractions` — `SELECT COUNT(*)` with same
    WHERE clause
  - [ ] `queryCountIncompleteExtractionsByType` —
    `SELECT component_type, COUNT(*) ... GROUP BY component_type`
- [ ] Modify `internal/store/postgres.go` — implement the three methods:
  - [ ] `ListIncompleteExtractions` — if `componentType == ""`, use
    `queryListIncompleteExtractions` with `limit` as `$1`. If
    `componentType != ""`, use `queryListIncompleteExtractionsForType`
    with `componentType` as `$1` and `limit` as `$2`. Use existing
    `queryListings` helper pattern from `postgres.go:511-532`.
  - [ ] `CountIncompleteExtractions` — follow the `CountUnextractedListings`
    pattern at `postgres.go:465-471`.
  - [ ] `CountIncompleteExtractionsByType` — query rows, scan
    `component_type` and `count` into a `map[string]int`. Close rows,
    check `rows.Err()`.
- [ ] Run `make mocks` to regenerate `internal/store/mocks/mock_store.go`
  with the three new methods
- [ ] Run `make test && make lint`

### Success Criteria

- `Store` interface has three new methods
- `PostgresStore` implements all three methods
- Mock store is regenerated and compiles
- All existing tests pass

### Files

- `internal/store/store.go` (modify)
- `internal/store/queries.go` (modify)
- `internal/store/postgres.go` (modify)
- `internal/store/mocks/mock_store.go` (regenerated)

---

## Phase 5: Re-Extract Engine Method

### Tasks

- [ ] Add `RunReExtraction` to `internal/engine/engine.go`:
  ```go
  // RunReExtraction re-extracts listings with incomplete extraction data.
  // Returns the count of successfully re-extracted listings.
  func (eng *Engine) RunReExtraction(ctx context.Context, componentType string, limit int) (int, error)
  ```
  Implementation:
  - Default limit to 100 if `limit <= 0`
  - Call `eng.store.ListIncompleteExtractions(ctx, componentType, limit)`
  - For each listing:
    1. Call `eng.extractor.ClassifyAndExtract(ctx, listing.Title, nil)`
    2. If extraction fails, log error and continue to next listing
    3. Compute `extract.ProductKey(string(ct), attrs)`
    4. Call `eng.store.UpdateListingExtraction(ctx, listing.ID, string(ct), attrs, 0.9, productKey)`
    5. Update listing in-place (`listing.ProductKey`, `listing.ComponentType`)
    6. Call `ScoreListing(ctx, eng.store, &listing)`
    7. Increment success counter
  - Log summary: `"re-extraction complete", "total", len(listings), "success", count, "failed", len(listings)-count`
  - Return `(count, nil)`
- [ ] Add test to `internal/engine/engine_test.go`:
  - [ ] `TestRunReExtraction_Success` — mock store returns 2 listings with
    `ProductKey: "ram:ddr4:ecc_reg:32gb:0"`. Mock extractor returns valid
    attrs with `speed_mhz: 2666`. Verify `UpdateListingExtraction` is
    called twice with the correct product key containing `2666`. Verify
    `GetBaseline` is called (from ScoreListing). Return `(2, nil)`.
  - [ ] `TestRunReExtraction_PartialFailure` — mock store returns 3 listings.
    Mock extractor returns success for listings 1 and 3, error for
    listing 2. Verify `UpdateListingExtraction` is called twice (not three
    times). Return `(2, nil)` — partial success without error.
  - [ ] `TestRunReExtraction_NoListings` — mock store returns empty slice.
    Verify no extraction calls. Return `(0, nil)`.
  - [ ] `TestRunReExtraction_DefaultLimit` — pass `limit=0`, verify
    `ListIncompleteExtractions` is called with `limit=100`.
  - [ ] Add `CountIncompleteExtractions` and `CountIncompleteExtractionsByType`
    to `expectCountMethods` helper (with `.Maybe()` + zero returns) so
    existing tests that call `SyncStateMetrics` don't break after Phase 6
    adds those calls.
- [ ] Run `go test ./internal/engine/... -v -run TestRunReExtraction`
- [ ] Run `make test && make lint`

### Success Criteria

- `RunReExtraction` processes listings in a loop, skips failures, and
  returns the success count
- Partial failures don't abort the entire batch
- Default limit is applied when 0 is passed
- All existing engine tests still pass

### Files

- `internal/engine/engine.go` (modify)
- `internal/engine/engine_test.go` (modify)

---

## Phase 6: Re-Extract API Handler

### Tasks

- [ ] Create `internal/api/handlers/reextract.go`:
  - [ ] Define `ReExtractor` interface:
    ```go
    type ReExtractor interface {
        RunReExtraction(ctx context.Context, componentType string, limit int) (int, error)
    }
    ```
  - [ ] Define `ReExtractHandler` struct with `reExtractor ReExtractor` field
  - [ ] Define `NewReExtractHandler(re ReExtractor) *ReExtractHandler` constructor
  - [ ] Define `ReExtractInput` with Huma body struct:
    ```go
    type ReExtractInput struct {
        Body struct {
            ComponentType string `json:"component_type,omitempty" doc:"Filter by component type (e.g., 'ram')" example:"ram"`
            Limit         int    `json:"limit,omitempty" doc:"Max listings to re-extract (default 100)" example:"100"`
        }
    }
    ```
  - [ ] Define `ReExtractOutput` with body struct:
    ```go
    type ReExtractOutput struct {
        Body struct {
            ReExtracted int `json:"re_extracted" example:"42" doc:"Number of listings successfully re-extracted"`
        }
    }
    ```
  - [ ] Implement `ReExtract(ctx, input) (*ReExtractOutput, error)` —
    calls `h.reExtractor.RunReExtraction(ctx, input.Body.ComponentType, input.Body.Limit)`,
    wraps errors with `huma.Error500InternalServerError`
  - [ ] Implement `RegisterReExtractRoutes(api huma.API, h *ReExtractHandler)`:
    - Operation ID: `"reextract-listings"`
    - Method: `POST`
    - Path: `/api/v1/reextract`
    - Summary: `"Re-extract listings with incomplete data"`
    - Description: `"Re-runs LLM extraction on listings with quality issues (e.g., missing RAM speed)."`
    - Tags: `[]string{"extract"}`
    - Errors: `[]int{http.StatusInternalServerError}`
- [ ] Create `internal/api/handlers/reextract_test.go`:
  - [ ] `TestReExtract_Success` — mock `ReExtractor` returning `(42, nil)`.
    POST to `/api/v1/reextract` with `{"component_type": "ram", "limit": 50}`.
    Assert 200 and body contains `"re_extracted":42`.
  - [ ] `TestReExtract_Error` — mock `ReExtractor` returning error. Assert 500.
  - [ ] `TestReExtract_EmptyBody` — POST with `{}`. Assert 200 (defaults
    should work — empty component_type means all, limit=0 means default).
- [ ] Modify `cmd/server-price-tracker/cmd/serve.go` — in `registerRoutes`,
  inside the `if eng != nil` block (after line 190), add:
  ```go
  reextractH := handlers.NewReExtractHandler(eng)
  handlers.RegisterReExtractRoutes(humaAPI, reextractH)
  ```
- [ ] Run `go test ./internal/api/handlers/... -v -run TestReExtract`
- [ ] Run `make test && make lint`

### Success Criteria

- `POST /api/v1/reextract` accepts optional `component_type` and `limit`,
  calls the engine, and returns `{"re_extracted": N}`
- Route is registered in `serve.go` only when the engine is available
- Handler tests pass with mock `ReExtractor`

### Files

- `internal/api/handlers/reextract.go` (new)
- `internal/api/handlers/reextract_test.go` (new)
- `cmd/server-price-tracker/cmd/serve.go` (modify)

---

## Phase 7: Re-Extract API Client and CLI Command

### Tasks

- [ ] Modify `internal/api/client/listings.go` — add:
  ```go
  // ReExtract triggers re-extraction of listings with incomplete data.
  func (c *Client) ReExtract(ctx context.Context, componentType string, limit int) (int, error) {
      body := map[string]any{}
      if componentType != "" {
          body["component_type"] = componentType
      }
      if limit > 0 {
          body["limit"] = limit
      }
      var resp struct {
          ReExtracted int `json:"re_extracted"`
      }
      if err := c.post(ctx, "/api/v1/reextract", body, &resp); err != nil {
          return 0, err
      }
      return resp.ReExtracted, nil
  }
  ```
- [ ] Create `cmd/spt/cmd/reextract.go`:
  ```go
  func reextractCmd() *cobra.Command {
      cmd := &cobra.Command{
          Use:   "reextract",
          Short: "Re-extract listings with incomplete data",
          Long:  "Re-runs LLM extraction on listings with quality issues\n" +
              "(e.g., missing RAM speed from PC module numbers).",
          Example: `  spt reextract
    spt reextract --type ram
    spt reextract --type ram --limit 50`,
          RunE: func(cmd *cobra.Command, _ []string) error {
              // Get flags
              // Call c.ReExtract(ctx, componentType, limit)
              // Print "Re-extracted N listings."
          },
      }
      cmd.Flags().String("type", "", "component type filter (e.g., ram, drive, cpu)")
      cmd.Flags().Int("limit", 0, "max listings to process (default 100)")
      return cmd
  }
  ```
  Follow the `rescoreCmd` pattern at `cmd/spt/cmd/rescore.go`.
- [ ] Modify `cmd/spt/cmd/root.go` — add `rootCmd.AddCommand(reextractCmd())`
  in `init()` at line 57.
- [ ] Run `make build` to verify both binaries compile
- [ ] Run `make lint`

### Success Criteria

- `spt reextract` calls `POST /api/v1/reextract` and prints the count
- `--type` and `--limit` flags work correctly
- Both binaries build without errors

### Files

- `internal/api/client/listings.go` (modify)
- `cmd/spt/cmd/reextract.go` (new)
- `cmd/spt/cmd/root.go` (modify)

---

## Phase 8: Extraction Quality Metrics

### Tasks

- [ ] Modify `internal/metrics/metrics.go` — add new metrics in a new
  `// Extraction quality metrics.` section after the existing
  `ExtractionFailuresTotal` in the `// Extraction metrics.` section:
  ```go
  ListingsIncompleteExtraction = promauto.NewGauge(prometheus.GaugeOpts{
      Namespace: namespace,
      Name:      "listings_incomplete_extraction",
      Help:      "Listings with incomplete extraction data (e.g., missing speed for RAM).",
  })

  ListingsIncompleteExtractionByType = promauto.NewGaugeVec(prometheus.GaugeOpts{
      Namespace: namespace,
      Name:      "listings_incomplete_extraction_by_type",
      Help:      "Listings with incomplete extraction data, by component type.",
  }, []string{"component_type"})
  ```
- [ ] Modify `internal/engine/engine.go` — in `SyncStateMetrics`
  (after the `CountProductKeysWithoutBaseline` block ending at line 374),
  add:
  ```go
  incomplete, err := eng.store.CountIncompleteExtractions(ctx)
  if err != nil {
      eng.log.Warn("failed to count incomplete extractions", "error", err)
  } else {
      metrics.ListingsIncompleteExtraction.Set(float64(incomplete))
  }

  byType, err := eng.store.CountIncompleteExtractionsByType(ctx)
  if err != nil {
      eng.log.Warn("failed to count incomplete extractions by type", "error", err)
  } else {
      for ct, count := range byType {
          metrics.ListingsIncompleteExtractionByType.WithLabelValues(ct).Set(float64(count))
      }
  }
  ```
- [ ] Update `expectCountMethods` helper in `internal/engine/engine_test.go`
  (if not already done in Phase 5) to include:
  ```go
  ms.EXPECT().CountIncompleteExtractions(mock.Anything).Return(0, nil).Maybe()
  ms.EXPECT().CountIncompleteExtractionsByType(mock.Anything).Return(nil, nil).Maybe()
  ```
- [ ] Run `make test && make lint`

### Success Criteria

- `spt_listings_incomplete_extraction` gauge is updated in
  `SyncStateMetrics` and visible at `/metrics`
- `spt_listings_incomplete_extraction_by_type` gauge vec is updated
  with per-component-type counts
- All existing engine tests pass (mock expectations added)

### Files

- `internal/metrics/metrics.go` (modify)
- `internal/engine/engine.go` (modify)
- `internal/engine/engine_test.go` (modify)

---

## Phase 9: Extraction Stats API Endpoint

### Tasks

- [ ] Create `internal/api/handlers/extraction_stats.go`:
  - [ ] Define `ExtractionStatsHandler` struct with `store store.Store` field
  - [ ] Define `NewExtractionStatsHandler(s store.Store)` constructor
  - [ ] Define `ExtractionStatsOutput` with body struct:
    ```go
    type ExtractionStatsOutput struct {
        Body struct {
            TotalIncomplete int            `json:"total_incomplete" example:"42" doc:"Total listings with incomplete extraction"`
            ByType          map[string]int `json:"by_type" doc:"Incomplete extraction count per component type"`
        }
    }
    ```
  - [ ] Implement `Stats(ctx, _ *struct{}) (*ExtractionStatsOutput, error)` —
    calls both `CountIncompleteExtractions` and `CountIncompleteExtractionsByType`
  - [ ] Implement `RegisterExtractionStatsRoutes(api, h)`:
    - Operation ID: `"extraction-stats"`
    - Method: `GET`
    - Path: `/api/v1/extraction/stats`
    - Summary: `"Get extraction quality statistics"`
    - Tags: `[]string{"extract"}`
- [ ] Create `internal/api/handlers/extraction_stats_test.go`:
  - [ ] `TestExtractionStats_Success` — mock store returns `42` total and
    `{"ram": 38, "drive": 4}` by type. Assert 200 and body matches.
  - [ ] `TestExtractionStats_Empty` — mock store returns 0 and empty map.
    Assert 200 and body has `total_incomplete: 0`.
- [ ] Modify `cmd/server-price-tracker/cmd/serve.go` — in `registerRoutes`,
  inside the `if s != nil` block (after baselines handler registration at
  line 171), add:
  ```go
  extractionStatsH := handlers.NewExtractionStatsHandler(s)
  handlers.RegisterExtractionStatsRoutes(humaAPI, extractionStatsH)
  ```
- [ ] Run `go test ./internal/api/handlers/... -v -run TestExtractionStats`
- [ ] Run `make test && make lint`

### Success Criteria

- `GET /api/v1/extraction/stats` returns total incomplete count and
  per-type breakdown
- Endpoint is store-dependent (registered only when store is available)
- Handler tests pass

### Files

- `internal/api/handlers/extraction_stats.go` (new)
- `internal/api/handlers/extraction_stats_test.go` (new)
- `cmd/server-price-tracker/cmd/serve.go` (modify)

---

## Phase 10 (Optional): Scheduled Periodic Re-Extraction

### Tasks

- [ ] Modify `internal/config/config.go`:
  - [ ] Add `ReExtractionInterval time.Duration` field to `ScheduleConfig`
    at line 122, with YAML tag `re_extraction_interval`
  - [ ] No default — zero value means disabled (opt-in only)
- [ ] Modify `internal/engine/scheduler.go`:
  - [ ] Add `reExtractionEntryID cron.EntryID` field to `Scheduler` struct
  - [ ] Add `reExtractionInterval time.Duration` as 4th parameter to
    `NewScheduler`:
    ```go
    func NewScheduler(
        eng *Engine,
        ingestionInterval time.Duration,
        baselineInterval time.Duration,
        reExtractionInterval time.Duration,
        log *slog.Logger,
    ) (*Scheduler, error)
    ```
  - [ ] If `reExtractionInterval > 0`, add a third cron job calling
    `s.runReExtraction()`. The cron schedule should stagger the job to
    avoid colliding with ingestion (e.g., offset by half the ingestion
    interval, or use a separate `@every` entry that naturally avoids
    overlap since cron jobs run sequentially within a single `cron.Cron`
    instance).
  - [ ] Add `runReExtraction()` method:
    ```go
    func (s *Scheduler) runReExtraction() {
        ctx := context.Background()
        s.log.Info("scheduled re-extraction starting")
        count, err := s.engine.RunReExtraction(ctx, "", 100)
        if err != nil {
            s.log.Error("scheduled re-extraction failed", "error", err)
        } else {
            s.log.Info("scheduled re-extraction completed", "re_extracted", count)
        }
        s.SyncNextRunTimestamps()
    }
    ```
  - [ ] Update `SyncNextRunTimestamps` to include re-extraction entry if
    the entry ID is non-zero (add a new Prometheus gauge for it)
- [ ] Fix existing callers of `NewScheduler` (4 total):
  - [ ] `cmd/server-price-tracker/cmd/serve.go:309` — pass
    `cfg.Schedule.ReExtractionInterval` as the new 4th argument
  - [ ] `internal/engine/scheduler_test.go` — update all 4 test calls to
    pass `0` as the 4th argument (re-extraction disabled)
- [ ] Add test in `internal/engine/scheduler_test.go`:
  - [ ] `TestNewScheduler_WithReExtraction` — pass non-zero
    `reExtractionInterval`, assert `len(sched.Entries()) == 3`
  - [ ] `TestNewScheduler_WithoutReExtraction` — pass `0`, assert
    `len(sched.Entries()) == 2` (existing test, verify it still passes)
- [ ] Run `make test && make lint`

### Success Criteria

- When `schedule.re_extraction_interval` is set in config, a periodic
  re-extraction job runs on that interval
- When not set (zero), no re-extraction job is scheduled (no behavior change)
- All 4 existing callers are updated, all existing scheduler tests pass
- New test verifies 3 cron entries when re-extraction is enabled

### Files

- `internal/config/config.go` (modify)
- `internal/engine/scheduler.go` (modify)
- `internal/engine/scheduler_test.go` (modify)
- `cmd/server-price-tracker/cmd/serve.go` (modify)

---

## Final Verification

```bash
# All unit tests
make test

# Lint
make lint

# Build both binaries
make build

# Manual smoke test (requires running server + Ollama)
build/bin/spt reextract --type ram --limit 5
curl -s localhost:8080/api/v1/extraction/stats | jq .
curl -s localhost:8080/metrics | grep incomplete_extraction
```

---

## Resolved Questions

1. **SQL for non-RAM incomplete extractions**: Include drives too. RAM
   checks for missing `speed_mhz` (product key ending `:0`). Drives
   check for missing `form_factor` or `type` (product key containing
   `:unknown`). NICs deferred for now.

2. **Re-extraction and LLM load**: The scheduled re-extraction job
   (Phase 10) should have a buffer before/after so it doesn't collide
   with scheduled ingestion. Sequential processing at ~10s/extraction is
   inherently self-limiting. If GPU saturation becomes a problem, that's
   an infrastructure issue (bigger GPU, model quantization, concurrency
   limits) — not something to solve in the re-extraction code itself.

3. **Scheduler constructor signature**: Add `reExtractionInterval` as a
   4th `time.Duration` parameter. There are only 4 callers: one in
   `serve.go` and three in `scheduler_test.go`. The tests pass `0` for
   the new param to keep current behavior (zero means disabled). This
   is the simplest fix with minimal churn.

4. **Metric cardinality for `by_type` gauge vec**: Initialize all known
   component types (ram, drive, server, cpu, nic) to zero on startup in
   `SyncStateMetrics`. Cardinality is bounded at 5.

5. **PC4 regex edge cases**: Yes — `ExtractSpeedFromTitle` should have a
   fallback regex for `DDR[345]-?(\d{4})` patterns (e.g., `DDR4-2666`,
   `DDR4 2666MHz`) in addition to the PC bandwidth regex. Try the PC4
   regex first; if no match, try the DDR speed regex. This catches the
   majority of eBay title formats.

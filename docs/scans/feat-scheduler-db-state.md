# Code Review Scan — feat/scheduler-db-state

**Branch:** `feat/scheduler-db-state`
**Date:** 2026-02-24
**Scope:** Phases 1–5 of `docs/impl/REFACTOR.md` + test coverage improvements
**Tools:** `go vet`, `go build -race`, `golangci-lint`, manual architectural review

---

## Automated Checks

| Tool | Result |
|------|--------|
| `go vet ./...` | Clean |
| `go build -race ./...` | Clean (no races; macOS `ld` warnings are linker noise, not code issues) |
| `golangci-lint run ./...` | 0 issues |

---

## Findings

### FIXED — Empty cursor string causes UUID cast error in ListListingsCursor

**File:** `internal/store/postgres.go:529`
**Commit:** `90b684e`
**Severity:** Medium

`ListListingsCursor` passed the empty string `""` directly into `WHERE id > $1`
where `id` is `UUID`. PostgreSQL rejects this with `invalid input syntax for type uuid: ""`.
The function's contract says "pass empty string to start from the beginning" but the
query didn't implement that. Fixed by substituting the nil UUID
(`00000000-0000-0000-0000-000000000000`) when `afterID == ""`, which is always less
than any `gen_random_uuid()` value. Discovered when running `POST /api/v1/baselines/refresh`
against a live database.

---

### FIXED — Error detail swallowed in extraction stats handler

**File:** `internal/api/handlers/extraction_stats.go:42`
**Commit:** `04217a8`
**Severity:** Medium

`huma.Error500InternalServerError("failed to get extraction stats")` dropped the underlying
`err.Error()`, making failures undebuggable from API responses or logs. Every other handler
in the codebase appends the error string. Fixed by appending `+ err.Error()`.

---

### Informational — Extraction worker shutdown is unverified

**File:** `cmd/server-price-tracker/cmd/serve.go:106-107`, `internal/engine/engine.go:136-145`
**Severity:** Low (accepted trade-off)

`StartExtractionWorkers` launches goroutines without a `sync.WaitGroup`. When `workerCancel()`
is called during shutdown, the next log line says "extraction workers stopped" but workers in the
middle of an LLM call may still be running.

**Why it's acceptable here:** Adding a WaitGroup would block shutdown until in-flight LLM calls
complete, which can take minutes. The current design relies on:

1. Context cancellation prevents new dequeues immediately.
2. The `FOR UPDATE SKIP LOCKED` queue pattern ensures any interrupted job is left in `running`
   state and recovered by `RecoverStaleJobRuns` on next startup.
3. The OS reclaims goroutine resources cleanly on process exit.

**If this needs to change:** Add `sync.WaitGroup` to `Engine`, increment in
`StartExtractionWorkers`, decrement via `defer wg.Done()` in `runExtractionWorker`, and expose
`eng.WaitWorkers()` in the shutdown path in `serve.go`. Callers would need to accept that
shutdown may take up to the LLM timeout (`cfg.LLM.Timeout`, default 120s).

---

### Informational — `slog.Default()` in alert package functions

**File:** `internal/engine/alert.go:111, 165`
**Severity:** Low

`sendSingle` and `sendBatch` call `slog.Default().Warn(...)` for failed
`InsertNotificationAttempt` log lines rather than using a caller-provided logger. This works
fine as long as `slog.SetDefault` is called at startup (which it is in `serve.go`), but breaks
structured log field consistency if a non-default handler is ever configured per-request.

The functions are package-level rather than methods, so there is no `eng.log` available without
threading a `*slog.Logger` through `ProcessAlerts → sendAlerts → sendSingle/sendBatch`. That
refactor is not worth the churn at current scale.

---

## False Positives Noted

These were flagged by automated review agents but are intentional in this codebase:

| Claim | Verdict |
|-------|---------|
| `pg.Close()` missing from shutdown sequence | False positive — already `defer pg.Close()` at `serve.go:48`, runs after HTTP server shutdown |
| Nil→empty slice checks in `jobs.go` unnecessary | False positive — required to serialize `[]` rather than `null` in Huma JSON responses |
| `attrs` parameter name should be `attributes` | Not a violation — `attrs` is idiomatic shorthand used consistently throughout `pkg/extract/` |
| `runJob` should wrap the `fn(ctx)` error | Not applicable — error is propagated to callers unchanged by design; wrapping would obscure root cause |
| Background context in `runIngestion` should have timeout | By design — TTL enforcement is at the scheduler lock level (`scheduler_locks.expires_at`), not the context |

---

## Architecture Assessment

### Distributed Lock Pattern (`internal/engine/scheduler.go`)

**Verdict: Correct** for multi-instance deployments.

The `scheduler_locks` table uses a conditional upsert:

```sql
INSERT INTO scheduler_locks (job_name, lock_holder, expires_at) VALUES (...)
ON CONFLICT (job_name) DO UPDATE
    SET ...
    WHERE scheduler_locks.expires_at < now()  -- only steal if expired
RETURNING job_name
```

Strengths: atomic acquisition, TTL-based expiry prevents orphaned locks from crashed pods,
lock holder identity prevents cross-instance releases. TTLs (ingestion 30 min, baseline 60 min,
re-extraction 30 min) are appropriate for current workload.

Minor caveats: no lock renewal for very long jobs, clock skew between pods could cause early
expiry. Both are acceptable at current scale.

### Cursor Pagination (`internal/store/postgres.go` — `ListListingsCursor`)

**Verdict: Safe and correct.**

```sql
SELECT ... FROM listings WHERE id > $1 ORDER BY id ASC LIMIT $2
```

UUID primary key provides stable, monotonic ordering. Empty `afterID` starts from the beginning.
The `RescoreAll` caller correctly advances the cursor to `batch[len(batch)-1].ID`. No rows will
be skipped or duplicated across pages.

### Extraction Queue (`FOR UPDATE SKIP LOCKED`)

**Verdict: Correct** for worker fan-out.

`DequeueExtractions` uses `FOR UPDATE SKIP LOCKED` on the `extraction_queue` table, ensuring
each job is claimed by exactly one worker even under concurrent access. Workers are stateless;
any worker can process any job.

### Shutdown Sequence (`cmd/server-price-tracker/cmd/serve.go`)

**Verdict: Correct order.**

1. `workerCancel()` — stops new dequeues
2. `scheduler.Stop()` / `<-schedCtx.Done()` — drains cron jobs
3. `e.Shutdown(shutdownCtx)` — HTTP server drain (10s timeout)
4. `defer pg.Close()` — connection pool closed last (via deferred call in `startServer`)

---

## Test Coverage Summary (post-PR)

Coverage improved as part of this branch. Key results:

| Package | Coverage |
|---------|----------|
| `internal/api/handlers` | 95.1% |
| `internal/api/client` | 86.2% |
| `internal/engine` | 88.0% |
| `internal/notify` | 100% (noop previously 0%) |
| `pkg/extract` | (unchanged, was high) |
| `internal/store` (unit) | (postgres.go excluded — integration only) |

Notable improvements:
- `notify/noop.go`: 0% → 100%
- `engine/alert.go` ProcessAlerts: 93% → 100%
- `engine/alert.go` sendBatch: 78% → 96%
- `engine/engine.go` processExtractionJob: 69% → 96%
- `engine/engine.go` completeJob: 50% → 100%
- `engine/scheduler.go` run* methods: 0% → 80%
- `api/client/jobs.go`: 0% → 75%
- `api/client/listings.go` ReExtract: 0% → 89%

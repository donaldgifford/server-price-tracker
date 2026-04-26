---
id: DESIGN-0009
title: "Discord notifier rate limiting and embed chunking"
status: Draft
author: Donald Gifford
created: 2026-04-26
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0009: Discord notifier rate limiting and embed chunking

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-26

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Phase 1 — off-by-one fix](#phase-1--off-by-one-fix)
  - [Phase 2 — chunked sends with rate-limit tracking](#phase-2--chunked-sends-with-rate-limit-tracking)
  - [Phase 3 — persisted bucket state (deferred)](#phase-3--persisted-bucket-state-deferred)
  - [Atomicity of `MarkAlertsNotified`](#atomicity-of-markalertsnotified)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Fix the Discord notifier's batch send so it (a) never exceeds Discord's
10-embed-per-message limit and (b) respects Discord's per-route rate
limits via the `X-RateLimit-*` response headers. Today a batch of >10
alerts produces a payload of 11 embeds (10 alerts + 1 summary) which
Discord rejects with HTTP 400 `Must be 10 or fewer in length`, and a
backlog of thousands of pending alerts can stampede the webhook with
no rate-limit awareness.

## Goals and Non-Goals

### Goals

- Eliminate the off-by-one that produces 11-embed batches.
- Send all batched alerts (no truncation) by chunking into multiple
  webhook POSTs of up to 10 embeds each.
- Honor Discord's `X-RateLimit-Remaining` / `X-RateLimit-Reset-After`
  headers between chunks so a single notifier loop cannot blast through
  a webhook bucket.
- Handle 429 responses with `Retry-After` instead of treating them as
  fatal errors that bubble up unwrapped.
- Surface rate-limit state via Prometheus metrics so a future Grafana
  panel can show "Discord throttled" / "queue draining at X/min".
- Preserve current alert idempotency: an alert is only marked notified
  after its embed actually reached Discord with a 2xx.

### Non-Goals

- Persisting bucket state across replicas / restarts (Phase 3, deferred
  until we run more than one notifier replica). Single-replica today.
- Replacing the in-process notifier with a queue/worker model — the
  scheduler-driven `ProcessAlerts` loop is fine; we only need it to
  back off correctly inside a single tick.
- Reducing alert *volume* — that is DESIGN-0010's problem (the alert
  review UI). DESIGN-0009 only ensures whatever volume we have can
  actually leave the process.
- Removing the `SendBatchAlert` summary embed entirely. It is useful;
  we just need to stop overflowing because of it.

## Background

PR #44 (`fix/post-score-alert-evaluation`) wired alert evaluation into
both the worker post-score path and the manual `/api/v1/rescore`
endpoint. After it merged we ran a rescore over a populated DB and
created ~2,100 pending alerts. The next scheduler tick (30 min cadence)
called `ProcessAlerts`, which grouped alerts by watch and dispatched
batches via `SendBatchAlert`. **All 2,129 attempts failed** with:

```text
discord returned 400: {"embeds": ["Must be 10 or fewer in length."]}
```

The cause is `internal/notify/discord.go:82-107`:

```go
limit := min(len(alerts), 10)
for i := range limit {
    embeds = append(embeds, buildEmbed(&alerts[i]))
}
if len(alerts) > 10 {
    embeds = append(embeds, discordEmbed{
        Title: fmt.Sprintf("... and %d more alerts for %s", len(alerts)-10, watchName),
        ...
    })
}
```

When `len(alerts) > 10` the function packs 10 alert embeds *and* a
summary embed → 11 embeds in the payload → 400. Beyond the off-by-one,
the notifier today is also rate-limit naive: a successful batch returns
nil and the next call goes out as fast as the engine produces it. The
`post()` helper recognizes 429 only as a generic error string
(`"discord rate limited (429)"`) — no `Retry-After` parsing, no
preemptive throttling.

Discord's webhook API enforces:

| Header                       | Meaning                                                  |
|------------------------------|----------------------------------------------------------|
| `X-RateLimit-Limit`          | Total requests in this bucket per window                 |
| `X-RateLimit-Remaining`      | Requests left in current window                          |
| `X-RateLimit-Reset`          | Unix epoch (seconds) when the bucket resets              |
| `X-RateLimit-Reset-After`    | Float seconds until reset (more accurate than `Reset`)   |
| `X-RateLimit-Bucket`         | Opaque bucket identifier (route hash)                    |
| `Retry-After` (on 429)       | Seconds (float) to wait before retrying                  |
| `X-RateLimit-Global` (on 429)| `true` if the cap is global (not per-route)              |

Webhooks are typically rate-limited at ~5 requests / 2 seconds per
channel route, but Discord intentionally does not document a fixed
number — clients must read the headers.

## Detailed Design

The fix is staged so the smallest, highest-value change ships first.

### Phase 1 — off-by-one fix

A 1-line change to `SendBatchAlert`: when overflowing, reserve a slot
for the summary embed.

```go
const maxEmbedsPerMessage = 10

func (d *DiscordNotifier) SendBatchAlert(
    ctx context.Context,
    alerts []AlertPayload,
    watchName string,
) error {
    embeds := make([]discordEmbed, 0, maxEmbedsPerMessage)

    alertCap := min(len(alerts), maxEmbedsPerMessage)
    if len(alerts) > maxEmbedsPerMessage {
        alertCap = maxEmbedsPerMessage - 1 // leave room for summary
    }

    for i := 0; i < alertCap; i++ {
        embeds = append(embeds, buildEmbed(&alerts[i]))
    }

    if len(alerts) > maxEmbedsPerMessage {
        embeds = append(embeds, discordEmbed{
            Title:       fmt.Sprintf("... and %d more alerts for %s", len(alerts)-alertCap, watchName),
            Color:       colorYellow,
            Description: "Check the dashboard for the full list.",
        })
    }

    return d.post(ctx, discordWebhookPayload{Embeds: embeds})
}
```

This is the immediate "stop dropping every batch on the floor" fix and
should ship on its own.

**Limitation:** still truncates. With 2,129 pending alerts in one
watch we deliver 9 to Discord and tell the user "and 2,120 more" —
better than 0 but unusable as an alert stream. Phase 2 fixes that.

### Phase 2 — chunked sends with rate-limit tracking

Replace truncation with chunking. For an N-alert batch:

- Split into `ceil(N/10)` chunks of ≤10 embeds each.
- Send chunks sequentially.
- Between chunks, consult an in-process rate-limit tracker and sleep
  if the current bucket has `Remaining == 0`.

New type inside `internal/notify`:

```go
// rateLimitState tracks the most recent Discord rate-limit response
// for the webhook's bucket. Single-bucket per webhook URL is the
// realistic case (one channel route).
type rateLimitState struct {
    mu        sync.Mutex
    bucket    string    // X-RateLimit-Bucket
    remaining int       // X-RateLimit-Remaining
    resetAt   time.Time // computed from X-RateLimit-Reset-After at receive time
}

func (r *rateLimitState) update(resp *http.Response) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.bucket = resp.Header.Get("X-RateLimit-Bucket")
    if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            r.remaining = n
        }
    }
    if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
        if f, err := strconv.ParseFloat(v, 64); err == nil {
            r.resetAt = time.Now().Add(time.Duration(f * float64(time.Second)))
        }
    }
}

// waitForBucket blocks until the current bucket has capacity, or returns
// immediately if remaining > 0 / state is unknown.
func (r *rateLimitState) waitForBucket(ctx context.Context) error {
    r.mu.Lock()
    needWait := r.remaining == 0 && time.Now().Before(r.resetAt)
    waitFor := time.Until(r.resetAt)
    r.mu.Unlock()

    if !needWait {
        return nil
    }
    metrics.DiscordRateLimitWaitsTotal.Inc()
    select {
    case <-time.After(waitFor):
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

`SendBatchAlert` becomes the chunk loop:

```go
func (d *DiscordNotifier) SendBatchAlert(
    ctx context.Context,
    alerts []AlertPayload,
    watchName string,
) error {
    chunks := chunkAlerts(alerts, maxEmbedsPerMessage)
    for i, chunk := range chunks {
        if err := d.rateLimit.waitForBucket(ctx); err != nil {
            return fmt.Errorf("rate-limit wait (chunk %d/%d): %w", i+1, len(chunks), err)
        }
        embeds := make([]discordEmbed, 0, len(chunk))
        for j := range chunk {
            embeds = append(embeds, buildEmbed(&chunk[j]))
        }
        if err := d.post(ctx, discordWebhookPayload{Embeds: embeds}); err != nil {
            return fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
        }
        metrics.DiscordChunksSentTotal.Inc()
    }
    return nil
}
```

`post()` is updated to:

- Always read response headers into `d.rateLimit.update(resp)` before
  any other handling, even on non-2xx.
- On 429: parse `Retry-After`, sleep that long, retry the same payload
  **once**. If the second attempt also 429s, return error and let
  `ProcessAlerts` handle (the alert stays unmarked and will retry next
  tick).
- On `X-RateLimit-Global: true` 429: return error immediately (don't
  retry — every other call will also be globally throttled). Caller
  should abort the batch.

```go
func (d *DiscordNotifier) post(ctx context.Context, payload discordWebhookPayload) error {
    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshaling discord payload: %w", err)
    }

    for attempt := 0; attempt < 2; attempt++ {
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
        if err != nil {
            return fmt.Errorf("creating discord request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")

        start := time.Now()
        resp, err := d.client.Do(req)
        metrics.NotificationDuration.Observe(time.Since(start).Seconds())
        if err != nil {
            return fmt.Errorf("sending discord webhook: %w", err)
        }

        d.rateLimit.update(resp)
        metrics.DiscordRateLimitRemaining.Set(float64(d.rateLimit.snapshotRemaining()))

        if resp.StatusCode == http.StatusTooManyRequests {
            global := resp.Header.Get("X-RateLimit-Global") == "true"
            metrics.Discord429Total.WithLabelValues(strconv.FormatBool(global)).Inc()
            retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
            resp.Body.Close()
            if global || attempt == 1 {
                return fmt.Errorf("discord rate limited (global=%v, attempt %d)", global, attempt+1)
            }
            select {
            case <-time.After(retryAfter):
            case <-ctx.Done():
                return ctx.Err()
            }
            continue
        }

        defer resp.Body.Close()
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
            respBody, _ := io.ReadAll(resp.Body)
            return fmt.Errorf("discord returned %d: %s", resp.StatusCode, respBody)
        }
        return nil
    }
    return fmt.Errorf("discord post: exhausted retries")
}
```

`SendAlert` (single-embed path) gets the same `waitForBucket` +
retry-aware `post` for free since they share the helper.

### Phase 3 — persisted bucket state (deferred)

Today the notifier is one process; in-memory state is sufficient. If
we later run multiple replicas (Helm `replicaCount > 1`) the in-process
tracker stops working — replica B doesn't know that replica A just
got `Remaining: 0`. At that point we either:

- Write bucket state to a `discord_rate_limits` row keyed by webhook
  URL hash, with `remaining` / `reset_at` columns and short TTL
  semantics, or
- Move the notifier behind a leader-elected scheduler lock so only one
  replica calls Discord at a time (we already have
  `AcquireSchedulerLock` for the broader scheduler).

The leader-lock route piggybacks on existing infra and is preferred.
Out of scope until we actually run multi-replica.

### Atomicity of `MarkAlertsNotified`

`engine.sendBatch` calls `s.MarkAlertsNotified(ctx, alertIDs)` once
after `n.SendBatchAlert` returns nil (`internal/engine/alert.go:180`).

With chunking, partial success becomes possible: chunks 1–4 succeed,
chunk 5 hits a 400, error returns. We must mark *only* the alerts in
chunks 1–4 as notified, otherwise the next tick re-sends them and we
double-notify.

Two options:

1. **Notifier returns the count of alerts successfully sent** (e.g.,
   `SendBatchAlert` returns `(sentCount int, err error)`). Engine
   slices `alertIDs[:sentCount]` and marks those.
2. **Move the per-chunk mark inside the notifier** by passing a
   per-chunk callback. Cleaner blast radius but couples notifier to
   store interface — bad layering.

Option 1 is the choice. `Notifier.SendBatchAlert` signature changes
to:

```go
SendBatchAlert(ctx context.Context, alerts []AlertPayload, watchName string) (sent int, err error)
```

`sent` is the number of embed slots from the front of `alerts` that
were actually delivered (i.e., chunks-completed × 10, capped at
`len(alerts)`). The summary embed in the final chunk does not count
against `sent`. Engine then does:

```go
sentCount, sendErr := n.SendBatchAlert(ctx, payloads, watch.Name)
if sentCount > 0 {
    delivered := alertIDs[:sentCount]
    if err := s.MarkAlertsNotified(ctx, delivered); err != nil { ... }
    for _, id := range delivered {
        s.InsertNotificationAttempt(ctx, id, true, 0, "")
    }
}
if sendErr != nil {
    // record failure on the undelivered tail
    for _, id := range alertIDs[sentCount:] {
        s.InsertNotificationAttempt(ctx, id, false, 0, sendErr.Error())
    }
    return sendErr
}
```

`SendAlert` (single) keeps its `error`-only signature.

## API / Interface Changes

- **HTTP API:** none.
- **CLI:** none.
- **Notifier interface (`internal/notify/notifier.go`):**
  `SendBatchAlert` signature changes to return `(int, error)`. All
  implementations updated: `DiscordNotifier`, `NoOpNotifier`,
  `MockNotifier` (re-generate via `make mocks`).
- **Engine (`internal/engine/alert.go`):** `sendBatch` reworked to
  honor partial-success contract (above).
- **Config (`internal/config`):** optional knobs under `notify.discord`:
  - `inter_chunk_delay`: default `0s` (rely on header-driven waits).
    Reserved for environments where headers prove insufficient.
  - `max_chunks_per_batch`: default `0` (unbounded). Safety cap if
    we want to defer the long tail to the next tick.

## Data Model

Phase 1 / Phase 2: no schema changes. `notification_attempts` already
records per-alert success/failure with error text; chunking just means
multiple alerts may share the same wall-clock attempt window.

Phase 3 (deferred): would add a `discord_rate_limits(bucket text
primary key, remaining int, reset_at timestamptz)` table. Not part of
this design's rollout.

## Testing Strategy

- **Unit (notifier):**
  - Table-driven tests over `chunkAlerts(n)` for n in `{0, 1, 9, 10,
    11, 21, 100}` asserting chunk counts and last-chunk sizes.
  - `httptest.NewServer` test that asserts `SendBatchAlert(15)` issues
    exactly 2 POSTs, both with `len(embeds) <= 10`, and returns
    `sent == 15`.
  - Mock server that returns `X-RateLimit-Remaining: 0`,
    `X-RateLimit-Reset-After: 0.05` after the first chunk; assert the
    second chunk waits at least ~50ms before posting.
  - Mock server that returns 429 with `Retry-After: 0.02` once, then
    200; assert one retry happens and `sent` reflects success.
  - Mock server that returns 429 with `X-RateLimit-Global: true`;
    assert no retry, error returned, `sent == 0`.
- **Unit (engine):**
  - Update `TestProcessAlerts_BatchAlert` to mock
    `SendBatchAlert(...).Return(len(alerts), nil)` and verify
    `MarkAlertsNotified` is called with the full slice.
  - New: partial-failure case. `SendBatchAlert` returns
    `(20, errors.New("chunk 3 failed"))` for a 30-alert batch; assert
    `MarkAlertsNotified` is called with the first 20 IDs only and
    `InsertNotificationAttempt(..., false, ...)` for the remaining
    10.
- **Integration:** none required. Real Discord smoke test is manual
  (point dev webhook at a throwaway channel, force a >10-alert
  scenario, watch logs + dashboard).

## Migration / Rollout Plan

- Ship Phase 1 (off-by-one) as its own commit. Backwards compatible —
  `sent` return value is new but Phase 1 doesn't change the
  signature; chunked sends arrive in Phase 2 commits.
- Phase 2 ships once mock-server tests are green. Notifier interface
  change is internal (consumed only by the engine), so no external
  contract break.
- No feature flag — the off-by-one is a bug fix and chunking is
  strictly more correct than truncating.
- Rollback: revert the two commits; v0.7.5 behavior (truncate at 10)
  returns. Acceptable because the 400 bug is already in v0.7.6 paths
  that we're fixing.
- Operator-visible: new metrics
  (`spt_discord_rate_limit_remaining`, `spt_discord_429_total`,
  `spt_discord_chunks_sent_total`, `spt_discord_rate_limit_waits_total`)
  appear on `/metrics`. Update Grafana via the existing dashgen flow
  in a follow-up.

## Open Questions

1. **Inter-chunk delay default.** Discord's header-driven approach is
   "send until `Remaining: 0`, then wait until `Reset-After`". Is
   that aggressive enough that we'll get throttled on the first batch
   we send and then the *real* delay kicks in only on the second?
   Tentative answer: yes, that's the protocol — first batch at full
   speed is correct. Add `inter_chunk_delay` config as an escape
   hatch only.
2. **Give-up criteria.** If a watch has 500 pending alerts and chunk
   17 fails with a 500 from Discord, do we (a) abort the watch, (b)
   skip the failing chunk and continue, (c) abort the whole tick?
   Current proposal: (a) — return the error, leave remaining alerts
   pending, retry on next tick. (b) is fragile (we don't know which
   embed in the chunk Discord choked on); (c) punishes other watches.
3. **Per-bucket vs global tracking.** Webhook posts to one channel
   share a single bucket, so a single `rateLimitState` per
   `DiscordNotifier` instance is correct. But if we ever support
   multiple webhooks (per-watch routing), tracker should key on
   `X-RateLimit-Bucket`. Defer until multi-webhook is on the
   roadmap.
4. **Coordination with DESIGN-0010.** If the alert review UI lands
   first and Discord becomes "summary-only" (one embed per tick
   linking to the dashboard), Phase 2 chunking is much less
   load-bearing. Worth keeping anyway since the summary path still
   needs to handle 429s, but the chunk math becomes mostly defensive.
   Not a blocker for either design — they're complementary.

## References

- Bug surface: `internal/notify/discord.go:82-107` (`SendBatchAlert`).
- Notifier interface: `internal/notify/notifier.go`.
- Engine batch path: `internal/engine/alert.go:127-180` (`sendBatch`,
  `MarkAlertsNotified`).
- Discord rate limits docs:
  <https://discord.com/developers/docs/topics/rate-limits>
- Discord webhook embed limit (10): documented inline in API ref under
  Execute Webhook.
- Triggering incident: PR #44 (`fix/post-score-alert-evaluation`)
  rescore at 2026-04-25 produced 2,129 pending alerts that all failed
  with `400 Must be 10 or fewer in length`.
- Related: DESIGN-0010 (alert review UI) — reduces pressure on this
  path by making Discord summary-only.

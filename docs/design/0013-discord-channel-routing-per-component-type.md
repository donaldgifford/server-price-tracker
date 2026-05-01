---
id: DESIGN-0013
title: "Discord channel routing per component type"
status: Draft
author: Donald Gifford
created: 2026-05-01
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0013: Discord channel routing per component type

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-01

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Allow the Discord notifier to route alerts to a different webhook (and
therefore a different Discord channel) per `ComponentType`. Today every
alert lands in one channel via a single `notifications.discord.webhook_url`,
which makes high-volume mixed alerts hard to triage. Multiple webhooks
let the operator subscribe to channels per type, mute the noisy ones,
and read GPU/server/RAM streams independently.

## Goals and Non-Goals

### Goals

- Allow a webhook URL to be configured *per component type*, with a
  default fallback when a type has no specific URL.
- Keep the existing single-webhook config working unchanged
  (backwards-compat for deployments that don't migrate).
- Per-type webhooks must continue to honor the existing chunking,
  rate-limiting, and 429 retry behavior from DESIGN-0009.
- Summary mode (DESIGN-0010) keeps working — when on, each per-type
  channel receives its own summary embed for a tick, OR a single combined
  summary lands in a designated default channel (decision below in
  Open Q3).
- Alert review UI (`/alerts`) is unchanged — DB rows still represent the
  authoritative work queue regardless of which channel got the embed.

### Non-Goals

- **No watch-level webhook overrides.** The decision is per
  ComponentType, not per watch. Two watches both targeting `ram` use the
  same RAM webhook.
- **No Discord thread routing.** Webhooks targeting a specific thread
  within a channel (`?thread_id=…`) are out of scope; one webhook URL
  per type is enough for the stated goal.
- **No dynamic channel creation** via Discord bot APIs. The operator
  creates channels and pastes webhook URLs into config.
- **No new metrics surface.** Existing `spt_alerts_fired_total` (Discord
  delivery counter) and `spt_alerts_created_total` (engine counter)
  cover this; we may *label* delivery by component_type as a follow-up
  but the design doesn't require it.

## Background

Today's wiring (after DESIGN-0009 / IMPL-0015 Phase 5):

- Single `DiscordNotifier` constructed from `notifications.discord.webhook_url`.
- Engine's `processAlertsForWatch` collects all pending alerts for a
  watch, builds `[]AlertPayload`, calls `notifier.SendBatchAlert`.
- Notifier chunks at ≤10 embeds/POST, parses `X-RateLimit-*` headers,
  retries non-global 429 once.
- Summary mode (DESIGN-0010): `summary_only=true` collapses a tick into
  one embed via `BuildSummaryPayload`.

Engine currently iterates watch-by-watch, but a tick across multiple
watches naturally interleaves multiple component types into the same
notifier. With one webhook, that means one channel receives every type's
embeds concatenated. Operators are reporting the obvious problem: it's
unsearchable in Discord and hard to triage.

The simplest fix is: maintain a `map[ComponentType]Notifier`, route each
alert to the notifier registered for its component type, and fall back
to a default notifier when a type isn't configured.

## Detailed Design

### 1. Config schema

Extend `DiscordConfig`:

```go
type DiscordConfig struct {
    Enabled    bool   `yaml:"enabled"`
    WebhookURL string `yaml:"webhook_url"` // default fallback
    // Channels maps component_type → webhook URL. When a type isn't
    // present, alerts of that type fall back to WebhookURL.
    Channels map[string]string `yaml:"channels,omitempty"`

    InterChunkDelay time.Duration `yaml:"inter_chunk_delay"`
    SummaryOnly     bool          `yaml:"summary_only"`
}
```

Example YAML:

```yaml
notifications:
  discord:
    enabled: true
    webhook_url: ${DISCORD_WEBHOOK_DEFAULT}   # fallback
    channels:
      ram:    ${DISCORD_WEBHOOK_RAM}
      drive:  ${DISCORD_WEBHOOK_DRIVE}
      server: ${DISCORD_WEBHOOK_SERVER}
      cpu:    ${DISCORD_WEBHOOK_CPU}
      gpu:    ${DISCORD_WEBHOOK_GPU}
      nic:    ${DISCORD_WEBHOOK_NIC}
      other:  ${DISCORD_WEBHOOK_OTHER}    # optional
```

Backwards-compat: omit `channels` and the existing single-webhook
behavior is preserved unchanged.

### 2. Notifier composition

Introduce `RoutingNotifier` in `internal/notify/`. It implements the
existing `Notifier` interface:

```go
type RoutingNotifier struct {
    perType map[domain.ComponentType]Notifier
    fallback Notifier
}

func (r *RoutingNotifier) SendBatchAlert(
    ctx context.Context,
    alerts []AlertPayload,
    watchName string,
) (sent int, err error) {
    // Group alerts by ComponentType. Send each group to its routed
    // notifier. Aggregate (sent, err) — first error wins, sent is the
    // total successfully delivered across all groups.
}
```

`SendAlert` (single) routes by `alert.ComponentType` directly.

The constructor builds one `DiscordNotifier` per configured channel
(each with its own rate limiter state, since Discord rate limits are
per-webhook). The fallback is the notifier built from `WebhookURL`.

### 3. Engine wiring

`engine.evaluateAlert` and the existing `processAlertsForWatch` don't
need to know about routing. They keep calling `notifier.SendBatchAlert`.
Composition happens at construction time in `cmd/server-price-tracker`.

```go
notifier := buildNotifier(cfg.Notifications.Discord) // RoutingNotifier or single
engine := NewEngine(store, ebay, extractor, notifier, ...)
```

`buildNotifier` returns a `Notifier` implementation:

- If `Channels` is empty → return single `DiscordNotifier(WebhookURL)`
  (status quo).
- Else → return `RoutingNotifier` with per-type notifiers + fallback.

### 4. Summary mode interaction

When `summary_only=true`, the engine currently produces *one*
`AlertPayload` per watch tick with `SummaryFields` populated. Two viable
behaviors:

- **Per-channel summary**: group pending alerts by component type, build
  one summary payload per type, route each to its channel. Pro: each
  channel still gets meaningful context. Con: more noise overall.
- **Combined summary in fallback channel**: one summary embed listing
  counts-by-type, sent only to the fallback webhook. Pro: minimal
  noise; matches the "I want one place to triage" intent of summary
  mode. Con: defeats the per-type channel split for summary mode users.

Defaulting to per-channel summary is the more consistent choice: if you
opted into channel routing, you want per-channel signal even in summary
mode. The fallback becomes a catch-all only for component types you
didn't configure.

(Open Q3 below; pick one before implementation.)

### 5. Edge cases

- **`other` component type**: usually represents accessories the
  pre-classifier suppressed. Operator may legitimately not want a
  channel for these. If unconfigured, falls through to the default
  webhook. If the default also isn't set, the alert is logged and
  skipped (no panic, no retry-loop).
- **Webhook URL secret rotation**: each per-type webhook URL is read
  via `os.ExpandEnv()` on config load (existing pattern). Rotation
  requires pod restart, same as today.
- **Per-webhook rate limits**: Discord rate-limits per webhook, which
  is exactly the upside — RAM channel hitting 429 doesn't slow GPU
  alerts. Each `DiscordNotifier` has its own state.
- **Empty `webhook_url` with `Channels` populated**: legal. No fallback.
  Any alert whose type isn't in the map gets logged and dropped.
- **Empty everything but `Enabled=true`**: validation error at startup.

### 6. Validation

`Config.Validate()` (or the closest equivalent for notifications) gains:

- If `Discord.Enabled` and `Channels` is non-empty, every URL must be
  non-empty after env expansion.
- If `Discord.Enabled` and both `WebhookURL` *and* `Channels` are empty,
  startup fails.
- Component type keys in `Channels` must be one of the known types
  (`ram, drive, server, cpu, nic, gpu, other`). Unknown keys log a
  warning at startup but don't fail (forward-compat).

## API / Interface Changes

- `notify.Notifier` interface unchanged.
- New unexported type `notify.routingNotifier` (or exported
  `RoutingNotifier`) implementing the same interface — caller-transparent.
- New constructor `notify.NewRoutingNotifier(perType map[ComponentType]Notifier, fallback Notifier)`.
- Existing `DiscordNotifier` and its options (`WithInterChunkDelay`)
  unchanged.
- Config: `DiscordConfig.Channels` field added. No CLI/API surface
  change.

## Data Model

No schema changes. Existing `notification_attempts` rows still link to
an `alert_id`; the alert's component_type is derivable from the joined
listing. If we want a "delivered to which channel" audit trail later,
add a `webhook_label` text column to `notification_attempts` — out of
scope here.

## Testing Strategy

Unit tests in `internal/notify/`:

- `RoutingNotifier_GroupsByComponentType` — given 6 alerts spread across
  3 types, asserts `SendBatchAlert` is called 3 times on the right
  per-type notifiers with correct subsets.
- `RoutingNotifier_FallbackForUnconfiguredType` — alerts whose type
  isn't in the map go to the fallback notifier.
- `RoutingNotifier_NoFallbackNoConfig` — when both routing entry and
  fallback are missing, alerts are dropped (logged) and the call still
  reports the count of alerts actually delivered.
- `RoutingNotifier_AggregatesSentCount` — three groups, two succeed
  fully, one returns `(2, errOf3)`. Caller sees the sum and the first
  error.
- `RoutingNotifier_SummaryMode` — depends on Open Q3 resolution; tests
  match the chosen behavior.

`internal/config/`:

- Validates the new map (empty values, unknown keys, no-fallback case).

`internal/engine/` engine tests need no changes — composition is
behind the `Notifier` interface.

End-to-end (manual): set up two test webhooks pointing at two channels
in a Discord test server, ingest, observe per-type routing.

## Migration / Rollout Plan

1. Land code change behind no flag — additive to config schema.
2. Existing deployments with only `webhook_url` set continue to use the
   single-channel path (no behavior change).
3. Operator wanting channel routing:
   1. Create new Discord channels (one per type they care about).
   2. Create a webhook on each, copy the URL.
   3. Add to deployment secrets, reference via `${DISCORD_WEBHOOK_X}`
      in the chart values.
   4. Deploy. Restart picks up new config.
4. To roll back: remove the `channels` block, keep `webhook_url`.

Helm chart additions: `values.yaml` gains a `notifications.discord.channels`
map example (commented out by default). Secret pattern documented in
`charts/server-price-tracker/README.md` follow-up.

## Open Questions

1. **`other` default behavior** — _resolved: leave unconfigured._
   Operators opt in. Falls through to fallback webhook when alerts of
   type `other` exist and `other` isn't in the map.

2. **Unknown type keys in config** — _resolved: strict._
   `channels:` keys must be one of the known ComponentTypes (`ram, drive,
   server, cpu, nic, gpu, other`). Unknown keys fail startup with a
   clear error message. Adding a new ComponentType to the codebase is a
   coordinated change anyway; strict validation surfaces config typos
   immediately.

3. **Summary mode behavior** — _resolved: per-channel summary embed._
   When `summary_only=true`, group pending alerts by component type and
   build one summary payload per type, route each to its channel.
   Components with no alerts that tick produce no summary embed.

4. **Concurrency cap on routing notifier** — _resolved: no cap for now._
   Each per-webhook `DiscordNotifier` has its own rate-limit state; with
   6 webhooks the cumulative request rate is well below Discord's per-
   webhook caps. Revisit if `/metrics` shows 429 patterns.

5. **Per-channel summary deep-links** — _resolved: yes, filter by
   component_type._ Summary embed for the RAM channel links to
   `<base>/alerts?component_type=ram`, drive channel to
   `?component_type=drive`, etc. Per-listing (non-summary) embeds keep
   their existing direct-to-eBay link; only the summary embed link
   changes.

## References

- DESIGN-0009 — Discord notifier rate limiting and embed chunking
- DESIGN-0010 — Alert review UI with pagination and search (summary mode origin)
- IMPL-0015 — implementation behind both 0009 and 0010
- `internal/notify/notifier.go` — `Notifier` interface this design composes around
- `internal/notify/discord.go` — chunking and rate-limit logic that per-type notifiers reuse unchanged

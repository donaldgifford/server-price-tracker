-- Migration 012: Add trace_id columns to extraction_queue and alerts
-- (DESIGN-0016 / IMPL-0019 Phase 2)
--
-- Trace IDs let the alert review UI deep-link from a fired alert back
-- to the full OpenTelemetry trace that produced it (ingest -> classify
-- -> extract -> normalise -> score -> alert -> notify). Both columns
-- are nullable so historical rows are unaffected and the migration
-- stays safe to apply on a live DB.
--
-- The trace_id is the W3C TraceContext trace ID encoded as a 32-char
-- hex string (no separators), per OTLP convention. TEXT is used
-- intentionally instead of UUID to match the on-the-wire format
-- without per-row casting.

ALTER TABLE extraction_queue
    ADD COLUMN IF NOT EXISTS trace_id TEXT NULL;

ALTER TABLE alerts
    ADD COLUMN IF NOT EXISTS trace_id TEXT NULL;

-- Partial index supports the alert review UI's "open trace" deep-link
-- lookup pattern: filter alerts where trace is available without
-- scanning the historical NULL backfill.
CREATE INDEX IF NOT EXISTS idx_alerts_trace_id
    ON alerts (trace_id)
    WHERE trace_id IS NOT NULL;

-- Migration 009: Add dismissed_at to alerts for the alert review UI.
--
-- Lets the operator hide alerts from the default work surface without losing
-- them (audit trail). The partial index keeps the active-queue lookup small
-- since most alerts will accumulate dismissed_at over time. The composite
-- index supports the default sort on the /alerts list page (score DESC,
-- created_at DESC).
--
-- Both indexes are cheap at current row counts (low thousands); pre-paying
-- the composite avoids a follow-up migration once row count grows.

ALTER TABLE alerts
    ADD COLUMN dismissed_at TIMESTAMPTZ NULL;

CREATE INDEX idx_alerts_dismissed_at ON alerts(dismissed_at)
    WHERE dismissed_at IS NULL;

CREATE INDEX idx_alerts_score_created ON alerts(score DESC, created_at DESC);

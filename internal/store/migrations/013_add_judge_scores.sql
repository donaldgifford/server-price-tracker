-- 013_add_judge_scores.sql
--
-- IMPL-0019 Phase 5 — async LLM-as-judge worker.
--
-- judge_scores stores the LLM-as-judge verdict for each fired alert so
-- the operator can compare the judge's quality grade against the
-- operator-truth `operator_dismissed` Langfuse score that lands when an
-- alert is dismissed in the review UI. Postgres is the durable store;
-- Langfuse is best-effort (the per-trace Score write goes through the
-- buffered client so a dropped Langfuse generation never blocks the
-- judge worker).
--
-- alert_id is the primary key — exactly one judge verdict per alert,
-- ever. Re-running the worker is idempotent because the worker
-- pre-filters on `WHERE alert_id NOT IN (SELECT alert_id FROM
-- judge_scores)`. Manual re-judge is a DELETE + worker re-run.
CREATE TABLE IF NOT EXISTS judge_scores (
    alert_id        TEXT PRIMARY KEY REFERENCES alerts(id) ON DELETE CASCADE,
    score           DOUBLE PRECISION NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
    reason          TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cost_usd        DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    judged_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS judge_scores_judged_at_idx
    ON judge_scores (judged_at DESC);

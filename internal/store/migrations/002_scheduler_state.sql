-- Job run history for scheduler observability and crash recovery.
-- status: running | succeeded | failed | crashed
CREATE TABLE job_runs (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name      TEXT        NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    status        TEXT        NOT NULL DEFAULT 'running',
    error_text    TEXT,
    rows_affected INT
);

CREATE INDEX job_runs_name_started ON job_runs (job_name, started_at DESC);

-- Distributed scheduler lock table (pool-safe alternative to pg advisory locks).
-- Acquire: INSERT ... ON CONFLICT DO UPDATE ... WHERE expires_at < now() RETURNING job_name
-- Release: DELETE WHERE job_name = $1 AND lock_holder = $2
-- A crashed holder's lock auto-expires at expires_at so the next run is never blocked.
CREATE TABLE scheduler_locks (
    job_name    TEXT        PRIMARY KEY,
    locked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    lock_holder TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

-- Track last ingestion timestamp per watch.
ALTER TABLE watches ADD COLUMN last_polled_at TIMESTAMPTZ;

-- Idempotent notification tracking.
CREATE TABLE notification_attempts (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id     UUID        NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    succeeded    BOOL        NOT NULL,
    http_status  INT,
    error_text   TEXT
);

CREATE INDEX notification_attempts_alert ON notification_attempts (alert_id, attempted_at DESC);

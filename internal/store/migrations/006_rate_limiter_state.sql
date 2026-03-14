CREATE TABLE rate_limiter_state (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tokens_used    INT         NOT NULL,
    daily_limit    INT         NOT NULL,
    reset_at       TIMESTAMPTZ NOT NULL,
    synced_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Only one row ever; use a partial unique index to enforce it.
CREATE UNIQUE INDEX rate_limiter_state_singleton ON rate_limiter_state ((true));

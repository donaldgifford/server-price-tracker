CREATE TABLE extraction_queue (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id   UUID        NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    priority     INT         NOT NULL DEFAULT 0,   -- 1=re-extract, 0=new
    enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at   TIMESTAMPTZ,
    claimed_by   TEXT,                              -- pod name or goroutine ID
    completed_at TIMESTAMPTZ,
    attempts     INT         NOT NULL DEFAULT 0,
    error_text   TEXT
);

-- Partial unique index for idempotent enqueue: only one pending job per listing.
CREATE UNIQUE INDEX extraction_queue_listing_pending
    ON extraction_queue (listing_id)
    WHERE completed_at IS NULL;

-- Partial index for fast queue dequeue (only unclaimed, uncompleted rows).
CREATE INDEX extraction_queue_dequeue
    ON extraction_queue (priority DESC, enqueued_at ASC)
    WHERE completed_at IS NULL AND claimed_at IS NULL;

-- Drop unique constraint that prevents re-alerting on a returned deal.
ALTER TABLE alerts DROP CONSTRAINT alerts_watch_id_listing_id_key;

-- Partial unique index: only one PENDING alert per (watch, listing) at a time.
-- Once an alert is notified, the listing can alert again.
CREATE UNIQUE INDEX alerts_pending_unique
    ON alerts (watch_id, listing_id)
    WHERE notified = false;

-- Cooldown: prevent re-alerting the same listing within 24h of last notification.
-- Enforced in application logic, not schema (avoids DDL complexity).

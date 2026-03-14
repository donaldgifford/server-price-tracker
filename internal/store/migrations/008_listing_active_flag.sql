-- Add active flag to listings for soft-deactivation of stale entries.
-- Inactive listings are excluded from metrics, baselines, and scoring
-- but remain in the database for historical analysis.

BEGIN;

-- 1. Add the column (all existing listings start as active).
ALTER TABLE listings ADD COLUMN active BOOLEAN NOT NULL DEFAULT true;

-- 2. Partial index for the common active-only queries.
CREATE INDEX idx_listings_active ON listings(active) WHERE active = true;

-- 3. Mark stale unextracted listings as inactive.
UPDATE listings SET active = false
WHERE component_type IS NULL OR component_type = '';

-- 4. Recreate system_state view with active filters.
DROP VIEW IF EXISTS system_state;
CREATE VIEW system_state AS
SELECT
    (SELECT COUNT(*)               FROM watches)              AS watches_total,
    (SELECT COUNT(*)               FROM watches WHERE enabled) AS watches_enabled,
    (SELECT COUNT(*)               FROM listings WHERE active) AS listings_total,
    (SELECT COUNT(*)               FROM listings WHERE active AND (component_type IS NULL OR component_type = ''))
                                                              AS listings_unextracted,
    (SELECT COUNT(*)               FROM listings WHERE active AND score IS NULL)
                                                              AS listings_unscored,
    (SELECT COUNT(*)               FROM alerts WHERE notified = false)
                                                              AS alerts_pending,
    (SELECT COUNT(*)               FROM price_baselines)      AS baselines_total,
    (SELECT COUNT(*)               FROM price_baselines WHERE sample_count >= 10)
                                                              AS baselines_warm,
    (SELECT COUNT(*)               FROM price_baselines WHERE sample_count < 10)
                                                              AS baselines_cold,
    (SELECT COUNT(DISTINCT product_key)
        FROM listings
        WHERE active
          AND product_key IS NOT NULL
          AND product_key NOT IN (SELECT product_key FROM price_baselines))
                                                              AS product_keys_no_baseline,
    (SELECT COUNT(*)
        FROM listings
        WHERE active
          AND ((component_type = 'ram' AND (product_key IS NULL OR product_key LIKE '%:0'))
           OR (component_type = 'drive' AND product_key LIKE '%:unknown%')))
                                                              AS listings_incomplete_extraction,
    (SELECT COUNT(*)               FROM extraction_queue WHERE completed_at IS NULL)
                                                              AS extraction_queue_depth;

-- 5. Recreate recompute_baseline with active filter.
CREATE OR REPLACE FUNCTION recompute_baseline(p_product_key TEXT, p_window_days INTEGER DEFAULT 90)
RETURNS void AS $$
BEGIN
    INSERT INTO price_baselines (product_key, sample_count, p10, p25, p50, p75, p90, mean, updated_at)
    SELECT
        p_product_key,
        count(*),
        percentile_cont(0.10) WITHIN GROUP (ORDER BY unit_price),
        percentile_cont(0.25) WITHIN GROUP (ORDER BY unit_price),
        percentile_cont(0.50) WITHIN GROUP (ORDER BY unit_price),
        percentile_cont(0.75) WITHIN GROUP (ORDER BY unit_price),
        percentile_cont(0.90) WITHIN GROUP (ORDER BY unit_price),
        avg(unit_price),
        now()
    FROM (
        SELECT
            CASE
                WHEN quantity > 1 THEN (COALESCE(sold_price, price) + COALESCE(shipping_cost, 0)) / quantity
                ELSE COALESCE(sold_price, price) + COALESCE(shipping_cost, 0)
            END AS unit_price
        FROM listings
        WHERE product_key = p_product_key
          AND active = true
          AND updated_at >= now() - (p_window_days || ' days')::interval
          AND condition_norm != 'for_parts'
    ) sub
    HAVING count(*) >= 5
    ON CONFLICT (product_key) DO UPDATE SET
        sample_count = EXCLUDED.sample_count,
        p10 = EXCLUDED.p10,
        p25 = EXCLUDED.p25,
        p50 = EXCLUDED.p50,
        p75 = EXCLUDED.p75,
        p90 = EXCLUDED.p90,
        mean = EXCLUDED.mean,
        updated_at = now();
END;
$$ LANGUAGE plpgsql;

-- 6. Recreate listings_with_baseline view with active filter.
DROP VIEW IF EXISTS listings_with_baseline;
CREATE VIEW listings_with_baseline AS
SELECT
    l.*,
    b.p10 AS baseline_p10,
    b.p25 AS baseline_p25,
    b.p50 AS baseline_p50,
    b.p75 AS baseline_p75,
    b.p90 AS baseline_p90,
    b.sample_count AS baseline_samples,
    CASE
        WHEN l.quantity > 1 THEN (l.price + COALESCE(l.shipping_cost, 0)) / l.quantity
        ELSE l.price + COALESCE(l.shipping_cost, 0)
    END AS unit_price
FROM listings l
LEFT JOIN price_baselines b ON b.product_key = l.product_key
WHERE l.active = true;

COMMIT;

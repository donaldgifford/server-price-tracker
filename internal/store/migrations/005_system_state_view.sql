-- Precomputed aggregate view for Prometheus scrape.
-- Queried by SyncStateMetrics() on every ingestion cycle.
CREATE VIEW system_state AS
SELECT
    (SELECT COUNT(*)               FROM watches)              AS watches_total,
    (SELECT COUNT(*)               FROM watches WHERE enabled) AS watches_enabled,
    (SELECT COUNT(*)               FROM listings)             AS listings_total,
    (SELECT COUNT(*)               FROM listings WHERE component_type IS NULL OR component_type = '')
                                                              AS listings_unextracted,
    (SELECT COUNT(*)               FROM listings WHERE score IS NULL)
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
        WHERE product_key IS NOT NULL
          AND product_key NOT IN (SELECT product_key FROM price_baselines))
                                                              AS product_keys_no_baseline,
    (SELECT COUNT(*)
        FROM listings
        WHERE (component_type = 'ram' AND (product_key IS NULL OR product_key LIKE '%:0'))
           OR (component_type = 'drive' AND product_key LIKE '%:unknown%'))
                                                              AS listings_incomplete_extraction,
    (SELECT COUNT(*)               FROM extraction_queue WHERE completed_at IS NULL)
                                                              AS extraction_queue_depth;

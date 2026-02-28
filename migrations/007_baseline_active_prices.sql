-- Replace recompute_baseline to use active listing prices as a baseline proxy.
-- Drops the sold_at IS NOT NULL requirement; uses updated_at as the window
-- filter so any listing seen within the window period contributes to the
-- baseline, regardless of whether it has sold. COALESCE(sold_price, price)
-- means real sold data is preferred if it ever becomes available.
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

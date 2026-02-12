-- 001_initial_schema.sql
-- Initial schema for eBay Deal Tracker

BEGIN;

-- Watches: saved searches with alert configs
CREATE TABLE watches (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    search_query    TEXT NOT NULL,
    category_id     TEXT,
    component_type  TEXT NOT NULL CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'other')),
    filters         JSONB NOT NULL DEFAULT '{}',
    score_threshold INTEGER NOT NULL DEFAULT 75,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_polled_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Listings: all eBay listings we've seen
CREATE TABLE listings (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ebay_item_id            TEXT UNIQUE NOT NULL,
    title                   TEXT NOT NULL,
    item_url                TEXT NOT NULL,
    image_url               TEXT,

    -- pricing
    price                   NUMERIC(10,2) NOT NULL,
    currency                TEXT NOT NULL DEFAULT 'USD',
    shipping_cost           NUMERIC(10,2),
    listing_type            TEXT NOT NULL CHECK (listing_type IN ('auction', 'buy_it_now', 'best_offer')),

    -- seller
    seller_name             TEXT,
    seller_feedback_score   INTEGER,
    seller_feedback_pct     NUMERIC(5,2),
    seller_top_rated        BOOLEAN DEFAULT false,

    -- condition
    condition_raw           TEXT,
    condition_norm          TEXT CHECK (condition_norm IN ('new', 'like_new', 'used_working', 'for_parts', 'unknown')),

    -- extracted data
    component_type          TEXT CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'other')),
    quantity                INTEGER NOT NULL DEFAULT 1,
    attributes              JSONB NOT NULL DEFAULT '{}',
    extraction_confidence   NUMERIC(3,2),
    product_key             TEXT,

    -- scoring
    score                   INTEGER,
    score_breakdown         JSONB,

    -- timestamps
    listed_at               TIMESTAMPTZ,
    auction_end_at          TIMESTAMPTZ,
    sold_at                 TIMESTAMPTZ,
    sold_price              NUMERIC(10,2),
    first_seen_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for common query patterns
CREATE INDEX idx_listings_ebay_id ON listings(ebay_item_id);
CREATE INDEX idx_listings_component ON listings(component_type);
CREATE INDEX idx_listings_product_key ON listings(product_key);
CREATE INDEX idx_listings_score ON listings(score DESC) WHERE score IS NOT NULL;
CREATE INDEX idx_listings_attrs ON listings USING GIN(attributes);
CREATE INDEX idx_listings_first_seen ON listings(first_seen_at DESC);
CREATE INDEX idx_listings_seller_feedback ON listings(seller_feedback_score);
CREATE INDEX idx_listings_sold ON listings(sold_at DESC) WHERE sold_at IS NOT NULL;

-- Price baselines: rolling percentile stats per product category
CREATE TABLE price_baselines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_key     TEXT UNIQUE NOT NULL,
    sample_count    INTEGER NOT NULL,
    p10             NUMERIC(10,2),
    p25             NUMERIC(10,2),
    p50             NUMERIC(10,2),
    p75             NUMERIC(10,2),
    p90             NUMERIC(10,2),
    mean            NUMERIC(10,2),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_baselines_key ON price_baselines(product_key);

-- Alerts: triggered notifications
CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    watch_id        UUID NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
    listing_id      UUID NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    score           INTEGER NOT NULL,
    notified        BOOLEAN NOT NULL DEFAULT false,
    notified_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Prevent duplicate alerts for same watch+listing
    UNIQUE(watch_id, listing_id)
);

CREATE INDEX idx_alerts_watch ON alerts(watch_id);
CREATE INDEX idx_alerts_pending ON alerts(notified) WHERE notified = false;

-- Useful view: listings with their baseline context
CREATE VIEW listings_with_baseline AS
SELECT
    l.*,
    b.p10 AS baseline_p10,
    b.p25 AS baseline_p25,
    b.p50 AS baseline_p50,
    b.p75 AS baseline_p75,
    b.p90 AS baseline_p90,
    b.sample_count AS baseline_samples,
    -- Compute unit price inline
    CASE
        WHEN l.quantity > 1 THEN (l.price + COALESCE(l.shipping_cost, 0)) / l.quantity
        ELSE l.price + COALESCE(l.shipping_cost, 0)
    END AS unit_price
FROM listings l
LEFT JOIN price_baselines b ON b.product_key = l.product_key;

-- Function to recompute baselines from sold listings
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
          AND sold_at IS NOT NULL
          AND sold_at >= now() - (p_window_days || ' days')::interval
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

-- Updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_listings_updated_at BEFORE UPDATE ON listings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER trg_watches_updated_at BEFORE UPDATE ON watches
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

COMMIT;

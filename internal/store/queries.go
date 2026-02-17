package store

// SQL query constants organized by entity.
// All SQL lives here — PostgresStore methods reference these constants.

// Listing queries.
const (
	queryUpsertListing = `
		INSERT INTO listings (
			ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, condition_norm,
			quantity, listed_at, first_seen_at, updated_at
		) VALUES (
			@ebay_item_id, @title, @item_url, @image_url,
			@price, @currency, @shipping_cost, @listing_type,
			@seller_name, @seller_feedback_score, @seller_feedback_pct, @seller_top_rated,
			@condition_raw, @condition_norm,
			@quantity, @listed_at, now(), now()
		)
		ON CONFLICT (ebay_item_id) DO UPDATE SET
			title = EXCLUDED.title,
			price = EXCLUDED.price,
			currency = EXCLUDED.currency,
			shipping_cost = EXCLUDED.shipping_cost,
			listing_type = EXCLUDED.listing_type,
			seller_name = EXCLUDED.seller_name,
			seller_feedback_score = EXCLUDED.seller_feedback_score,
			seller_feedback_pct = EXCLUDED.seller_feedback_pct,
			seller_top_rated = EXCLUDED.seller_top_rated,
			condition_raw = EXCLUDED.condition_raw,
			condition_norm = EXCLUDED.condition_norm,
			quantity = EXCLUDED.quantity,
			listed_at = EXCLUDED.listed_at,
			updated_at = now()
		RETURNING id, first_seen_at, updated_at`

	queryGetListingByEbayID = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE ebay_item_id = $1`

	queryGetListingByID = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE id = $1`

	queryUpdateListingExtraction = `
		UPDATE listings SET
			component_type = $2,
			attributes = $3,
			extraction_confidence = $4,
			product_key = $5,
			updated_at = now()
		WHERE id = $1`

	queryUpdateScore = `
		UPDATE listings SET
			score = $2,
			score_breakdown = $3,
			updated_at = now()
		WHERE id = $1`

	queryListUnextractedListings = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE component_type IS NULL
		ORDER BY first_seen_at DESC
		LIMIT $1`

	queryListUnscoredListings = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE component_type IS NOT NULL AND score IS NULL
		ORDER BY first_seen_at DESC
		LIMIT $1`

	queryCountListings            = `SELECT COUNT(*) FROM listings`
	queryCountUnextractedListings = `SELECT COUNT(*) FROM listings WHERE component_type IS NULL`
	queryCountUnscoredListings    = `SELECT COUNT(*) FROM listings WHERE component_type IS NOT NULL AND score IS NULL`
)

// Watch queries.
const (
	queryCreateWatch = `
		INSERT INTO watches (
			name, search_query, category_id, component_type,
			filters, score_threshold, enabled, created_at, updated_at
		) VALUES (
			@name, @search_query, @category_id, @component_type,
			@filters, @score_threshold, @enabled, now(), now()
		)
		RETURNING id, created_at, updated_at`

	queryGetWatch = `
		SELECT id, name, search_query, category_id, component_type,
			filters, score_threshold, enabled, created_at, updated_at
		FROM watches
		WHERE id = $1`

	queryListWatchesAll = `
		SELECT id, name, search_query, category_id, component_type,
			filters, score_threshold, enabled, created_at, updated_at
		FROM watches
		ORDER BY created_at DESC`

	queryListWatchesEnabled = `
		SELECT id, name, search_query, category_id, component_type,
			filters, score_threshold, enabled, created_at, updated_at
		FROM watches
		WHERE enabled = true
		ORDER BY created_at DESC`

	queryUpdateWatch = `
		UPDATE watches SET
			name = @name,
			search_query = @search_query,
			category_id = @category_id,
			component_type = @component_type,
			filters = @filters,
			score_threshold = @score_threshold,
			enabled = @enabled,
			updated_at = now()
		WHERE id = @id`

	queryDeleteWatch = `DELETE FROM watches WHERE id = $1`

	queryCountWatches = `SELECT COUNT(*) AS total, COUNT(*) FILTER (WHERE enabled = true) AS enabled FROM watches`

	querySetWatchEnabled = `
		UPDATE watches SET
			enabled = $2,
			updated_at = now()
		WHERE id = $1`
)

// Baseline queries.
const (
	queryGetBaseline = `
		SELECT id, product_key, sample_count, p10, p25, p50, p75, p90, mean, updated_at
		FROM price_baselines
		WHERE product_key = $1`

	queryListBaselines = `
		SELECT id, product_key, sample_count, p10, p25, p50, p75, p90, mean, updated_at
		FROM price_baselines
		ORDER BY product_key`

	queryRecomputeBaseline = `SELECT recompute_baseline($1, $2)`

	queryListDistinctProductKeys = `
		SELECT DISTINCT product_key
		FROM listings
		WHERE product_key IS NOT NULL AND product_key != ''`

	queryCountBaselinesByMaturity = `
		SELECT
			COUNT(*) FILTER (WHERE sample_count < 10) AS cold,
			COUNT(*) FILTER (WHERE sample_count >= 10) AS warm
		FROM price_baselines`

	queryCountProductKeysWithoutBaseline = `
		SELECT COUNT(DISTINCT l.product_key)
		FROM listings l
		LEFT JOIN price_baselines b ON l.product_key = b.product_key
		WHERE l.product_key != '' AND l.product_key IS NOT NULL
		  AND b.product_key IS NULL`
)

// Alert queries.
const (
	queryCreateAlert = `
		INSERT INTO alerts (watch_id, listing_id, score, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (watch_id, listing_id) DO NOTHING
		RETURNING id, created_at`

	queryListPendingAlerts = `
		SELECT id, watch_id, listing_id, score, notified, notified_at, created_at
		FROM alerts
		WHERE notified = false
		ORDER BY created_at DESC`

	queryListAlertsByWatch = `
		SELECT id, watch_id, listing_id, score, notified, notified_at, created_at
		FROM alerts
		WHERE watch_id = $1
		ORDER BY created_at DESC
		LIMIT $2`

	queryMarkAlertNotified = `
		UPDATE alerts SET
			notified = true,
			notified_at = now()
		WHERE id = $1`

	queryMarkAlertsNotified = `
		UPDATE alerts SET
			notified = true,
			notified_at = now()
		WHERE id = ANY($1)`

	queryCountPendingAlerts = `SELECT COUNT(*) FROM alerts WHERE notified = false`
)

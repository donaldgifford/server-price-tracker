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
			filters, score_threshold, enabled, last_polled_at, created_at, updated_at
		FROM watches
		WHERE id = $1`

	queryListWatchesAll = `
		SELECT id, name, search_query, category_id, component_type,
			filters, score_threshold, enabled, last_polled_at, created_at, updated_at
		FROM watches
		ORDER BY created_at DESC`

	queryListWatchesEnabled = `
		SELECT id, name, search_query, category_id, component_type,
			filters, score_threshold, enabled, last_polled_at, created_at, updated_at
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

// Extraction quality queries.
const (
	queryListIncompleteExtractions = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE component_type IS NOT NULL AND (
			(component_type = 'ram' AND (product_key LIKE '%:0' OR (attributes->>'speed_mhz') IS NULL))
			OR (component_type = 'drive' AND (product_key LIKE '%:unknown%'))
		)
		ORDER BY first_seen_at DESC
		LIMIT $1`

	queryListIncompleteExtractionsForType = `
		SELECT id, ebay_item_id, title, item_url, image_url,
			price, currency, shipping_cost, listing_type,
			seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
			condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
			COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
			listed_at, sold_at, sold_price, first_seen_at, updated_at
		FROM listings
		WHERE component_type = $1 AND (
			(component_type = 'ram' AND (product_key LIKE '%:0' OR (attributes->>'speed_mhz') IS NULL))
			OR (component_type = 'drive' AND (product_key LIKE '%:unknown%'))
		)
		ORDER BY first_seen_at DESC
		LIMIT $2`

	queryCountIncompleteExtractions = `
		SELECT COUNT(*)
		FROM listings
		WHERE component_type IS NOT NULL AND (
			(component_type = 'ram' AND (product_key LIKE '%:0' OR (attributes->>'speed_mhz') IS NULL))
			OR (component_type = 'drive' AND (product_key LIKE '%:unknown%'))
		)`

	queryCountIncompleteExtractionsByType = `
		SELECT component_type, COUNT(*)
		FROM listings
		WHERE component_type IS NOT NULL AND (
			(component_type = 'ram' AND (product_key LIKE '%:0' OR (attributes->>'speed_mhz') IS NULL))
			OR (component_type = 'drive' AND (product_key LIKE '%:unknown%'))
		)
		GROUP BY component_type`
)

// Scheduler queries.
const (
	queryInsertJobRun = `
		INSERT INTO job_runs (job_name)
		VALUES ($1)
		RETURNING id`

	queryCompleteJobRun = `
		UPDATE job_runs SET
			completed_at  = now(),
			status        = $2,
			error_text    = $3,
			rows_affected = $4
		WHERE id = $1`

	queryListJobRuns = `
		SELECT id, job_name, started_at, completed_at, status,
			COALESCE(error_text, ''), rows_affected
		FROM job_runs
		WHERE job_name = $1
		ORDER BY started_at DESC
		LIMIT $2`

	queryListLatestJobRuns = `
		SELECT DISTINCT ON (job_name)
			id, job_name, started_at, completed_at, status,
			COALESCE(error_text, ''), rows_affected
		FROM job_runs
		ORDER BY job_name, started_at DESC`

	queryUpdateWatchLastPolled = `
		UPDATE watches SET last_polled_at = $2 WHERE id = $1`

	queryMarkStaleJobRunsCrashed = `
		UPDATE job_runs SET
			status       = 'crashed',
			completed_at = now()
		WHERE status = 'running' AND started_at < $1`

	queryDeleteOldJobRuns = `
		DELETE FROM job_runs WHERE started_at < now() - interval '30 days'`

	queryAcquireSchedulerLock = `
		INSERT INTO scheduler_locks (job_name, lock_holder, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (job_name) DO UPDATE
			SET locked_at   = now(),
				lock_holder = EXCLUDED.lock_holder,
				expires_at  = EXCLUDED.expires_at
			WHERE scheduler_locks.expires_at < now()
		RETURNING job_name`

	queryReleaseSchedulerLock = `
		DELETE FROM scheduler_locks WHERE job_name = $1 AND lock_holder = $2`
)

// ExtractionQueue queries.
const (
	queryEnqueueExtraction = `
		INSERT INTO extraction_queue (listing_id, priority)
		VALUES ($1, $2)
		ON CONFLICT (listing_id) WHERE completed_at IS NULL DO NOTHING`

	queryDequeueExtractions = `
		WITH claimed AS (
			SELECT id FROM extraction_queue
			WHERE completed_at IS NULL AND claimed_at IS NULL
			ORDER BY priority DESC, enqueued_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE extraction_queue
		SET claimed_at = now(), claimed_by = $1, attempts = attempts + 1
		FROM claimed
		WHERE extraction_queue.id = claimed.id
		RETURNING extraction_queue.id, extraction_queue.listing_id,
		          extraction_queue.priority, extraction_queue.enqueued_at,
		          extraction_queue.attempts`

	queryCompleteExtractionJob = `
		UPDATE extraction_queue
		SET completed_at = now(), error_text = NULLIF($2, '')
		WHERE id = $1`

	queryCountPendingExtractionJobs = `
		SELECT COUNT(*) FROM extraction_queue WHERE completed_at IS NULL`
)

// Alert queries.
const (
	queryCreateAlert = `
		INSERT INTO alerts (watch_id, listing_id, score, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (watch_id, listing_id) WHERE notified = false DO NOTHING
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

	queryHasRecentAlert = `
		SELECT EXISTS (
			SELECT 1 FROM alerts
			WHERE watch_id = $1
			  AND listing_id = $2
			  AND notified = true
			  AND notified_at > $3
		)`

	queryInsertNotificationAttempt = `
		INSERT INTO notification_attempts (alert_id, succeeded, http_status, error_text)
		VALUES ($1, $2, $3, NULLIF($4, ''))`

	queryHasSuccessfulNotification = `
		SELECT EXISTS (
			SELECT 1 FROM notification_attempts
			WHERE alert_id = $1
			  AND succeeded = true
		)`
)

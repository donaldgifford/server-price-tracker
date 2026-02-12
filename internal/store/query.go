package store

import (
	"fmt"
	"strings"
)

const (
	defaultLimit = 50
	maxLimit     = 500

	orderByScore     = "score"
	orderByPrice     = "price"
	orderByFirstSeen = "first_seen_at"
)

// validOrderBy maps allowed OrderBy values to their SQL column expressions.
var validOrderBy = map[string]string{
	orderByScore:     "score DESC NULLS LAST",
	orderByPrice:     "price ASC",
	orderByFirstSeen: "first_seen_at DESC",
}

const defaultOrderBy = "first_seen_at DESC"

const baseListingsSelect = `SELECT id, ebay_item_id, title, item_url, image_url,
	price, currency, shipping_cost, listing_type,
	seller_name, seller_feedback_score, seller_feedback_pct, seller_top_rated,
	condition_raw, COALESCE(condition_norm, 'unknown'), COALESCE(component_type, ''), quantity, COALESCE(attributes, '{}'),
	COALESCE(extraction_confidence, 0), COALESCE(product_key, ''), score, score_breakdown,
	listed_at, sold_at, sold_price, first_seen_at, updated_at
FROM listings`

const countListingsSelect = "SELECT COUNT(*) FROM listings"

// ToSQL builds the WHERE clause, ORDER BY, LIMIT, and OFFSET for a listing query.
// It returns two SQL strings (one for the data query, one for the count query)
// and the positional parameters.
func (q *ListingQuery) ToSQL() (dataSQL, countSQL string, args []any) {
	var conditions []string
	paramIdx := 1

	if q.ComponentType != nil {
		conditions = append(conditions, fmt.Sprintf("component_type = $%d", paramIdx))
		args = append(args, *q.ComponentType)
		paramIdx++
	}

	if q.MinScore != nil {
		conditions = append(conditions, fmt.Sprintf("score >= $%d", paramIdx))
		args = append(args, *q.MinScore)
		paramIdx++
	}

	if q.MaxScore != nil {
		conditions = append(conditions, fmt.Sprintf("score <= $%d", paramIdx))
		args = append(args, *q.MaxScore)
		paramIdx++
	}

	if q.ProductKey != nil {
		conditions = append(conditions, fmt.Sprintf("product_key = $%d", paramIdx))
		args = append(args, *q.ProductKey)
		paramIdx++
	}

	if q.SellerMinFB != nil {
		conditions = append(conditions, fmt.Sprintf("seller_feedback_score >= $%d", paramIdx))
		args = append(args, *q.SellerMinFB)
		paramIdx++
	}

	if len(q.Conditions) > 0 {
		placeholders := make([]string, len(q.Conditions))
		for i, c := range q.Conditions {
			placeholders[i] = fmt.Sprintf("$%d", paramIdx)
			args = append(args, c)
			paramIdx++
		}
		conditions = append(conditions, fmt.Sprintf(
			"condition_norm IN (%s)", strings.Join(placeholders, ", "),
		))
	}

	var whereClause string
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	// Order by
	orderClause := defaultOrderBy
	if q.OrderBy != "" {
		if col, ok := validOrderBy[q.OrderBy]; ok {
			orderClause = col
		}
	}

	// Limit
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset := max(q.Offset, 0)

	dataSQL = fmt.Sprintf(
		"%s%s ORDER BY %s LIMIT %d OFFSET %d",
		baseListingsSelect, whereClause, orderClause, limit, offset,
	)

	countSQL = countListingsSelect + whereClause

	return dataSQL, countSQL, args
}

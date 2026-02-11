package handlers

import (
	"fmt"
	"strconv"
	"strings"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ParseFilters parses CLI --filter flags into WatchFilters.
// Supported formats:
//
//	price_max=100.00
//	price_min=10.00
//	seller_min_feedback=500
//	seller_min_feedback_pct=95.0
//	seller_top_rated_only=true
//	conditions=used_working,new
//	attr:capacity_gb=32
//	attr:ddr_gen=eq:ddr4
//	attr:speed_mhz=min:2400
func ParseFilters(filters []string) (domain.WatchFilters, error) {
	var wf domain.WatchFilters
	wf.AttributeFilters = make(map[string]domain.AttributeFilter)

	for _, f := range filters {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			return wf, fmt.Errorf("invalid filter format %q: expected key=value", f)
		}
		key, value := parts[0], parts[1]

		if strings.HasPrefix(key, "attr:") {
			attrKey := strings.TrimPrefix(key, "attr:")
			af, err := parseAttributeFilter(value)
			if err != nil {
				return wf, fmt.Errorf("parsing attribute filter %q: %w", f, err)
			}
			wf.AttributeFilters[attrKey] = af
			continue
		}

		if err := parseStandardFilter(&wf, key, value); err != nil {
			return wf, err
		}
	}

	if len(wf.AttributeFilters) == 0 {
		wf.AttributeFilters = nil
	}

	return wf, nil
}

func parseStandardFilter(wf *domain.WatchFilters, key, value string) error {
	switch key {
	case "price_max":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid price_max %q: %w", value, err)
		}
		wf.PriceMax = &v
	case "price_min":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid price_min %q: %w", value, err)
		}
		wf.PriceMin = &v
	case "seller_min_feedback":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid seller_min_feedback %q: %w", value, err)
		}
		wf.SellerMinFeedback = &v
	case "seller_min_feedback_pct":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid seller_min_feedback_pct %q: %w", value, err)
		}
		wf.SellerMinFeedbackPct = &v
	case "seller_top_rated_only":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid seller_top_rated_only %q: %w", value, err)
		}
		wf.SellerTopRatedOnly = v
	case "conditions":
		for _, cond := range strings.Split(value, ",") {
			wf.Conditions = append(wf.Conditions, domain.Condition(strings.TrimSpace(cond)))
		}
	default:
		return fmt.Errorf("unknown filter key %q", key)
	}
	return nil
}

func parseAttributeFilter(value string) (domain.AttributeFilter, error) {
	var af domain.AttributeFilter

	// Check for prefixed operators: min:, max:, eq:.
	if strings.HasPrefix(value, "min:") {
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, "min:"), 64)
		if err != nil {
			return af, err
		}
		af.Min = &v
		return af, nil
	}

	if strings.HasPrefix(value, "max:") {
		v, err := strconv.ParseFloat(strings.TrimPrefix(value, "max:"), 64)
		if err != nil {
			return af, err
		}
		af.Max = &v
		return af, nil
	}

	if strings.HasPrefix(value, "eq:") {
		af.Equals = strings.TrimPrefix(value, "eq:")
		return af, nil
	}

	// Try numeric first, fall back to string exact match.
	if v, err := strconv.ParseFloat(value, 64); err == nil {
		af.Equals = v
		return af, nil
	}

	af.Equals = value
	return af, nil
}

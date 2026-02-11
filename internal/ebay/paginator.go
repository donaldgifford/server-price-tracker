package ebay

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const (
	defaultPageSize      = 200
	defaultMaxPages      = 10
	defaultFirstRunPages = 5
)

// ListingChecker determines whether a listing already exists in the store.
type ListingChecker interface {
	GetListing(ctx context.Context, ebayID string) (*domain.Listing, error)
}

// Paginator handles paginating through eBay search results.
type Paginator struct {
	client   EbayClient
	checker  ListingChecker
	logger   *log.Logger
	pageSize int
	maxPages int
}

// PaginatorOption configures the Paginator.
type PaginatorOption func(*Paginator)

// WithPageSize overrides the default page size.
func WithPageSize(size int) PaginatorOption {
	return func(p *Paginator) {
		p.pageSize = size
	}
}

// WithMaxPages overrides the default max pages.
func WithMaxPages(n int) PaginatorOption {
	return func(p *Paginator) {
		p.maxPages = n
	}
}

// WithPaginatorLogger sets the logger.
func WithPaginatorLogger(l *log.Logger) PaginatorOption {
	return func(p *Paginator) {
		p.logger = l
	}
}

// NewPaginator creates a new Paginator.
func NewPaginator(
	client EbayClient,
	checker ListingChecker,
	opts ...PaginatorOption,
) *Paginator {
	p := &Paginator{
		client:   client,
		checker:  checker,
		pageSize: defaultPageSize,
		maxPages: defaultMaxPages,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// PaginateResult holds the result of a paginated search.
type PaginateResult struct {
	NewListings []domain.Listing
	TotalSeen   int
	PagesUsed   int
	StoppedAt   string // "known_listing", "max_pages", "no_more_results"
}

// Paginate fetches listings for a search query, stopping when:
// - A known listing is found (already in DB)
// - Max pages reached
// - No more results from eBay
// isFirstRun caps at defaultFirstRunPages pages for initial watch polls.
func (p *Paginator) Paginate(
	ctx context.Context,
	req SearchRequest,
	isFirstRun bool,
) (*PaginateResult, error) {
	maxPages := p.maxPages
	if isFirstRun && maxPages > defaultFirstRunPages {
		maxPages = defaultFirstRunPages
	}

	req.Limit = p.pageSize

	result := &PaginateResult{}

	for page := range maxPages {
		req.Offset = page * p.pageSize

		resp, err := p.client.Search(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("searching page %d: %w", page, err)
		}

		result.PagesUsed++

		if len(resp.Items) == 0 {
			result.StoppedAt = "no_more_results"
			return result, nil
		}

		listings := ToListings(resp.Items)
		var foundKnown bool

		for i := range listings {
			result.TotalSeen++

			existing, err := p.checker.GetListing(ctx, listings[i].EbayID)
			if err != nil {
				// Log but continue — a store error shouldn't stop ingestion.
				if p.logger != nil {
					p.logger.Warn(
						"error checking listing",
						"ebay_id",
						listings[i].EbayID,
						"err",
						err,
					)
				}
			}

			if existing != nil {
				foundKnown = true
				break
			}

			result.NewListings = append(result.NewListings, listings[i])
		}

		if foundKnown {
			result.StoppedAt = "known_listing"
			return result, nil
		}

		if !resp.HasMore {
			result.StoppedAt = "no_more_results"
			return result, nil
		}
	}

	result.StoppedAt = "max_pages"
	return result, nil
}

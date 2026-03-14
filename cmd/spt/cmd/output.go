package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// tabWriter wraps tabwriter with error tracking.
type tabWriter struct {
	*tabwriter.Writer
	err error
}

func newTabWriter(w io.Writer) *tabWriter {
	return &tabWriter{Writer: tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)}
}

func (tw *tabWriter) writef(format string, args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintf(tw.Writer, format, args...)
}

func (tw *tabWriter) finish() error {
	if tw.err != nil {
		return tw.err
	}
	return tw.Flush()
}

func printWatchTable(watches []domain.Watch) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("ID\tNAME\tQUERY\tTYPE\tTHRESHOLD\tENABLED\n")
	for i := range watches {
		tw.writef("%s\t%s\t%s\t%s\t%d\t%v\n",
			watches[i].ID,
			watches[i].Name,
			watches[i].SearchQuery,
			watches[i].ComponentType,
			watches[i].ScoreThreshold,
			watches[i].Enabled,
		)
	}
	return tw.finish()
}

func printWatchDetail(w *domain.Watch) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("ID:\t%s\n", w.ID)
	tw.writef("Name:\t%s\n", w.Name)
	tw.writef("Query:\t%s\n", w.SearchQuery)
	tw.writef("Type:\t%s\n", w.ComponentType)
	tw.writef("Threshold:\t%d\n", w.ScoreThreshold)
	tw.writef("Enabled:\t%v\n", w.Enabled)
	tw.writef("Category:\t%s\n", w.CategoryID)
	return tw.finish()
}

func printListingsTable(listings []domain.Listing) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("ID\tTITLE\tPRICE\tSCORE\tTYPE\tSELLER\n")
	for i := range listings {
		score := "-"
		if listings[i].Score != nil {
			score = fmt.Sprintf("%d", *listings[i].Score)
		}
		tw.writef("%s\t%s\t$%.2f\t%s\t%s\t%s\n",
			listings[i].ID,
			truncate(listings[i].Title, 40),
			listings[i].Price,
			score,
			listings[i].ComponentType,
			listings[i].SellerName,
		)
	}
	return tw.finish()
}

func printListingDetail(l *domain.Listing) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("ID:\t%s\n", l.ID)
	tw.writef("eBay ID:\t%s\n", l.EbayID)
	tw.writef("Title:\t%s\n", l.Title)
	tw.writef("Price:\t$%.2f %s\n", l.Price, l.Currency)
	tw.writef("Unit Price:\t$%.2f\n", l.UnitPrice())
	tw.writef("Type:\t%s\n", l.ComponentType)
	tw.writef("Condition:\t%s\n", l.ConditionNorm)
	tw.writef("Seller:\t%s (%d, %.1f%%)\n", l.SellerName, l.SellerFeedback, l.SellerFeedbackPct)
	if l.Score != nil {
		tw.writef("Score:\t%d/100\n", *l.Score)
	}
	tw.writef("URL:\t%s\n", l.ItemURL)
	tw.writef("Product Key:\t%s\n", l.ProductKey)
	return tw.finish()
}

func printBaselinesTable(baselines []domain.PriceBaseline) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("PRODUCT KEY\tSAMPLES\tP10\tP25\tP50\tP75\tP90\tMEAN\n")
	for i := range baselines {
		tw.writef("%s\t%d\t$%.2f\t$%.2f\t$%.2f\t$%.2f\t$%.2f\t$%.2f\n",
			baselines[i].ProductKey,
			baselines[i].SampleCount,
			baselines[i].P10,
			baselines[i].P25,
			baselines[i].P50,
			baselines[i].P75,
			baselines[i].P90,
			baselines[i].Mean,
		)
	}
	return tw.finish()
}

func printBaselineDetail(b *domain.PriceBaseline) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("Product Key:\t%s\n", b.ProductKey)
	tw.writef("Samples:\t%d\n", b.SampleCount)
	tw.writef("P10:\t$%.2f\n", b.P10)
	tw.writef("P25:\t$%.2f\n", b.P25)
	tw.writef("P50:\t$%.2f\n", b.P50)
	tw.writef("P75:\t$%.2f\n", b.P75)
	tw.writef("P90:\t$%.2f\n", b.P90)
	tw.writef("Mean:\t$%.2f\n", b.Mean)
	return tw.finish()
}

func printJobRunsTable(runs []domain.JobRun) error {
	tw := newTabWriter(os.Stdout)
	tw.writef("JOB\tSTATUS\tSTARTED\tCOMPLETED\tERROR\n")
	for i := range runs {
		r := &runs[i]
		completed := "-"
		if r.CompletedAt != nil {
			completed = r.CompletedAt.Format("2006-01-02 15:04:05")
		}
		errText := truncate(r.ErrorText, 40)
		tw.writef("%s\t%s\t%s\t%s\t%s\n",
			r.JobName,
			r.Status,
			r.StartedAt.Format("2006-01-02 15:04:05"),
			completed,
			errText,
		)
	}
	return tw.finish()
}

func outputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

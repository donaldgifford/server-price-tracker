package cmd

import (
	"context"
	"fmt"
	"maps"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// validComponentTypes mirrors the server-side enum accepted by the watch
// handler. Kept here to fail fast on the CLI rather than waiting for the
// server to reject the PUT.
var validComponentTypes = map[string]struct{}{
	"ram":    {},
	"drive":  {},
	"server": {},
	"cpu":    {},
	"nic":    {},
	"other":  {},
}

// watchUpdateFlags holds the parsed flag values for `spt watches update`.
// Bound to the Cobra command via pointer fields so RunE can stay terse and
// the flag-application logic stays unit-testable.
type watchUpdateFlags struct {
	name         string
	query        string
	category     string
	compType     string
	threshold    int
	enabled      bool
	filterFlag   []string
	addFilter    []string
	clearFilters bool
}

func watchUpdateCmd() *cobra.Command {
	f := &watchUpdateFlags{}

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing watch",
		Long: "Update fields on an existing watch. Only flags explicitly passed are\n" +
			"changed; everything else is preserved by fetching the current state\n" +
			"and PUT-ing the merged result.\n\n" +
			"Filter semantics:\n" +
			"  --filter        replaces the entire filter block\n" +
			"  --add-filter    merges attribute filters into the existing map\n" +
			"  --clear-filters empties the filter block\n" +
			"At most one of these three may be passed in a single invocation.",
		Example: `  # Tighten the score threshold without touching anything else
  spt watches update abc123 --threshold 80

  # Add a capacity_gb constraint without dropping existing filters
  spt watches update abc123 --add-filter "attr:capacity_gb=eq:32"

  # Replace the entire filter block
  spt watches update abc123 --filter "attr:capacity_gb=eq:64" --filter "price_max=500"

  # Clear all filters
  spt watches update abc123 --clear-filters`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return f.run(cmd, args[0])
		},
	}

	cmd.Flags().StringVar(&f.name, "name", "", "watch name")
	cmd.Flags().StringVar(&f.query, "query", "", "eBay search query")
	cmd.Flags().StringVar(&f.category, "category", "", "category id")
	cmd.Flags().
		StringVar(&f.compType, "type", "", "component type (ram, drive, server, cpu, nic, gpu, workstation, desktop, other)")
	cmd.Flags().IntVar(&f.threshold, "threshold", 0, "score threshold for alerts")
	cmd.Flags().BoolVar(&f.enabled, "enabled", false, "enable or disable the watch")
	cmd.Flags().
		StringArrayVar(&f.filterFlag, "filter", nil, "replace the entire filter block (key=value, repeatable)")
	cmd.Flags().
		StringArrayVar(&f.addFilter, "add-filter", nil, "merge attribute filters into the existing map (attr:key=value, repeatable)")
	cmd.Flags().BoolVar(&f.clearFilters, "clear-filters", false, "clear all filters on the watch")

	return cmd
}

func (f *watchUpdateFlags) run(cmd *cobra.Command, id string) error {
	if f.compType != "" {
		if _, ok := validComponentTypes[f.compType]; !ok {
			return fmt.Errorf(
				"invalid --type %q: must be one of ram, drive, server, cpu, nic, other",
				f.compType,
			)
		}
	}

	c := newClient()
	ctx := context.Background()

	current, err := c.GetWatch(ctx, id)
	if err != nil {
		return fmt.Errorf("fetching watch %s: %w", id, err)
	}

	f.applyScalarFlags(cmd, current)

	updatedFilters, err := applyFilterUpdates(
		current.Filters,
		f.filterFlag,
		f.addFilter,
		f.clearFilters,
	)
	if err != nil {
		return err
	}
	current.Filters = updatedFilters

	updated, err := c.UpdateWatch(ctx, current)
	if err != nil {
		return err
	}

	if jsonOutput() {
		return outputJSON(updated)
	}
	return printWatchDetail(updated)
}

// applyScalarFlags copies any explicitly-set scalar flag values (everything
// except the filter-related flags) into w. Cobra's Changed() is the source of
// truth for "did the user pass this", which is how we get partial-update
// semantics on top of a PUT endpoint that requires the full body.
func (f *watchUpdateFlags) applyScalarFlags(cmd *cobra.Command, w *domain.Watch) {
	flags := cmd.Flags()
	if flags.Changed("name") {
		w.Name = f.name
	}
	if flags.Changed("query") {
		w.SearchQuery = f.query
	}
	if flags.Changed("category") {
		w.CategoryID = f.category
	}
	if flags.Changed("type") {
		w.ComponentType = domain.ComponentType(f.compType)
	}
	if flags.Changed("threshold") {
		w.ScoreThreshold = f.threshold
	}
	if flags.Changed("enabled") {
		w.Enabled = f.enabled
	}
}

// applyFilterUpdates returns the new Filters value to PUT given the current
// filters and the filter-related flags. Mutually exclusive: at most one of
// {clearFlag, filterFlag, addFilter} may be set per invocation. When none is
// set, the current filters are returned unchanged.
//
// Behavior per flag:
//
//   - clearFlag=true   → zero-value WatchFilters
//   - filterFlag set   → ParseFilters(filterFlag), entire block replaces current
//   - addFilter set    → ParseFilters(addFilter), AttributeFilters merged
//     key-by-key into current.AttributeFilters; other parsed
//     fields are ignored (use --filter to replace those).
func applyFilterUpdates(
	current domain.WatchFilters,
	filterFlag, addFilter []string,
	clearFlag bool,
) (domain.WatchFilters, error) {
	set := 0
	if clearFlag {
		set++
	}
	if len(filterFlag) > 0 {
		set++
	}
	if len(addFilter) > 0 {
		set++
	}
	if set > 1 {
		return current, fmt.Errorf(
			"--filter, --add-filter, and --clear-filters are mutually exclusive",
		)
	}

	if clearFlag {
		return domain.WatchFilters{}, nil
	}

	if len(filterFlag) > 0 {
		parsed, err := handlers.ParseFilters(filterFlag)
		if err != nil {
			return current, fmt.Errorf("parsing --filter: %w", err)
		}
		return parsed, nil
	}

	if len(addFilter) > 0 {
		parsed, err := handlers.ParseFilters(addFilter)
		if err != nil {
			return current, fmt.Errorf("parsing --add-filter: %w", err)
		}
		if current.AttributeFilters == nil {
			current.AttributeFilters = make(map[string]domain.AttributeFilter)
		}
		maps.Copy(current.AttributeFilters, parsed.AttributeFilters)
		return current, nil
	}

	return current, nil
}

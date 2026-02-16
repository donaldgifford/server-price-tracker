// Package validate checks generated dashboards for correctness: PromQL syntax,
// metric name cross-referencing, and panel structure invariants.
package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	promparser "github.com/prometheus/prometheus/promql/parser"
)

// Result holds the outcome of validating one or more dashboards.
type Result struct {
	Errors   []string
	Warnings []string
}

// Ok returns true if the validation found no errors.
func (r *Result) Ok() bool { return len(r.Errors) == 0 }

func (r *Result) errorf(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

func (r *Result) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// grafanaVarRe matches Grafana template variable references like $var or ${var}.
var grafanaVarRe = regexp.MustCompile(`\$\{?\w+\}?`)

// Dashboard validates a single built dashboard.
func Dashboard(dash dashboard.Dashboard, knownMetrics map[string]bool) Result {
	var r Result
	title := "unknown"
	if dash.Title != nil {
		title = *dash.Title
	}

	allPanels := collectPanels(dash)

	checkPromQL(&r, title, allPanels)
	checkMetricNames(&r, title, allPanels, knownMetrics)
	checkUniqueIDs(&r, title, allPanels)

	return r
}

// panel is a flattened representation used during validation.
type panel struct {
	Title   string
	ID      *uint32
	Targets []prometheus.Dataquery
}

// collectPanels flattens all panels (including those inside collapsed rows).
func collectPanels(dash dashboard.Dashboard) []panel {
	var out []panel
	for _, por := range dash.Panels {
		if por.Panel != nil {
			out = append(out, extractPanel(*por.Panel))
		}
		if por.RowPanel != nil {
			for _, p := range por.RowPanel.Panels {
				out = append(out, extractPanel(p))
			}
		}
	}
	return out
}

func extractPanel(p dashboard.Panel) panel {
	title := ""
	if p.Title != nil {
		title = *p.Title
	}
	var targets []prometheus.Dataquery
	for _, t := range p.Targets {
		if pq, ok := t.(*prometheus.Dataquery); ok {
			targets = append(targets, *pq)
		}
	}
	return panel{Title: title, ID: p.Id, Targets: targets}
}

// checkPromQL parses every PromQL expression after replacing Grafana template
// variables with placeholder values.
func checkPromQL(r *Result, dashTitle string, panels []panel) {
	for _, p := range panels {
		for _, t := range p.Targets {
			expr := t.Expr
			if expr == "" {
				continue
			}
			sanitized := grafanaVarRe.ReplaceAllString(expr, ".*")
			_, err := promparser.ParseExpr(sanitized)
			if err != nil {
				r.errorf("%s > %s: invalid PromQL: %s\n  expr: %s", dashTitle, p.Title, err, expr)
			}
		}
	}
}

// checkMetricNames extracts metric names from PromQL expressions and warns if
// any are not in the known metrics registry.
func checkMetricNames(r *Result, dashTitle string, panels []panel, knownMetrics map[string]bool) {
	for _, p := range panels {
		for _, t := range p.Targets {
			expr := t.Expr
			if expr == "" {
				continue
			}
			sanitized := grafanaVarRe.ReplaceAllString(expr, ".*")
			parsed, err := promparser.ParseExpr(sanitized)
			if err != nil {
				continue // already reported by checkPromQL
			}
			for _, name := range extractMetricNames(parsed) {
				if !knownMetrics[name] {
					r.warnf("%s > %s: unknown metric %q", dashTitle, p.Title, name)
				}
			}
		}
	}
}

// extractMetricNames walks a PromQL AST and returns all metric names.
func extractMetricNames(node promparser.Node) []string {
	var names []string
	v := &metricVisitor{names: &names}
	_ = promparser.Walk(v, node, nil)
	return names
}

// metricVisitor implements promparser.Visitor to collect metric names.
type metricVisitor struct {
	names *[]string
}

func (v *metricVisitor) Visit(node promparser.Node, _ []promparser.Node) (promparser.Visitor, error) {
	if n, ok := node.(*promparser.VectorSelector); ok {
		if n.Name != "" {
			*v.names = append(*v.names, n.Name)
		}
	}
	return v, nil
}

// checkUniqueIDs verifies that all panel IDs are unique within the dashboard.
func checkUniqueIDs(r *Result, dashTitle string, panels []panel) {
	seen := make(map[uint32]string, len(panels))
	for _, p := range panels {
		if p.ID == nil {
			continue
		}
		id := *p.ID
		if prev, ok := seen[id]; ok {
			r.errorf("%s: duplicate panel ID %d: %q and %q", dashTitle, id, prev, p.Title)
		}
		seen[id] = p.Title
	}
}

// FormatResult returns a human-readable summary of validation results.
func FormatResult(name string, r Result) string {
	var b strings.Builder
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  ERROR: %s\n", e)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  WARN:  %s\n", w)
	}
	if r.Ok() && len(r.Warnings) == 0 {
		fmt.Fprintf(&b, "  %s: ok\n", name)
	}
	return b.String()
}

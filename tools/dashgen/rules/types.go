// Package rules generates Prometheus recording and alert rule files
// as Kubernetes PrometheusRule custom resources.
package rules

// PrometheusRule is a Kubernetes custom resource for Prometheus Operator.
type PrometheusRule struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   PrometheusRuleMetadata `yaml:"metadata"`
	Spec       PrometheusRuleSpec     `yaml:"spec"`
}

// PrometheusRuleMetadata holds the CR metadata fields.
type PrometheusRuleMetadata struct {
	Name   string            `yaml:"name"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

// PrometheusRuleSpec holds the rule groups.
type PrometheusRuleSpec struct {
	Groups []RuleGroup `yaml:"groups"`
}

// RuleGroup is a named collection of recording or alerting rules.
type RuleGroup struct {
	Name     string `yaml:"name"`
	Interval string `yaml:"interval,omitempty"`
	Rules    []Rule `yaml:"rules"`
}

// Rule is a single recording or alerting rule.
// Use Record for recording rules and Alert for alerting rules.
type Rule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// RuleFile is a standalone Prometheus rules file (non-CR).
type RuleFile struct {
	Groups []RuleGroup `yaml:"groups"`
}

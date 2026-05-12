package agenticgovernance

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

// FilterChain orchestrates multiple filters in sequence
type FilterChain struct {
	// Filters to apply in order
	Filters []Filter

	// Policy determines behavior when a filter blocks
	Policy ViolationPolicy

	// Metrics collector
	metrics *governanceMetrics
}

// ChainResult aggregates results from all filters
type ChainResult struct {
	// OriginalMessage is the input message
	OriginalMessage *Message

	// ModifiedMessage is the potentially altered message
	ModifiedMessage *Message

	// Allowed indicates whether the message should proceed
	Allowed bool

	// FiltersApplied lists filters that were run
	FiltersApplied []string

	// Modifications lists filters that modified the message
	Modifications []string

	// Violations contains any detected violations
	Violations []*Violation
}

// NewFilterChain creates a new filter chain
func NewFilterChain(policy ViolationPolicy, metrics *governanceMetrics) *FilterChain {
	if policy == "" {
		policy = PolicyFailFast
	}
	return &FilterChain{
		Filters: make([]Filter, 0),
		Policy:  policy,
		metrics: metrics,
	}
}

// AddFilter adds a filter to the chain
func (fc *FilterChain) AddFilter(filter Filter) {
	fc.Filters = append(fc.Filters, filter)
}

// Process runs all filters in sequence
func (fc *FilterChain) Process(ctx context.Context, msg *Message) (*ChainResult, error) {
	result := &ChainResult{
		OriginalMessage: msg,
		ModifiedMessage: msg,
		Allowed:         true,
		FiltersApplied:  make([]string, 0, len(fc.Filters)),
		Modifications:   make([]string, 0),
		Violations:      make([]*Violation, 0),
	}

	for _, filter := range fc.Filters {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		startTime := time.Now()

		filterResult, err := filter.Process(ctx, result.ModifiedMessage)
		if err != nil {
			return nil, errs.Wrap(err, "FilterChain", "Process", fmt.Sprintf("process filter %s", filter.Name()))
		}

		// Record metrics
		duration := time.Since(startTime).Seconds()
		if fc.metrics != nil {
			fc.metrics.recordFilterLatency(filter.Name(), duration)
			fc.metrics.recordFilterResult(filter.Name(), filterResult.Allowed)
		}

		result.FiltersApplied = append(result.FiltersApplied, filter.Name())

		// Handle modified message
		if filterResult.Modified != nil {
			result.ModifiedMessage = filterResult.Modified
			result.Modifications = append(result.Modifications, filter.Name())
		}

		// Handle blocked messages
		if !filterResult.Allowed {
			result.Allowed = false
			if filterResult.Violation != nil {
				result.Violations = append(result.Violations, filterResult.Violation)
			}

			// Apply violation policy
			switch fc.Policy {
			case PolicyFailFast:
				// Stop processing at first violation
				return result, nil

			case PolicyContinue:
				// Keep processing filters but mark as blocked
				continue

			case PolicyLogOnly:
				// Override block - allow the message through but log violation
				result.Allowed = true
				continue
			}
		}
	}

	return result, nil
}

// HasViolations returns true if any violations were detected
func (r *ChainResult) HasViolations() bool {
	return len(r.Violations) > 0
}

// HighestSeverity returns the highest severity among violations
func (r *ChainResult) HighestSeverity() Severity {
	if len(r.Violations) == 0 {
		return ""
	}

	severityOrder := map[Severity]int{
		SeverityCritical: 4,
		SeverityHigh:     3,
		SeverityMedium:   2,
		SeverityLow:      1,
	}

	highest := r.Violations[0].Severity
	highestOrder := severityOrder[highest]

	for _, v := range r.Violations[1:] {
		if order := severityOrder[v.Severity]; order > highestOrder {
			highest = v.Severity
			highestOrder = order
		}
	}

	return highest
}

// AddGovernanceMetadata adds governance processing metadata to the message
func (r *ChainResult) AddGovernanceMetadata() {
	if r.ModifiedMessage == nil {
		return
	}

	metadata := map[string]any{
		"filters_applied": r.FiltersApplied,
	}

	if len(r.Modifications) > 0 {
		metadata["modifications"] = r.Modifications
	}

	if len(r.Violations) > 0 {
		violationSummary := make([]map[string]any, len(r.Violations))
		for i, v := range r.Violations {
			violationSummary[i] = map[string]any{
				"filter":   v.FilterName,
				"severity": v.Severity,
				"action":   v.Action,
			}
		}
		metadata["violations"] = violationSummary
	}

	r.ModifiedMessage.SetMetadata("governance", metadata)
}

// FilterChainBuilder provides a fluent API for building filter chains
type FilterChainBuilder struct {
	chain *FilterChain
}

// NewFilterChainBuilder creates a new filter chain builder
func NewFilterChainBuilder(metrics *governanceMetrics) *FilterChainBuilder {
	return &FilterChainBuilder{
		chain: NewFilterChain(PolicyFailFast, metrics),
	}
}

// WithPolicy sets the violation policy
func (b *FilterChainBuilder) WithPolicy(policy ViolationPolicy) *FilterChainBuilder {
	b.chain.Policy = policy
	return b
}

// AddFilter adds a filter to the chain
func (b *FilterChainBuilder) AddFilter(filter Filter) *FilterChainBuilder {
	b.chain.AddFilter(filter)
	return b
}

// Build returns the constructed filter chain
func (b *FilterChainBuilder) Build() *FilterChain {
	return b.chain
}

// BuildFromConfig creates a filter chain from configuration
func BuildFromConfig(config FilterChainConfig, metrics *governanceMetrics) (*FilterChain, error) {
	chain := NewFilterChain(config.Policy, metrics)

	for _, filterConfig := range config.Filters {
		if !filterConfig.Enabled {
			continue
		}

		filter, err := createFilter(filterConfig)
		if err != nil {
			return nil, errs.WrapInvalid(err, "FilterChain", "BuildFromConfig", fmt.Sprintf("create filter %s", filterConfig.Name))
		}

		chain.AddFilter(filter)
	}

	return chain, nil
}

// createFilter creates a filter from configuration
func createFilter(config FilterConfig) (Filter, error) {
	switch config.Name {
	case "pii_redaction":
		piiConfig := config.PIIConfig
		if piiConfig == nil {
			piiConfig = DefaultPIIConfig()
		}
		return NewPIIFilter(piiConfig)

	case "injection_detection":
		injectionConfig := config.InjectionConfig
		if injectionConfig == nil {
			injectionConfig = DefaultInjectionConfig()
		}
		return NewInjectionFilter(injectionConfig)

	case "content_moderation":
		contentConfig := config.ContentConfig
		if contentConfig == nil {
			contentConfig = DefaultContentConfig()
		}
		return NewContentFilter(contentConfig)

	case "rate_limiting":
		rateLimitConfig := config.RateLimitConfig
		if rateLimitConfig == nil {
			rateLimitConfig = DefaultRateLimitConfig()
		}
		return NewRateLimiter(rateLimitConfig)

	case "tool_call_governance":
		// PIIFilter dependency is wired post-build in component.go
		// (it must reference the live PIIFilter sibling that
		// BuildFromConfig produced; the chain author cannot know the
		// pointer here). When no PII filter is configured the
		// tool_call_governance filter simply skips PII scanning for
		// non-bash/non-http_request tool args — its bash/URL
		// pattern enforcement is independent.
		return NewToolCallFilterWithConfig(nil, config.ToolCallConfig), nil

	default:
		return nil, errs.WrapInvalid(fmt.Errorf("unknown filter: %s", config.Name), "FilterChain", "createFilter", "validate filter name")
	}
}

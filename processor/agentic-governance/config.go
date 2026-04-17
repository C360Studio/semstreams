package agenticgovernance

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config holds configuration for agentic-governance processor component
type Config struct {
	FilterChain        FilterChainConfig     `json:"filter_chain" schema:"type:object,description:Filter chain configuration,category:basic"`
	Violations         ViolationConfig       `json:"violations" schema:"type:object,description:Violation handling configuration,category:basic"`
	Ports              *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
	StreamName         string                `json:"stream_name,omitempty" schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
	ConsumerNameSuffix string                `json:"consumer_name_suffix,omitempty" schema:"type:string,description:Consumer name suffix for uniqueness,category:advanced"`
}

// FilterChainConfig holds filter chain configuration
type FilterChainConfig struct {
	Policy  ViolationPolicy `json:"policy" schema:"type:string,description:Violation handling policy (fail_fast continue log_only),category:basic,default:fail_fast"`
	Filters []FilterConfig  `json:"filters" schema:"type:array,description:Ordered list of filters to apply,category:basic"`
}

// ViolationPolicy determines how the chain handles violations
type ViolationPolicy string

// Violation policies define how the filter chain handles detected violations.
const (
	// PolicyFailFast stops processing at first violation
	PolicyFailFast ViolationPolicy = "fail_fast"

	// PolicyContinue runs all filters even after violations
	PolicyContinue ViolationPolicy = "continue"

	// PolicyLogOnly logs violations but allows all content through
	PolicyLogOnly ViolationPolicy = "log_only"
)

// FilterConfig holds configuration for a single filter
type FilterConfig struct {
	Name    string `json:"name" schema:"type:string,description:Filter name (pii_redaction injection_detection content_moderation rate_limiting),category:basic"`
	Enabled bool   `json:"enabled" schema:"type:bool,description:Whether this filter is enabled,category:basic,default:true"`

	// PII filter config
	PIIConfig *PIIFilterConfig `json:"pii_config,omitempty" schema:"type:object,description:PII filter configuration,category:advanced"`

	// Injection filter config
	InjectionConfig *InjectionFilterConfig `json:"injection_config,omitempty" schema:"type:object,description:Injection filter configuration,category:advanced"`

	// Content filter config
	ContentConfig *ContentFilterConfig `json:"content_config,omitempty" schema:"type:object,description:Content filter configuration,category:advanced"`

	// Rate limiter config
	RateLimitConfig *RateLimitFilterConfig `json:"rate_limit_config,omitempty" schema:"type:object,description:Rate limit filter configuration,category:advanced"`
}

// ViolationConfig holds violation handling configuration
type ViolationConfig struct {
	Store               string     `json:"store" schema:"type:string,description:KV bucket for violations,category:basic,default:GOVERNANCE_VIOLATIONS"`
	RetentionDays       int        `json:"retention_days" schema:"type:int,description:Violation retention in days,category:basic,default:90"`
	NotifyUser          bool       `json:"notify_user" schema:"type:bool,description:Send error messages to users,category:basic,default:true"`
	NotifyAdminSeverity []Severity `json:"notify_admin_severity,omitempty" schema:"type:array,description:Severity levels that trigger admin alerts,category:basic"`
	AdminSubject        string     `json:"admin_subject,omitempty" schema:"type:string,description:NATS subject for admin alerts,category:advanced,default:admin.governance.alert"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if err := c.FilterChain.Validate(); err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "validate filter_chain")
	}

	if err := c.Violations.Validate(); err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "validate violations")
	}

	return nil
}

// Validate checks the filter chain configuration
func (fc *FilterChainConfig) Validate() error {
	// Validate policy
	switch fc.Policy {
	case PolicyFailFast, PolicyContinue, PolicyLogOnly, "":
		// Valid
	default:
		return errs.WrapInvalid(fmt.Errorf("invalid policy: %s", fc.Policy), "FilterChainConfig", "Validate", "validate policy")
	}

	// Validate each filter
	for i, filter := range fc.Filters {
		if err := filter.Validate(); err != nil {
			return errs.WrapInvalid(err, "FilterChainConfig", "Validate", fmt.Sprintf("validate filters[%d]", i))
		}
	}

	return nil
}

// Validate checks filter configuration
func (f *FilterConfig) Validate() error {
	if f.Name == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "FilterConfig", "Validate", "validate name")
	}

	// Validate filter-specific config based on name
	switch f.Name {
	case "pii_redaction":
		if f.PIIConfig != nil {
			if err := f.PIIConfig.Validate(); err != nil {
				return errs.WrapInvalid(err, "FilterConfig", "Validate", "validate pii_config")
			}
		}
	case "injection_detection":
		if f.InjectionConfig != nil {
			if err := f.InjectionConfig.Validate(); err != nil {
				return errs.WrapInvalid(err, "FilterConfig", "Validate", "validate injection_config")
			}
		}
	case "content_moderation":
		if f.ContentConfig != nil {
			if err := f.ContentConfig.Validate(); err != nil {
				return errs.WrapInvalid(err, "FilterConfig", "Validate", "validate content_config")
			}
		}
	case "rate_limiting":
		if f.RateLimitConfig != nil {
			if err := f.RateLimitConfig.Validate(); err != nil {
				return errs.WrapInvalid(err, "FilterConfig", "Validate", "validate rate_limit_config")
			}
		}
	default:
		return errs.WrapInvalid(fmt.Errorf("unknown filter name: %s", f.Name), "FilterConfig", "Validate", "validate filter name")
	}

	return nil
}

// Validate checks violation configuration
func (c *ViolationConfig) Validate() error {
	if c.RetentionDays < 0 {
		return errs.WrapInvalid(fmt.Errorf("retention_days cannot be negative"), "ViolationConfig", "Validate", "validate retention_days")
	}

	return nil
}

// DefaultConfig returns default configuration for agentic-governance processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "task_validation",
			Type:        "jetstream",
			Subject:     "agent.task.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "User task requests to validate (JetStream)",
		},
		{
			Name:        "request_validation",
			Type:        "jetstream",
			Subject:     "agent.request.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Outgoing model requests to validate (JetStream)",
		},
		{
			Name:        "response_validation",
			Type:        "jetstream",
			Subject:     "agent.response.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Incoming model responses to validate (JetStream)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "agent.task.validated",
			Type:        "jetstream",
			Subject:     "agent.task.validated.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Validated task messages",
		},
		{
			Name:        "agent.request.validated",
			Type:        "jetstream",
			Subject:     "agent.request.validated.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Validated agent requests",
		},
		{
			Name:        "agent.response.validated",
			Type:        "jetstream",
			Subject:     "agent.response.validated.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Validated agent responses",
		},
		{
			Name:        "violations",
			Type:        "jetstream",
			Subject:     "governance.violation.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Policy violations for audit (JetStream)",
		},
		{
			Name:        "user_errors",
			Type:        "nats",
			Subject:     "user.response.*",
			Required:    false,
			Description: "Error notifications to users (NATS)",
		},
	}

	return Config{
		FilterChain: FilterChainConfig{
			Policy: PolicyFailFast,
			Filters: []FilterConfig{
				{
					Name:    "pii_redaction",
					Enabled: true,
					PIIConfig: &PIIFilterConfig{
						Types:               []PIIType{PIITypeEmail, PIITypePhone, PIITypeSSN, PIITypeCreditCard, PIITypeAPIKey},
						Strategy:            RedactionLabel,
						MaskChar:            "*",
						ConfidenceThreshold: 0.85,
					},
				},
				{
					Name:    "injection_detection",
					Enabled: true,
					InjectionConfig: &InjectionFilterConfig{
						ConfidenceThreshold: 0.80,
						EnabledPatterns:     []string{"instruction_override", "jailbreak_persona", "system_injection", "delimiter_injection", "role_confusion"},
					},
				},
				{
					Name:    "content_moderation",
					Enabled: true,
					ContentConfig: &ContentFilterConfig{
						BlockThreshold: 0.90,
						WarnThreshold:  0.70,
						EnabledDefault: []string{"harmful", "illegal"},
					},
				},
				{
					Name:    "rate_limiting",
					Enabled: true,
					RateLimitConfig: &RateLimitFilterConfig{
						PerUser: RateLimitDef{
							RequestsPerMinute: 60,
							TokensPerHour:     100000,
						},
						Algorithm: AlgoTokenBucket,
						Storage: RateLimitStorage{
							Type: "memory",
						},
					},
				},
			},
		},
		Violations: ViolationConfig{
			Store:               "GOVERNANCE_VIOLATIONS",
			RetentionDays:       90,
			NotifyUser:          true,
			NotifyAdminSeverity: []Severity{SeverityCritical, SeverityHigh},
			AdminSubject:        "admin.governance.alert",
		},
		StreamName: "AGENT",
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
	}
}

// ParseDuration parses a duration string with sensible defaults
func ParseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

package otel

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config defines the configuration for the OTEL exporter component.
type Config struct {
	// Ports defines the input/output port configuration.
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// Endpoint is the OTEL collector endpoint.
	Endpoint string `json:"endpoint" schema:"type:string,description:OTEL collector endpoint,category:basic,default:localhost:4317"`

	// Protocol specifies the export protocol.
	// Supported values: "grpc", "http"
	Protocol string `json:"protocol" schema:"type:string,description:Export protocol,category:basic,default:grpc"`

	// ServiceName is the service name for OTEL traces.
	ServiceName string `json:"service_name" schema:"type:string,description:Service name for traces,category:basic,default:semstreams"`

	// ServiceVersion is the service version for OTEL traces.
	ServiceVersion string `json:"service_version" schema:"type:string,description:Service version,category:basic,default:1.0.0"`

	// ExportTraces enables trace export.
	ExportTraces bool `json:"export_traces" schema:"type:bool,description:Enable trace export,category:basic,default:true"`

	// ExportMetrics enables metric export.
	ExportMetrics bool `json:"export_metrics" schema:"type:bool,description:Enable metric export,category:basic,default:true"`

	// ExportLogs enables log export.
	ExportLogs bool `json:"export_logs" schema:"type:bool,description:Enable log export,category:basic,default:false"`

	// BatchTimeout is the timeout for batching exports.
	BatchTimeout string `json:"batch_timeout" schema:"type:string,description:Batch export timeout,category:advanced,default:5s"`

	// MaxBatchSize is the maximum number of items per batch.
	MaxBatchSize int `json:"max_batch_size" schema:"type:int,description:Maximum batch size,category:advanced,default:512"`

	// MaxExportBatchSize is the maximum number of items per export.
	MaxExportBatchSize int `json:"max_export_batch_size" schema:"type:int,description:Max export batch size,category:advanced,default:512"`

	// ExportTimeout is the timeout for each export operation.
	ExportTimeout string `json:"export_timeout" schema:"type:string,description:Export operation timeout,category:advanced,default:30s"`

	// Insecure allows insecure connections to the collector.
	Insecure bool `json:"insecure" schema:"type:bool,description:Allow insecure connections,category:security,default:true"`

	// Headers are additional headers to send with exports.
	Headers map[string]string `json:"headers" schema:"type:object,description:Additional export headers,category:advanced"`

	// ResourceAttributes are additional resource attributes.
	ResourceAttributes map[string]string `json:"resource_attributes" schema:"type:object,description:Resource attributes,category:advanced"`

	// SamplingRate is the trace sampling rate (0.0 to 1.0).
	SamplingRate float64 `json:"sampling_rate" schema:"type:float,description:Trace sampling rate,category:advanced,default:1.0"`

	// ConsumerNameSuffix adds a suffix to consumer names for uniqueness in tests.
	ConsumerNameSuffix string `json:"consumer_name_suffix" schema:"type:string,description:Suffix for consumer names,category:advanced"`

	// DeleteConsumerOnStop enables consumer cleanup on stop (for testing).
	DeleteConsumerOnStop bool `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete consumers on Stop,category:advanced,default:false"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "agent_events",
					Subject:     "agent.>",
					Type:        "jetstream",
					StreamName:  "AGENT",
					Required:    true,
					Description: "Agent lifecycle events for span collection",
				},
				{
					Name:        "tool_events",
					Subject:     "tool.result.>",
					Type:        "jetstream",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Tool results for tool span creation",
				},
			},
			Outputs: []component.PortDefinition{},
		},
		Endpoint:           "localhost:4317",
		Protocol:           "grpc",
		ServiceName:        "semstreams",
		ServiceVersion:     "1.0.0",
		ExportTraces:       true,
		ExportMetrics:      true,
		ExportLogs:         false,
		BatchTimeout:       "5s",
		MaxBatchSize:       512,
		MaxExportBatchSize: 512,
		ExportTimeout:      "30s",
		Insecure:           true,
		SamplingRate:       1.0,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Ports == nil {
		return fmt.Errorf("ports configuration is required")
	}

	if c.Protocol != "" && c.Protocol != "grpc" && c.Protocol != "http" {
		return fmt.Errorf("invalid protocol: %s (must be 'grpc' or 'http')", c.Protocol)
	}

	if c.BatchTimeout != "" {
		if _, err := time.ParseDuration(c.BatchTimeout); err != nil {
			return fmt.Errorf("invalid batch_timeout: %w", err)
		}
	}

	if c.ExportTimeout != "" {
		if _, err := time.ParseDuration(c.ExportTimeout); err != nil {
			return fmt.Errorf("invalid export_timeout: %w", err)
		}
	}

	if c.MaxBatchSize < 0 {
		return fmt.Errorf("max_batch_size must be non-negative")
	}

	if c.MaxExportBatchSize < 0 {
		return fmt.Errorf("max_export_batch_size must be non-negative")
	}

	if c.SamplingRate < 0 || c.SamplingRate > 1 {
		return fmt.Errorf("sampling_rate must be between 0.0 and 1.0")
	}

	return nil
}

// GetBatchTimeout returns the batch timeout duration.
func (c *Config) GetBatchTimeout() time.Duration {
	if c.BatchTimeout == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(c.BatchTimeout)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// GetExportTimeout returns the export timeout duration.
func (c *Config) GetExportTimeout() time.Duration {
	if c.ExportTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ExportTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

package otel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponentRejectsUnimplementedConfigFields(t *testing.T) {
	for _, field := range []string{
		"export_logs",
		"resource_attributes",
		"max_batch_size",
		"max_export_batch_size",
		"insecure",
	} {
		t.Run(field, func(t *testing.T) {
			raw := json.RawMessage(`{"endpoint":"http://localhost:4318","protocol":"http","` + field + `":true}`)
			if field == "resource_attributes" {
				raw = json.RawMessage(`{"endpoint":"http://localhost:4318","protocol":"http","resource_attributes":{"region":"test"}}`)
			}
			if strings.Contains(field, "batch_size") {
				raw = json.RawMessage(`{"endpoint":"http://localhost:4318","protocol":"http","` + field + `":1}`)
			}
			_, err := NewComponent(raw, component.Dependencies{})
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("NewComponent error = %v, want explicit unknown-field rejection", err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Ports == nil {
		t.Fatal("expected ports, got nil")
	}

	if cfg.Endpoint != "http://localhost:4318" {
		t.Errorf("expected OTLP/HTTP endpoint, got %q", cfg.Endpoint)
	}

	if cfg.Protocol != "http" {
		t.Errorf("expected protocol 'http', got %q", cfg.Protocol)
	}

	if cfg.ServiceName != "semstreams" {
		t.Errorf("expected service_name 'semstreams', got %q", cfg.ServiceName)
	}

	if !cfg.ExportTraces {
		t.Error("expected export_traces to be true")
	}

	if !cfg.ExportMetrics {
		t.Error("expected export_metrics to be true")
	}

	if cfg.SamplingRate != 1.0 {
		t.Errorf("expected sampling_rate 1.0, got %f", cfg.SamplingRate)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid http config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "unsupported grpc config",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.Protocol = "grpc"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "missing ports",
			config: Config{
				Endpoint: "http://localhost:4318",
				Protocol: "http",
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.Protocol = "websocket"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "endpoint without http scheme",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.Endpoint = "localhost:4318"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "invalid batch_timeout",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.BatchTimeout = "invalid"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "invalid export_timeout",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.ExportTimeout = "invalid"
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "sampling_rate below 0",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.SamplingRate = -0.1
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "sampling_rate above 1",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.SamplingRate = 1.5
				return cfg
			}(),
			wantErr: true,
		},
		{
			name: "zero values allowed",
			config: func() Config {
				cfg := DefaultConfig()
				cfg.SamplingRate = 0
				return cfg
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigGetBatchTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    string
	}{
		{
			name:    "empty defaults to 5s",
			timeout: "",
			want:    "5s",
		},
		{
			name:    "10 seconds",
			timeout: "10s",
			want:    "10s",
		},
		{
			name:    "1 minute",
			timeout: "1m",
			want:    "1m0s",
		},
		{
			name:    "invalid defaults to 5s",
			timeout: "invalid",
			want:    "5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Ports:        &component.PortConfig{},
				BatchTimeout: tt.timeout,
			}
			got := cfg.GetBatchTimeout()
			if got.String() != tt.want {
				t.Errorf("GetBatchTimeout() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestConfigGetExportTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    string
	}{
		{
			name:    "empty defaults to 30s",
			timeout: "",
			want:    "30s",
		},
		{
			name:    "1 minute",
			timeout: "1m",
			want:    "1m0s",
		},
		{
			name:    "10 seconds",
			timeout: "10s",
			want:    "10s",
		},
		{
			name:    "invalid defaults to 30s",
			timeout: "invalid",
			want:    "30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Ports:         &component.PortConfig{},
				ExportTimeout: tt.timeout,
			}
			got := cfg.GetExportTimeout()
			if got.String() != tt.want {
				t.Errorf("GetExportTimeout() = %s, want %s", got, tt.want)
			}
		})
	}
}

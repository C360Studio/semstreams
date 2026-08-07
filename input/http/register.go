package http

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// CreateInput is the factory function the component registry uses
// to construct an http input from raw JSON config. NewInput validates
// the merged effective config during construction.
func CreateInput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	cfg := DefaultConfig()
	if len(rawConfig) > 0 {
		type configOverride Config
		var override configOverride
		if err := component.SafeUnmarshal(rawConfig, &override); err != nil {
			return nil, errs.Wrap(err, "http-input-factory", "create", "config parse")
		}
		cfg = mergeConfig(cfg, Config(override))
	}

	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(fmt.Errorf("NATS client is required"),
			"http-input-factory", "create", "dependency validation")
	}

	return NewInput(InputDeps{
		Name:            "http-input",
		Config:          cfg,
		NATSClient:      deps.NATSClient,
		MetricsRegistry: deps.MetricsRegistry,
		Logger:          deps.Logger,
	})
}

// mergeConfig overlays user-supplied fields onto the defaults.
// Nested-pointer fields (Auth, Retry, Decoder) are replaced
// wholesale when the user supplied them — partial overrides
// inside those sub-objects must be expressed explicitly by the
// caller. Headers / Ports are passed through unchanged.
func mergeConfig(base, user Config) Config {
	out := base
	if user.URL != "" {
		out.URL = user.URL
	}
	if user.Method != "" {
		out.Method = user.Method
	}
	if user.Body != "" {
		out.Body = user.Body
	}
	if user.Interval != "" {
		out.Interval = user.Interval
	}
	if user.Timeout != "" {
		out.Timeout = user.Timeout
	}
	if user.Headers != nil {
		out.Headers = user.Headers
	}
	if user.Auth != nil {
		out.Auth = user.Auth
	}
	if user.Retry != nil {
		out.Retry = user.Retry
	}
	if user.Decoder != nil {
		out.Decoder = user.Decoder
	}
	if user.Ports != nil {
		out.Ports = user.Ports
	}
	return out
}

// Register wires the http input into the component registry.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "http_input",
		Factory:     CreateInput,
		Schema:      httpInputSchema,
		Type:        "input",
		Protocol:    "http",
		Domain:      "network",
		Description: "Generic REST polling input. Polls an HTTP endpoint on a schedule and publishes decoded JSON to NATS.",
		Version:     "1.0.0",
	})
}

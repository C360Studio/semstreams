package graphgateway

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestDefaultPortConfigUsesBindAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   Config
		wantHost string
		wantPort int
	}{
		{name: "default", config: DefaultConfig(), wantHost: "localhost", wantPort: 8080},
		{name: "custom", config: Config{BindAddress: "127.0.0.1:9191"}, wantHost: "127.0.0.1", wantPort: 9191},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.ApplyDefaults()
			port, ok := test.config.Ports.Inputs[0].Config.(component.NetworkPort)
			if !ok {
				t.Fatalf("input config = %T, want component.NetworkPort", test.config.Ports.Inputs[0].Config)
			}
			if port.Protocol != "http" || port.Host != test.wantHost || port.Port != test.wantPort {
				t.Fatalf("network port = %+v, want http %s:%d", port, test.wantHost, test.wantPort)
			}
		})
	}
}

func TestInvalidBindAddressFailsStrictPortResolution(t *testing.T) {
	t.Parallel()

	config := Config{BindAddress: "not-an-address"}
	config.ApplyDefaults()
	definition := config.Ports.Inputs[0]
	_, err := json.Marshal(component.Port{
		Name:      definition.Name,
		Direction: component.DirectionInput,
		Config:    definition.Config,
	})
	if err == nil {
		t.Fatal("strict port resolution accepted invalid bind address")
	}
}

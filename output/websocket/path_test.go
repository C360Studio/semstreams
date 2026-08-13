package websocket

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

func TestCreateOutputPathFromRawJSON(t *testing.T) {
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}

	t.Run("custom path reaches output", func(t *testing.T) {
		created, err := CreateOutput(json.RawMessage(`{"path":"/factory-proof"}`), deps)
		require.NoError(t, err)
		require.Equal(t, "/factory-proof", created.(*Output).path)
	})

	t.Run("omitted path defaults to ws", func(t *testing.T) {
		created, err := CreateOutput(json.RawMessage(`{}`), deps)
		require.NoError(t, err)
		require.Equal(t, "/ws", created.(*Output).path)
	})
}

func TestCreateOutputAcceptsValidPathOnlyServeMuxPatterns(t *testing.T) {
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}
	tests := []struct {
		name      string
		rawConfig json.RawMessage
		want      string
	}{
		{name: "root", rawConfig: json.RawMessage(`{"path":"/"}`), want: "/"},
		{name: "trailing slash", rawConfig: json.RawMessage(`{"path":"/events/"}`), want: "/events/"},
		{name: "percent escaped", rawConfig: json.RawMessage(`{"path":"/caf%C3%A9"}`), want: "/caf%C3%A9"},
		{name: "wildcard", rawConfig: json.RawMessage(`{"path":"/events/{id}"}`), want: "/events/{id}"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			created, err := CreateOutput(test.rawConfig, deps)
			require.NoError(t, err)
			require.Equal(t, test.want, created.(*Output).path)
		})
	}
}

func TestNewOutputFromConfigAcceptsValidPathOnlyServeMuxPatterns(t *testing.T) {
	for _, path := range []string{"/", "/events/", "/caf%C3%A9", "/events/{id}"} {
		t.Run(path, func(t *testing.T) {
			config := DefaultConstructorConfig()
			config.Path = path
			created, err := NewOutputFromConfig(config)
			require.NoError(t, err)
			require.Equal(t, path, created.path)
		})
	}
}

func TestCreateOutputRejectsInvalidPathFromRawJSON(t *testing.T) {
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}
	tests := []struct {
		name      string
		rawConfig json.RawMessage
	}{
		{name: "empty", rawConfig: json.RawMessage(`{"path":""}`)},
		{name: "missing leading slash", rawConfig: json.RawMessage(`{"path":"events"}`)},
		{name: "method pattern", rawConfig: json.RawMessage(`{"path":"GET /events"}`)},
		{name: "host pattern", rawConfig: json.RawMessage(`{"path":"example.com/events"}`)},
		{name: "full URL", rawConfig: json.RawMessage(`{"path":"https://example.com/events"}`)},
		{name: "whitespace", rawConfig: json.RawMessage(`{"path":"/event stream"}`)},
		{name: "control character", rawConfig: json.RawMessage(`{"path":"/events\u0001"}`)},
		{name: "invalid mux pattern", rawConfig: json.RawMessage(`{"path":"/events/{id"}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CreateOutput(test.rawConfig, deps)
			require.Error(t, err)
			require.Contains(t, err.Error(), "path")
		})
	}
}

func TestNewOutputFromConfigRejectsInvalidPath(t *testing.T) {
	for _, path := range []string{"", "events", "GET /events", "example.com/events", "https://example.com/events", "/event stream", "/events\x01", "/events/{id"} {
		t.Run(path, func(t *testing.T) {
			config := DefaultConstructorConfig()
			config.Path = path
			_, err := NewOutputFromConfig(config)
			require.Error(t, err)
			require.Contains(t, err.Error(), "path")
		})
	}
}

func TestCreateOutputRejectsRetiredRootEndpointOnly(t *testing.T) {
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}

	_, err := CreateOutput(json.RawMessage(`{"endpoint":"ws://localhost:8080/stream"}`), deps)
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint")

	created, err := CreateOutput(json.RawMessage(`{"unrelated_future_field":true}`), deps)
	require.NoError(t, err)
	require.Equal(t, "/ws", created.(*Output).path)
}

func TestCreateOutputPathDoesNotChangeListenerIdentity(t *testing.T) {
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}
	defaultRoute, err := CreateOutput(json.RawMessage(`{"path":"/ws"}`), deps)
	require.NoError(t, err)
	customRoute, err := CreateOutput(json.RawMessage(`{"path":"/graph"}`), deps)
	require.NoError(t, err)

	defaultFacts, err := defaultRoute.(*Output).OutputPorts()[0].Facts()
	require.NoError(t, err)
	customFacts, err := customRoute.(*Output).OutputPorts()[0].Facts()
	require.NoError(t, err)
	require.Equal(t, defaultFacts.ResourceID(), customFacts.ResourceID())
	defaultNetwork, ok := defaultFacts.Network()
	require.True(t, ok)
	customNetwork, ok := customFacts.Network()
	require.True(t, ok)
	require.Equal(t, defaultNetwork, customNetwork)
}

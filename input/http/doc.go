// Package http provides a REST polling input component for
// ingesting data from HTTP endpoints into SemStreams.
//
// # Overview
//
// The http input polls a configurable REST endpoint on a fixed
// schedule, decodes the response body, and publishes each decoded
// record as a BaseMessage envelope to a NATS subject. It is the
// generic counterpart to the typed-input components (UDP,
// WebSocket, GitHub webhook, …) — anything reachable over HTTP
// that emits JSON, JSON Lines, or a single JSON object is in
// scope.
//
// Authentication via bearer token or HTTP Basic, custom request
// headers, configurable timeout, and exponential-backoff retry on
// transient failures are first-class config options.
//
// # Quick start
//
//	config := http.Config{
//	    URL:      "https://api.example.org/sensors/telemetry",
//	    Interval: "30s",
//	    Auth: &http.AuthConfig{
//	        Type:  "bearer",
//	        Token: "<token>",
//	    },
//	    Decoder: &http.DecoderConfig{Mode: "json"},
//	    Ports: &component.PortConfig{
//	        Outputs: []component.PortDefinition{
//	            {Name: "out", Config: component.JetStreamPort{Subjects: []string{"sensors.telemetry"}}},
//	        },
//	    },
//	}
//	rawConfig, _ := json.Marshal(config)
//	input, err := http.CreateInput(rawConfig, deps)
//
// # Decoder modes
//
//   - "json"  — parse the response body as a single JSON object;
//     publish one BaseMessage per poll cycle.
//   - "jsonl" — parse the response body as JSON Lines; publish
//     one BaseMessage per non-empty line.
//
// Both modes wrap the decoded JSON in a [message.GenericJSONPayload]
// ("core.json.v1") and route through the payload registry — every
// publish carries the BaseMessage envelope so downstream
// processors decode through the registry without bespoke handlers.
// Raw byte passthrough is intentionally NOT a mode here: anything
// not decodable as JSON should flow through a dedicated processor
// rather than skipping the registry contract.
//
// # Authentication
//
//   - "none"   — no auth header is set (default).
//   - "bearer" — Authorization: Bearer <Token>.
//   - "basic"  — Authorization: Basic base64(Username:Password).
//
// Tokens and passwords live in the operator config today; an
// env-var substitution layer is on the roadmap. Treat config
// files containing AuthConfig material as secrets.
//
// # Retry and backoff
//
// Retries apply to network errors and 5xx responses. 4xx responses
// are NOT retried by default — they indicate misconfiguration,
// not transience. Backoff is exponential with a configurable cap.
// MaxAttempts == 1 disables retry entirely.
//
// # Scope (and what this component is NOT)
//
// This package ships REST polling. Streaming HTTP (Server-Sent
// Events) and WebSocket subscription are deliberately deferred:
//
//   - Plain WebSocket endpoints — use [input/websocket].
//   - SSE / event-stream endpoints — a follow-up extension to
//     this package or a sibling input/sse.
//   - GraphQL subscriptions — out of scope; clients construct
//     POST requests with bodies, which the Method/Body config
//     supports, but the persistent-stream semantics do not.
//
// See [ADR-044] for the framework / sister-repo split rationale
// and the dependency chain that places this component in Phase 4.
//
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
// [message.GenericJSONPayload]: ../../message
package http

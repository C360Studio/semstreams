package graphgateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semstreams/graph/readiness"
)

func newSurfaceComponent(keys []string) *Component {
	cfg := DefaultConfig()
	cfg.ReadinessKeys = keys
	return &Component{config: cfg, logger: slog.New(slog.DiscardHandler)}
}

// TestReadinessSurface_NoAggregateVerdictOnTheWire is the guard for the requirement's
// central prohibition: the surface MUST NOT return a computed aggregate verdict,
// because the key list belongs to the consumer and this gateway watches whatever an
// operator configured — not what any given consumer depends on.
//
// Asserted on the WIRE rather than the struct, because a future field would be added
// to the struct and this is the thing that must never appear to callers.
func TestReadinessSurface_NoAggregateVerdictOnTheWire(t *testing.T) {
	c := newSurfaceComponent(nil)

	rec := httptest.NewRecorder()
	c.handleReadiness(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Anything that reads as a fleet-wide verdict is forbidden at the top level.
	for _, banned := range []string{"ready", "proceed", "ok", "covered", "verdict", "healthy"} {
		if _, present := wire[banned]; present {
			t.Errorf("aggregate verdict field %q present: %s", banned, rec.Body.String())
		}
	}
	if _, ok := wire["producers"]; !ok {
		t.Errorf("expected per-producer rows: %s", rec.Body.String())
	}
}

// TestReadinessSurface_UnconfiguredReportsEmptyNotReady pins the honest answer when no
// keys are configured: an empty list. NOT an error, and emphatically NOT an implied
// "everything is ready" — a caller that saw a 200 with no rows must not read that as
// health.
func TestReadinessSurface_UnconfiguredReportsEmptyNotReady(t *testing.T) {
	c := newSurfaceComponent(nil)

	rec := httptest.NewRecorder()
	c.handleReadiness(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))

	var body readinessSurfaceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Producers) != 0 {
		t.Errorf("got %d producers, want 0", len(body.Producers))
	}
	// The empty list must serialize as [] rather than null, so a consumer iterating
	// it does not have to special-case a nil.
	if got := rec.Body.String(); !jsonHasEmptyProducers(t, got) {
		t.Errorf("producers must serialize as an empty array: %s", got)
	}
}

func jsonHasEmptyProducers(t *testing.T, body string) bool {
	t.Helper()
	var wire struct {
		Producers *[]readiness.Dump `json:"producers"`
	}
	if err := json.Unmarshal([]byte(body), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return wire.Producers != nil && len(*wire.Producers) == 0
}

// TestReadinessSurface_RejectsNonGET keeps a read-only surface read-only.
func TestReadinessSurface_RejectsNonGET(t *testing.T) {
	c := newSurfaceComponent(nil)

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		rec := httptest.NewRecorder()
		c.handleReadiness(rec, httptest.NewRequest(method, "/readiness", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET", method, allow)
		}
	}
}

// TestJoinGatewayPath pins the prefix joining, including the double-slash collapse the
// other route registrations each hand-roll.
func TestJoinGatewayPath(t *testing.T) {
	tests := []struct {
		prefix, path, want string
	}{
		{"", "/readiness", "/readiness"},
		{"/graph", "/readiness", "/graph/readiness"},
		{"/graph/", "/readiness", "/graph/readiness"},
		{"graph", "/readiness", "/graph/readiness"},
		{"", "", "/"},
	}
	for _, tt := range tests {
		if got := joinGatewayPath(tt.prefix, tt.path); got != tt.want {
			t.Errorf("joinGatewayPath(%q, %q) = %q, want %q", tt.prefix, tt.path, got, tt.want)
		}
	}
}

// TestReadinessSurface_ConfigRoundTrip keeps the two new operator-facing config fields
// surviving a JSON round trip — this repo has been bitten by config that validates,
// publishes a schema, and is then silently dropped by a factory overlay.
func TestReadinessSurface_ConfigRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReadinessKeys = []string{readiness.KeyGraphIngest, readiness.KeyRule}
	cfg.ReadinessPath = "/ops/readiness"

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.ReadinessKeys) != 2 ||
		got.ReadinessKeys[0] != readiness.KeyGraphIngest ||
		got.ReadinessKeys[1] != readiness.KeyRule {
		t.Errorf("ReadinessKeys = %v, want the two configured keys", got.ReadinessKeys)
	}
	if got.ReadinessPath != "/ops/readiness" {
		t.Errorf("ReadinessPath = %q, want /ops/readiness", got.ReadinessPath)
	}

	// Both are omitempty: a deployment that does not use the surface must not gain
	// new keys in its serialized config.
	bare, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(bare, &wire); err != nil {
		t.Fatalf("unmarshal bare: %v", err)
	}
	for _, k := range []string{"readiness_keys", "readiness_path"} {
		if _, present := wire[k]; present {
			t.Errorf("%q present on a default config: %s", k, bare)
		}
	}
}

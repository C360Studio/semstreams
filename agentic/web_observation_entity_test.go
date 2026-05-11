package agentic

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestTryWebObservationEntityID_HappyPath(t *testing.T) {
	id, canon, err := TryWebObservationEntityID("acme", "ops", "https://pkg.go.dev/net/http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !message.IsValidEntityID(id) {
		t.Errorf("id %q is not a valid 6-part entity ID", id)
	}
	parts := strings.Split(id, ".")
	if len(parts) != 6 {
		t.Fatalf("id %q does not split into 6 parts", id)
	}
	if parts[0] != "acme" || parts[1] != "ops" {
		t.Errorf("org/platform parts = %q/%q, want acme/ops", parts[0], parts[1])
	}
	if parts[2] != "agent" || parts[3] != "web" || parts[4] != "observation" {
		t.Errorf("domain.system.type = %q.%q.%q, want agent.web.observation", parts[2], parts[3], parts[4])
	}
	if len(parts[5]) != webObservationInstanceLen {
		t.Errorf("instance segment %q has length %d, want %d", parts[5], len(parts[5]), webObservationInstanceLen)
	}
	if canon != "https://pkg.go.dev/net/http" {
		t.Errorf("canonical URL = %q, want https://pkg.go.dev/net/http", canon)
	}
}

// TestTryWebObservationEntityID_DedupsAcrossInputs proves the
// canonicalisation rules collapse common URL variants into one entity:
// scheme/host case, default port, trailing-slash, fragment.
func TestTryWebObservationEntityID_DedupsAcrossInputs(t *testing.T) {
	canonicalInput := "https://example.com/foo"
	wantID, wantCanon, err := TryWebObservationEntityID("acme", "ops", canonicalInput)
	if err != nil {
		t.Fatalf("canonical input failed: %v", err)
	}

	variants := []string{
		"https://EXAMPLE.com/foo",
		"HTTPS://example.com/foo",
		"https://example.com:443/foo",
		"https://example.com/foo#section",
		"  https://example.com/foo  ",
	}
	for _, v := range variants {
		gotID, gotCanon, err := TryWebObservationEntityID("acme", "ops", v)
		if err != nil {
			t.Errorf("variant %q: unexpected error: %v", v, err)
			continue
		}
		if gotID != wantID {
			t.Errorf("variant %q: id = %q, want %q (canonical=%q vs %q)", v, gotID, wantID, gotCanon, wantCanon)
		}
	}
}

// TestTryWebObservationEntityID_BareHostTrailingSlash documents that
// "/" gets stripped but internal trailing slashes don't — that distinction
// is meaningful for many APIs.
func TestTryWebObservationEntityID_BareHostTrailingSlash(t *testing.T) {
	idA, _, err := TryWebObservationEntityID("acme", "ops", "https://example.com")
	if err != nil {
		t.Fatalf("no-slash: %v", err)
	}
	idB, _, err := TryWebObservationEntityID("acme", "ops", "https://example.com/")
	if err != nil {
		t.Fatalf("trailing-slash: %v", err)
	}
	if idA != idB {
		t.Errorf("bare-host trailing slash not collapsed: %q vs %q", idA, idB)
	}

	idC, _, err := TryWebObservationEntityID("acme", "ops", "https://example.com/foo/")
	if err != nil {
		t.Fatalf("internal-trailing-slash: %v", err)
	}
	idD, _, err := TryWebObservationEntityID("acme", "ops", "https://example.com/foo")
	if err != nil {
		t.Fatalf("no-internal-trailing-slash: %v", err)
	}
	if idC == idD {
		t.Errorf("internal trailing slash should not collapse: /foo/ and /foo produced the same id %q", idC)
	}
}

// TestTryWebObservationEntityID_QueryStringPreserved ensures different
// query strings produce different observation entities. Tracking-param
// stripping and query-param sorting are intentionally out of scope for V1.
func TestTryWebObservationEntityID_QueryStringPreserved(t *testing.T) {
	idA, _, _ := TryWebObservationEntityID("acme", "ops", "https://example.com/foo?a=1")
	idB, _, _ := TryWebObservationEntityID("acme", "ops", "https://example.com/foo?a=2")
	idC, _, _ := TryWebObservationEntityID("acme", "ops", "https://example.com/foo?b=1&a=1")
	if idA == idB {
		t.Errorf("different query values collapsed to one entity: %q", idA)
	}
	if idA == idC {
		t.Errorf("different query orderings collapsed (V1 preserves order, V2 may sort): %q", idA)
	}
}

// TestTryWebObservationEntityID_Rejections covers the input-validation
// failure modes: empty/dotted org/platform/url, malformed url, missing scheme/host.
func TestTryWebObservationEntityID_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		org     string
		plat    string
		url     string
		wantSub string
	}{
		{"empty org", "", "ops", "https://example.com", "org must not be empty"},
		{"dotted org", "ac.me", "ops", "https://example.com", "must not contain dots"},
		{"empty platform", "acme", "", "https://example.com", "platform must not be empty"},
		{"dotted platform", "acme", "o.ps", "https://example.com", "must not contain dots"},
		{"empty url", "acme", "ops", "", "rawURL must not be empty"},
		{"no scheme", "acme", "ops", "example.com/foo", "no scheme"},
		{"no host", "acme", "ops", "https://", "no host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, canon, err := TryWebObservationEntityID(c.org, c.plat, c.url)
			if err == nil {
				t.Fatalf("expected error, got id=%q canon=%q", id, canon)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

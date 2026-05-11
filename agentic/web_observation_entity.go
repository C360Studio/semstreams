package agentic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// webObservationInstanceLen is the number of sha256 hex characters used
// as the instance segment. 16 hex chars = 64 bits of entropy. Long
// enough to make collisions vanishingly unlikely at any realistic graph
// scale; short enough to keep entity IDs and NATS subjects compact.
const webObservationInstanceLen = 16

// TryWebObservationEntityID returns the canonical 6-part entity ID for a
// URL observed by an agent (web_search) or fetched by an agent
// (http_request), along with the canonical URL the entity represents.
// Same URL across loops → same entity ID, so observations naturally
// dedup: rule queries against agent.web.observation entities see one
// vertex per URL with whatever predicates the system has so far
// accumulated.
//
// Format: {org}.{platform}.agent.web.observation.{sha256-hex-16}
//
// Returns ("", "", error) when org/platform are empty or contain dots,
// when rawURL fails to parse, or when the constructed ID fails
// IsValidEntityID. The Try-variant naming follows the beta.36 precedent
// for runtime tool executors: a panic in a tool handler silently kills
// the dispatch goroutine, so runtime code MUST use this error-returning
// form.
//
// Canonicalisation (V1, conservative — easily extended in a follow-up
// without changing the entity hash for URLs that didn't hit the new
// rules):
//   - lowercase scheme and host
//   - strip default port (:80 for http, :443 for https)
//   - strip fragment (#section)
//   - strip trailing slash on bare-host URLs (preserve internal slashes)
//   - preserve query string as-is (tracking-param stripping and
//     query-param sorting are V2 candidates; query-param semantics are
//     too domain-specific to canonicalise generically)
//
// Note that canonicalisation is one-way: callers who need to display
// the agent's original input URL should keep it from the tool call, not
// try to reverse the hash.
func TryWebObservationEntityID(org, platform, rawURL string) (entityID, canonicalURL string, err error) {
	if err := validatePart("org", org); err != nil {
		return "", "", fmt.Errorf("WebObservationEntityID: %w", err)
	}
	if err := validatePart("platform", platform); err != nil {
		return "", "", fmt.Errorf("WebObservationEntityID: %w", err)
	}
	if rawURL == "" {
		return "", "", fmt.Errorf("WebObservationEntityID: rawURL must not be empty")
	}

	canonicalURL, err = canonicalizeURL(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("WebObservationEntityID: %w", err)
	}

	sum := sha256.Sum256([]byte(canonicalURL))
	instance := hex.EncodeToString(sum[:])[:webObservationInstanceLen]

	id := fmt.Sprintf("%s.%s.agent.web.observation.%s", org, platform, instance)
	if !message.IsValidEntityID(id) {
		return "", "", fmt.Errorf("WebObservationEntityID: constructed id %q failed IsValidEntityID — check input values", id)
	}
	return id, canonicalURL, nil
}

// canonicalizeURL applies the V1 canonicalisation rules. Returns an
// error when rawURL is unparseable, has no scheme, or has no host —
// these are caller bugs (the http_request executor already rejects
// non-http(s) URLs before reaching this point; web_search inherits the
// provider's URL validity).
func canonicalizeURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("url %q has no scheme", rawURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url %q has no host", rawURL)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	// Lowercase host but preserve port (port is numeric so case-irrelevant;
	// strings.ToLower on host:port still works).
	u.Host = strings.ToLower(u.Host)

	// Strip default port.
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = u.Hostname()
	}

	// Strip fragment.
	u.Fragment = ""
	u.RawFragment = ""

	// Strip trailing slash on bare-host URLs ("https://example.com/" →
	// "https://example.com"). Preserve internal slashes; "/foo/" stays
	// "/foo/" because that's a meaningful distinction in many APIs.
	if u.Path == "/" {
		u.Path = ""
	}

	return u.String(), nil
}

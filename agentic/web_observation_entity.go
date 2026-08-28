package agentic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// webObservationInstanceLen is the number of sha256 hex characters used
// as the instance segment. 16 hex chars = 64 bits of entropy. Long
// enough to make collisions vanishingly unlikely at any realistic graph
// scale; short enough to keep entity IDs and NATS subjects compact.
// Derived from the family table so the length lives in one home: the
// same value binds the authority-pair budget at configuration load.
var webObservationInstanceLen = semtypes.WebObservationIdentityFamily().InstanceBytes

// CategoryWebObservation identifies graph entities that represent one
// canonical URL observed by agent tools.
const CategoryWebObservation = "web_observation"

// WebObservationMessageType returns the message.Type for a web observation
// entity — key "agentic.web_observation.v1". Registered by RegisterPayloads with
// floor content (ADR-103): stamped when the web_search / http_request tools
// birth the entity, and decodes on the fact lane as *WebObservationEntity.
func WebObservationMessageType() message.Type {
	return message.Type{Domain: Domain, Category: CategoryWebObservation, Version: SchemaVersion}
}

// WebObservationTool discriminates which agent tool observed the URL. It
// selects the triple Source and the unconditional predicate set the entity
// emits, reproducing the two former inline builders byte for byte.
type WebObservationTool string

const (
	// WebObservationToolHTTPRequest marks an observation made by fetching the
	// URL (http_request): url, fetched-at, fetched-by, content-type,
	// status-code, text, truncated.
	WebObservationToolHTTPRequest WebObservationTool = "http_request"
	// WebObservationToolWebSearch marks an observation made by a search hit
	// (web_search): url, title, snippet, source-query, observed-at, observed-by.
	WebObservationToolWebSearch WebObservationTool = "web_search"
)

const (
	// webObservationSourceHTTPRequest is the Source on http_request triples.
	webObservationSourceHTTPRequest = "agent-http-request"
	// webObservationSourceWebSearch is the Source on web_search triples.
	webObservationSourceWebSearch = "agent-web-search"
)

// WebObservationEntity is the registered Graphable payload for one canonical
// URL observed by an agent tool (ADR-103). Tool selects which fields are
// emitted; the other tool's fields are carried but ignored. Every triple
// object is a field; zero values are emitted (each tool's set is
// unconditional, exactly as the former builders were).
type WebObservationEntity struct {
	Org          string             `json:"org"`
	Platform     string             `json:"platform"`
	CanonicalURL string             `json:"canonical_url"`
	Tool         WebObservationTool `json:"tool"`
	LoopEntityID string             `json:"loop_entity_id"`

	// http_request fields.
	FetchedAt   string `json:"fetched_at,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"`
	Text        string `json:"text,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`

	// web_search fields.
	Title       string `json:"title,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	SourceQuery string `json:"source_query,omitempty"`
	ObservedAt  string `json:"observed_at,omitempty"`
}

// EntityID returns the canonical observation entity ID derived from the
// canonical URL, or "" when it cannot be formed (graph-ingest rejects an empty
// ID; a decoded payload must never panic the consumer).
func (e *WebObservationEntity) EntityID() string {
	id, _, err := TryWebObservationEntityID(e.Org, e.Platform, e.CanonicalURL)
	if err != nil {
		// entity-id-audit:classify intentional-sentinel "" line=93 column=10 surface=go-return:EntityID entity_id_invalid:empty documented web observation failure return; graph-ingest rejects an empty ID and a decoded payload must not panic
		return ""
	}
	return id
}

// Triples returns the tool's unconditional predicate set with the tool's
// Source, Confidence 1.0, and a call-time Timestamp. An unknown Tool emits
// nothing (Validate rejects it).
func (e *WebObservationEntity) Triples() []message.Triple {
	entityID := e.EntityID()
	now := time.Now()
	switch e.Tool {
	case WebObservationToolHTTPRequest:
		source := webObservationSourceHTTPRequest
		return []message.Triple{
			{Subject: entityID, Predicate: agvocab.WebURL, Object: e.CanonicalURL, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebFetchedAt, Object: e.FetchedAt, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebFetchedBy, Object: e.LoopEntityID, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebContentType, Object: e.ContentType, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebStatusCode, Object: e.StatusCode, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebText, Object: e.Text, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebTruncated, Object: e.Truncated, Source: source, Timestamp: now, Confidence: 1.0},
		}
	case WebObservationToolWebSearch:
		source := webObservationSourceWebSearch
		return []message.Triple{
			{Subject: entityID, Predicate: agvocab.WebURL, Object: e.CanonicalURL, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebTitle, Object: e.Title, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebSnippet, Object: e.Snippet, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebSourceQuery, Object: e.SourceQuery, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebObservedAt, Object: e.ObservedAt, Source: source, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: agvocab.WebObservedBy, Object: e.LoopEntityID, Source: source, Timestamp: now, Confidence: 1.0},
		}
	default:
		return nil
	}
}

// Schema implements message.Payload.
func (e *WebObservationEntity) Schema() message.Type {
	return WebObservationMessageType()
}

// Validate implements message.Payload and IS the observation writers'
// contract: a known tool, identity (a canonicalisable URL), a well-formed
// observing-loop entity ID, and the tool's own facts — http_request records
// an RFC 3339 fetch time and a status the executor would have emitted (it
// never emits 4xx/5xx: "we observed this URL's content" is not that
// observation); web_search records an RFC 3339 observation time and the
// query that produced the hit. BaseMessage.MarshalJSON refuses a payload
// that fails it; both executors delegate here before publishing.
func (e *WebObservationEntity) Validate() error {
	switch e.Tool {
	case WebObservationToolHTTPRequest, WebObservationToolWebSearch:
	default:
		return fmt.Errorf("web observation tool %q is not http_request or web_search", e.Tool)
	}
	if _, _, err := TryWebObservationEntityID(e.Org, e.Platform, e.CanonicalURL); err != nil {
		return err
	}
	if !message.IsValidEntityID(e.LoopEntityID) {
		return fmt.Errorf("loop_entity_id %q is not a well-formed 6-part entity ID (the observing loop)", e.LoopEntityID)
	}
	switch e.Tool {
	case WebObservationToolHTTPRequest:
		if _, err := time.Parse(time.RFC3339Nano, e.FetchedAt); err != nil {
			return fmt.Errorf("fetched_at %q is not an RFC 3339 timestamp: %w", e.FetchedAt, err)
		}
		if e.StatusCode < 100 || e.StatusCode >= 400 {
			return fmt.Errorf("status_code %d is outside the observed range 100-399 (http_request does not emit 4xx/5xx)", e.StatusCode)
		}
	case WebObservationToolWebSearch:
		if _, err := time.Parse(time.RFC3339Nano, e.ObservedAt); err != nil {
			return fmt.Errorf("observed_at %q is not an RFC 3339 timestamp: %w", e.ObservedAt, err)
		}
		if e.SourceQuery == "" {
			return errors.New("source_query is required for a web_search observation")
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler with the alias idiom.
func (e *WebObservationEntity) MarshalJSON() ([]byte, error) {
	type alias WebObservationEntity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON implements json.Unmarshaler with the alias idiom.
func (e *WebObservationEntity) UnmarshalJSON(data []byte) error {
	type alias WebObservationEntity
	return json.Unmarshal(data, (*alias)(e))
}

// TryWebObservationEntityID returns the canonical 6-part entity ID for a
// URL observed by an agent (web_search) or fetched by an agent
// (http_request), along with the canonical URL the entity represents.
// Same URL across loops → same entity ID, so observations naturally
// dedup: rule queries against web.agent.observation entities see one
// vertex per URL with whatever predicates the system has so far
// accumulated.
//
// Format: {org}.{platform}.web.agent.observation.{sha256-hex-16}
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

	// The web.agent.observation prefix is declared once, in the framework
	// identity-family table (ADR-102, ruled O-14); composing from it keeps
	// this builder from becoming a second spelling of the same family.
	id, err := semtypes.WebObservationIdentityFamily().EntityID(org, platform, instance)
	if err != nil {
		return "", "", fmt.Errorf("WebObservationEntityID: %w", err)
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

	// Strip userinfo. Credentials in a canonical URL would leak into the
	// entity hash AND into agent.web.url; the same URL with vs without
	// credentials would also diverge across loops, breaking dedup. The
	// agent's original (credentialed) URL stays in the trajectory for
	// audit if anyone needs it.
	u.User = nil

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

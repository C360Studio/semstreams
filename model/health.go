package model

import "time"

// EndpointStatus is the circuit-breaker state for a single endpoint.
//
// Transitions follow the standard pattern:
//
//	Closed --(error rate > threshold)--> Open
//	Open   --(cooldown elapsed)-------->  HalfOpen
//	HalfOpen --(probe succeeds)--------> Closed
//	HalfOpen --(probe fails)-----------> Open  (cooldown restarts)
type EndpointStatus int

const (
	// StatusClosed is the healthy state; requests flow normally.
	StatusClosed EndpointStatus = iota
	// StatusOpen rejects requests until the cooldown elapses. Callers
	// (typically agentic-model's fallback chain) skip Open endpoints.
	StatusOpen
	// StatusHalfOpen lets a single probe through to test recovery.
	// While in this state, additional concurrent calls are still
	// treated as if Open.
	StatusHalfOpen
)

// String returns the lowercase status name suitable for log fields and
// Prometheus label values.
func (s EndpointStatus) String() string {
	switch s {
	case StatusClosed:
		return "closed"
	case StatusOpen:
		return "open"
	case StatusHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// ErrorKind classifies a failed request for breaker accounting and
// metrics. Mirrors the categories agentic-model already records via
// recordRequestError so health policies and metrics agree on shape.
type ErrorKind string

const (
	// ErrorKindNone marks a successful result; required field on
	// successful Results so callers don't have to remember to clear it.
	ErrorKindNone ErrorKind = ""
	// ErrorKindTimeout — context deadline exceeded or transport timeout.
	ErrorKindTimeout ErrorKind = "timeout"
	// ErrorKindRateLimit — provider returned 429 (after retry budget
	// exhausted; transient retries don't reach the policy).
	ErrorKindRateLimit ErrorKind = "rate_limit"
	// ErrorKindServerError — provider returned 5xx (after retry budget
	// exhausted).
	ErrorKindServerError ErrorKind = "server_error"
	// ErrorKindNetwork — connection refused, DNS failure, TLS handshake
	// failure, etc. Distinguished from server errors because retry
	// strategy differs.
	ErrorKindNetwork ErrorKind = "network"
	// ErrorKindUnknown — fallback for errors we couldn't classify.
	// Treated like ErrorKindServerError for breaker math.
	ErrorKindUnknown ErrorKind = "unknown"
)

// Result is a single endpoint observation reported to a HealthPolicy
// after a request completes (whether or not it succeeded).
type Result struct {
	// Success is true iff the request returned a usable response after
	// any internal retry. Transient retries that ultimately succeeded
	// should be reported as Success=true with ErrorKindNone.
	Success bool
	// Kind classifies the failure for metrics and breaker tuning;
	// ErrorKindNone on success.
	Kind ErrorKind
	// Latency is wall time spent on the request from caller dispatch
	// to result. Includes retry time. Zero is acceptable when the
	// caller doesn't measure (e.g., synthetic results in tests).
	Latency time.Duration
}

// HealthStats is a snapshot of an endpoint's recent observable state.
// Returned by HealthPolicy.EndpointStats for callers that want to
// surface a richer view than the binary IsHealthy.
//
// All counters are over the policy's most recent observation window;
// they reset implicitly as the window slides.
type HealthStats struct {
	// Status is the current circuit-breaker state.
	Status EndpointStatus
	// Successes is the count of successful results in the window.
	Successes int
	// Failures is the count of failed results in the window.
	Failures int
	// ErrorRate is Failures / (Successes + Failures); 0 if no
	// observations yet.
	ErrorRate float64
	// LastFailure is the time of the most recent failure (zero if no
	// failure has been observed in the window).
	LastFailure time.Time
	// LastTransition is the time of the most recent state change.
	// Useful for "how long has this endpoint been open?" dashboards.
	LastTransition time.Time
}

// HealthPolicy tracks per-endpoint failure state and decides which
// endpoints are eligible to receive traffic. agentic-model consults
// IsHealthy when iterating its fallback chain and reports each request
// outcome via RecordResult.
//
// Implementations must be safe for concurrent use. The default
// in-process implementation is RollingWindowBreaker (model/breaker.go);
// callers can swap in their own (e.g., NATS-KV-backed for shared state
// across processes) by implementing the same surface.
type HealthPolicy interface {
	// IsHealthy returns true iff the endpoint is eligible to receive
	// new requests. Equivalent to EndpointStatus(name) != StatusOpen
	// (the half-open state allows one probe through).
	IsHealthy(endpoint string) bool

	// EndpointStatus returns the current circuit-breaker state for
	// the named endpoint. Unknown endpoints (no observations yet)
	// must return StatusClosed — new endpoints are healthy by default.
	EndpointStatus(endpoint string) EndpointStatus

	// EndpointStats returns a snapshot of the endpoint's recent
	// observation window. Unknown endpoints return a zero HealthStats
	// with Status=StatusClosed.
	EndpointStats(endpoint string) HealthStats

	// RecordResult tells the policy that endpoint just produced
	// the given result. Drives state transitions on the next call.
	// Safe to call concurrently from multiple request goroutines
	// targeting the same endpoint.
	RecordResult(endpoint string, result Result)
}

// alwaysHealthyPolicy is the no-op policy used as a fallback when no
// real HealthPolicy is configured. Every endpoint is reported as
// healthy; RecordResult is a no-op.
//
// Returned by NewAlwaysHealthyPolicy so tests and library consumers
// who don't want circuit breaking can opt out cleanly without nil-
// guarding every call site.
type alwaysHealthyPolicy struct{}

// NewAlwaysHealthyPolicy returns a HealthPolicy that always reports
// endpoints as healthy. Use this in tests or in deployments that
// explicitly want to disable circuit breaking.
func NewAlwaysHealthyPolicy() HealthPolicy { return alwaysHealthyPolicy{} }

func (alwaysHealthyPolicy) IsHealthy(string) bool                { return true }
func (alwaysHealthyPolicy) EndpointStatus(string) EndpointStatus { return StatusClosed }
func (alwaysHealthyPolicy) EndpointStats(string) HealthStats {
	return HealthStats{Status: StatusClosed}
}
func (alwaysHealthyPolicy) RecordResult(string, Result) {}

// HealthAwareRegistry embeds RegistryReader with a HealthPolicy view.
// External consumers (e.g., a sidecar dispatcher wanting to consult
// circuit-breaker state before routing) can take this richer interface
// instead of plumbing the policy and registry separately.
//
// agentic-model wraps its (registry, policy) pair into one of these
// for callers that want the combined shape; raw *Registry instances
// don't satisfy it, but ComposeHealth(r, p) returns one.
type HealthAwareRegistry interface {
	RegistryReader
	HealthPolicy
}

// healthAwareRegistry is the trivial composition adapter. The two
// halves are independent — Registry doesn't know about HealthPolicy
// internals, HealthPolicy doesn't know which registry it tracks.
type healthAwareRegistry struct {
	RegistryReader
	HealthPolicy
}

// ComposeHealth wraps a RegistryReader and HealthPolicy into a single
// HealthAwareRegistry. Useful for callers who pass one value through
// dependency injection but want both shapes available.
//
// Panics if r is nil. If p is nil, the always-healthy fallback is
// substituted so health methods don't have to nil-guard their callers.
func ComposeHealth(r RegistryReader, p HealthPolicy) HealthAwareRegistry {
	if r == nil {
		panic("model.ComposeHealth: nil RegistryReader")
	}
	if p == nil {
		p = NewAlwaysHealthyPolicy()
	}
	return &healthAwareRegistry{RegistryReader: r, HealthPolicy: p}
}

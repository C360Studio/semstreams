// Package natsclient — header-classified handler errors (gh#93).
//
// Handler errors from SubscribeForRequests handlers, and from direct
// msg.Respond callers that opt in, travel as wire headers:
//
//	X-Status: error
//	X-Error-Class: transient | invalid | fatal
//
// The reply body keeps the legacy "error: <msg>" shape unchanged so
// existing prefix-sniffing callers (pathrag.go, graphrag.go, etc.) keep
// working unmodified — dual encoding is the load-bearing
// backward-compat contract. Phase 4 (deferred post-1.0) drops the
// legacy body shape; until then headers are the new-callers signal,
// body is the old-callers signal, and both carry the same information.
//
// Caller migration:
//
//	OLD: data, err := c.Request(ctx, subj, body, timeout)
//	     if err != nil { /* transport */ }
//	     if bytes.HasPrefix(data, []byte("error: ")) { /* handler */ }
//
//	NEW: data, err := c.RequestClassified(ctx, subj, body, timeout)
//	     if err != nil {
//	         // err may be transport OR classified handler error
//	         if errs.IsInvalid(err) { /* 400 */ }
//	         if errs.IsTransient(err) { /* retry */ }
//	     }
//
// Handler migration:
//
//	OLD: msg.Respond([]byte("error: " + err.Error()))
//	NEW: natsclient.RespondError(msg, err)
//
// HTTP semantics (400 vs 404) belong at the gateway layer, not the
// wire layer — "not found" maps to ErrorClass=invalid at this layer;
// gateways disambiguate with a small body-substring check or a
// per-subject convention. See feedback_natsclient_error_payload_convention.md.
//
// # Footgun warning — plain Request() is still a silent-corruption surface
//
// Phase 1 is ADDITIVE. The plain Request() and RequestWithHeaders()
// methods are UNCHANGED — they do NOT inspect X-Status / X-Error-Class
// headers, and they return the legacy "error: <msg>" body verbatim
// with err == nil when the handler errors. New code that does
//
//	data, err := c.Request(...)
//	if err != nil { return err }
//	json.Unmarshal(data, &resp)        // SILENT CORRUPTION on handler error
//
// will silently mis-decode handler errors as success data. This is
// the exact bug class that shipped three times in the beta.86 cycle
// (commit c626854 fixed FindSimilar + searchEntitiesSemantic; commit
// 895ec44 fixed searchGraph's fallback adapter — different shape,
// same class). New callers MUST use RequestClassified, OR explicitly
// check bytes.HasPrefix(data, []byte("error: ")) before unmarshal.
// See feedback_silent_handler_error_payload_audit.md for the audit
// pattern.
//
// # Sentinel chains do not survive the wire boundary
//
// classifiedFromHeader reconstructs a fresh *errs.ClassifiedError
// from the header value + body message. The original handler's
// sentinel chain (e.g. an inner jetstream.ErrKeyNotFound, or any
// custom errors.Is-friendly sentinel) is LOST in transit. Callers
// that previously branched on errors.Is(err, sentinel) need to
// substring-match the body message instead, OR move the
// sentinel-distinction logic to the handler side and surface the
// distinction through a different mechanism (separate subject,
// distinct ErrorClass, response-payload field). The ErrorClass
// taxonomy (invalid / transient / fatal) is the ONLY structured
// signal that round-trips today.
//
// ADR-060 delivers sentinel-Is parity for the one control-flow code
// that needs it: the X-Error-Code header carries the stable machine
// Code, classifiedFromHeader sets it on the reconstructed
// *errs.ClassifiedError, and (*ClassifiedError).Is matches it by Code —
// so errors.Is(err, errs.ErrRevisionMismatch) round-trips the wire. The
// general discriminator is ce.Code (via errors.As); only revision_mismatch
// gets a named sentinel (no other code has a looping consumer). This
// supersedes the earlier deferred X-Error-Sentinel-header idea.
package natsclient

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Header keys for the header-classified error convention. New callers
// branch on these; legacy callers fall through to the "error: " body
// prefix as before.
const (
	// HeaderStatus is set to HeaderStatusError on reply messages that
	// represent a handler-side failure. Absent on success replies.
	HeaderStatus = "X-Status"

	// HeaderErrorClass carries the pkg/errs.ErrorClass value as a
	// lowercase string: "transient", "invalid", or "fatal". Set
	// only when HeaderStatus == HeaderStatusError.
	HeaderErrorClass = "X-Error-Class"

	// HeaderErrorCode carries the ADR-060 stable machine Code for the
	// failure (the graph.ErrorCode* values: "entity_not_found",
	// "revision_mismatch", ...). Additive over the gh#93 header set:
	// legacy callers ignore it; ClassifyReply reads it into
	// (*errs.ClassifiedError).Code so errors.Is(err, ErrRevisionMismatch)
	// and ce.Code discrimination work across the wire. Set ONLY when the
	// handler error carries a non-empty Code, so existing uncoded handler
	// errors are byte-for-byte unchanged on the wire (the reply body is
	// also unchanged — Code rides the header; the standard error body for
	// Detail lands with the breaking PR).
	HeaderErrorCode = "X-Error-Code"
)

// Values used in HeaderStatus / HeaderErrorClass.
const (
	HeaderStatusError = "error"

	ErrorClassTransient = "transient"
	ErrorClassInvalid   = "invalid"
	ErrorClassFatal     = "fatal"
)

// legacyErrorBodyPrefix is the body-prefix convention the framework
// has used since pre-#93. Callers can still sniff this shape for the
// duration of the dual-encoding window (Phase 4 retires it).
var legacyErrorBodyPrefix = []byte("error: ")

// errMissingReplySubject is returned by RespondError when the inbound
// message had no reply subject — there's no one to respond to.
var errMissingReplySubject = errors.New("natsclient: message has no reply subject")

// RespondError writes a header-classified error reply to msg. The
// reply body keeps the legacy "error: <msg>" shape; headers carry
// the new X-Status / X-Error-Class signal.
//
// Used by SubscribeForRequests internally + by direct-msg.Respond
// handlers that opt in to the convention.
//
// When to reach for RespondError vs (*Client).ReplyError:
//   - Handler has *nats.Msg in scope (most common — direct Subscribe
//     callback): use RespondError(msg, err). Free function; reads
//     the reply subject off msg.
//   - Handler has only a reply subject + *Client (e.g. deferred-reply
//     forwarder): use c.ReplyError(ctx, replyTo, err). Method;
//     publishes via the client connection.
//
// Returns nil + no-op when err is nil (treat as success — caller
// should have used msg.Respond with success data). Returns
// errMissingReplySubject when the inbound message had no reply
// subject; caller can ignore (the request was fire-and-forget).
func RespondError(msg *nats.Msg, err error) error {
	if err == nil {
		return nil
	}
	if msg.Reply == "" {
		return errMissingReplySubject
	}

	reply := nats.NewMsg(msg.Reply)
	reply.Header = make(nats.Header)
	reply.Header.Set(HeaderStatus, HeaderStatusError)
	reply.Header.Set(HeaderErrorClass, classForHeader(err))
	if code := codeForHeader(err); code != "" {
		reply.Header.Set(HeaderErrorCode, code)
	}
	reply.Data = []byte(legacyErrorBodyPrefix)
	reply.Data = append(reply.Data, err.Error()...)

	return msg.RespondMsg(reply)
}

// ReplyError sends a header-classified error reply via the
// client's Publish path. Companion to Reply / ReplyWithHeaders for
// handlers that don't have the inbound *nats.Msg in scope.
//
// Returns nil + no-op when err is nil OR replyTo is empty.
func (c *Client) ReplyError(ctx context.Context, replyTo string, err error) error {
	if err == nil || replyTo == "" {
		return nil
	}

	body := append([]byte(legacyErrorBodyPrefix), err.Error()...)
	headers := map[string]string{
		HeaderStatus:     HeaderStatusError,
		HeaderErrorClass: classForHeader(err),
	}
	if code := codeForHeader(err); code != "" {
		headers[HeaderErrorCode] = code
	}
	return c.ReplyWithHeaders(ctx, replyTo, body, headers)
}

// ClassifyReply inspects a reply message and returns either the
// success body (when no error signal is present) or a classified
// error suitable for branching with errs.IsInvalid / IsTransient /
// IsFatal.
//
// Detection order:
//  1. X-Status: error header → reconstruct from X-Error-Class.
//  2. Legacy "error: " body prefix → classify as invalid (the
//     default for old handlers; gateways doing finer-grained mapping
//     can substring-check the unwrapped message).
//  3. Otherwise → success: return body, nil.
//
// The reconstructed classified error reads
// "natsclient.ClassifyReply: handler error failed: <original>" so its
// provenance is obvious in logs while errs.Is* still returns the
// correct class.
func ClassifyReply(msg *nats.Msg) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	if msg.Header.Get(HeaderStatus) == HeaderStatusError {
		// Header-driven path. Strip the legacy prefix only if
		// present so the inner message is clean; future Phase 4
		// handlers may stop emitting the prefix entirely, in which
		// case this becomes a pure pass-through. Either way the
		// reconstructed error carries the original handler text.
		body := msg.Data
		if bytes.HasPrefix(body, legacyErrorBodyPrefix) {
			body = bytes.TrimPrefix(body, legacyErrorBodyPrefix)
		}
		return nil, classifiedFromHeader(
			msg.Header.Get(HeaderErrorClass),
			msg.Header.Get(HeaderErrorCode),
			string(body),
		)
	}

	if bytes.HasPrefix(msg.Data, legacyErrorBodyPrefix) {
		// Pre-#93 handler — no header signal. Conservative default:
		// classify as invalid so callers don't loop-retry on an
		// unknown shape. Gateways doing finer-grained mapping can
		// substring-check the unwrapped message. No code on the legacy
		// path (pre-ADR-060 handlers don't carry one).
		body := bytes.TrimPrefix(msg.Data, legacyErrorBodyPrefix)
		return nil, classifiedFromHeader(ErrorClassInvalid, "", string(body))
	}

	return msg.Data, nil
}

// RequestClassified is the recommended caller-side replacement for
// Request + body-prefix sniffing. It performs the request, then
// runs the reply through ClassifyReply so the returned error covers
// both transport and handler failure modes uniformly.
//
// Transport failures (no responders, timeout) are returned as the
// underlying error and classify as ErrorTransient via pkg/errs.IsTransient
// — caller's existing retry logic on IsTransient continues to fire.
//
// Handler failures arrive as a *errs.ClassifiedError reconstructed
// from the X-Error-Class header (or the legacy body prefix as
// fallback). Caller branches on errs.IsInvalid / IsTransient / IsFatal.
//
// Use this for QUERIES. For MUTATIONS where the responder is
// idempotent AND emits classified errors, use
// RequestWithRetryClassified — retrying on a hung query masks
// responder problems as latency. See
// docs/operations/07-nats-request-retry.md for the full rule.
func (c *Client) RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	msg, err := c.RequestWithHeaders(ctx, subject, data, nil, timeout)
	if err != nil {
		return nil, err
	}
	return ClassifyReply(msg)
}

// RequestWithRetryClassified is the retry-aware sibling of
// RequestClassified. It runs RequestWithRetry's retry-on-transport-
// failure loop and then runs the final reply through ClassifyReply
// so the returned error covers both transport (retried away) AND
// handler failure modes uniformly. Closes gh#192: pre-beta.93 the
// consumer-side method matrix exposed Request / RequestClassified /
// RequestWithRetry but had no retry+classify combination, forcing
// mutation-path callers to either skip the classified contract or
// wrap RequestWithRetry in a `json.Valid` pre-decode guard (the
// shape semteams shipped in cmd/semteams/tools/addsource/executor.go
// before this method existed).
//
// Use this for MUTATIONS where the responder is idempotent AND
// emits classified errors. The classified contract round-trips per
// the same rules as RequestClassified: transport failures arrive as
// the underlying error after the retry budget exhausts (classifies
// as ErrorTransient via pkg/errs.IsTransient); handler failures
// arrive as *errs.ClassifiedError reconstructed from the
// X-Error-Class header (or the legacy body prefix as fallback).
// Caller branches on errs.IsInvalid / IsTransient / IsFatal.
//
// For QUERIES use RequestClassified; retrying on a hung query
// masks responder problems as latency. See
// docs/operations/07-nats-request-retry.md for the full
// mutation-vs-query rule.
func (c *Client) RequestWithRetryClassified(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
	retry RetryConfig,
) ([]byte, error) {
	msg, err := c.requestMsgWithRetry(ctx, subject, data, timeout, retry)
	if err != nil {
		return nil, err
	}
	return ClassifyReply(msg)
}

// classForHeader returns the lowercase ErrorClass string that should
// be stamped in the X-Error-Class header for the given error.
func classForHeader(err error) string {
	switch errs.Classify(err) {
	case errs.ErrorInvalid:
		return ErrorClassInvalid
	case errs.ErrorFatal:
		return ErrorClassFatal
	case errs.ErrorTransient:
		return ErrorClassTransient
	default:
		return ErrorClassTransient
	}
}

// codeForHeader extracts the ADR-060 stable machine Code from err when
// it carries (or wraps) a *errs.ClassifiedError with a non-empty Code;
// "" otherwise. Returning "" means RespondError / ReplyError stamp no
// X-Error-Code header, so uncoded handler errors are unchanged on the wire.
func codeForHeader(err error) string {
	var ce *errs.ClassifiedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// classifiedFromHeader reconstructs a *errs.ClassifiedError from the
// X-Error-Class header value, the X-Error-Code header value (ADR-060),
// and the unwrapped body message. Uses errs.Classified / ClassifiedCode
// (the bare constructors) rather than the Wrap* family so the inner
// message survives verbatim — external surfaces (GraphQL responses, agent
// tool results) get the handler's clean text via err.Error() instead of a
// leaky framework-attribution prefix.
//
// When code is non-empty the reconstructed error carries it, so
// errors.Is(err, errs.ErrRevisionMismatch) and ce.Code discrimination
// work caller-side (the (*ClassifiedError).Is method matches by Code).
// When code is empty the result is exactly as before ADR-060 — uncoded.
//
// The resulting error round-trips through errs.IsInvalid / IsTransient /
// IsFatal correctly because the bare constructors preserve the class tag.
//
// Unknown class strings fall back to invalid (the conservative choice — a
// caller seeing IsInvalid won't retry on a class they don't recognize, vs
// IsTransient which would loop).
func classifiedFromHeader(class, code, message string) error {
	if message == "" {
		message = "handler error"
	}
	inner := errors.New(message)
	var ec errs.ErrorClass
	switch class {
	case ErrorClassFatal:
		ec = errs.ErrorFatal
	case ErrorClassTransient:
		ec = errs.ErrorTransient
	case ErrorClassInvalid:
		ec = errs.ErrorInvalid
	default:
		ec = errs.ErrorInvalid
	}
	if code != "" {
		return errs.ClassifiedCode(ec, code, inner)
	}
	return errs.Classified(ec, inner)
}

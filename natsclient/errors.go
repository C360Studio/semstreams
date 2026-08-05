// Package natsclient — the unified RPC error contract (ADR-060, gh#93).
//
// A request/reply is EITHER a success body (with no error header) OR a single
// typed error value. There is no in-band error channel. A handler error from a
// SubscribeForRequests handler — and from direct msg.Respond callers that opt in
// via RespondError — travels as wire headers plus a standard JSON error body:
//
//	X-Status:      error
//	X-Error-Class: transient | invalid | fatal
//	X-Error-Code:  entity_not_found | revision_mismatch | ...   (when coded)
//	body:          {"message": "<text>", "detail": {...}}
//
// ClassifyReply reconstructs a *errs.ClassifiedError from these: the class
// drives errs.IsInvalid / IsTransient / IsFatal, the code is reached via
// errors.As(err, &ce) → ce.Code, the one control-flow sentinel round-trips via
// errors.Is(err, errs.ErrRevisionMismatch), and the structured detail via
// ce.Detail. (The earlier additive window — a legacy "error: <msg>" body kept
// alongside the headers — was retired with the PR-D breaking change.)
//
// Caller pattern:
//
//	data, err := c.RequestClassified(ctx, subj, body, timeout)
//	if err != nil {
//	    // transport OR classified handler error, uniformly:
//	    if errors.Is(err, errs.ErrRevisionMismatch) { /* caller decides whether to re-read and retry */ }
//	    if errs.IsInvalid(err) { /* 4xx */ }
//	    if errs.IsTransient(err) { /* surface or apply the operation's explicit policy */ }
//	    var ce *errs.ClassifiedError
//	    if errors.As(err, &ce) { /* ce.Code, ce.Detail */ }
//	    return err
//	}
//	json.Unmarshal(data, &resp) // data is a success body; err already handled
//
// Handler pattern: return (nil, err) — SubscribeForRequests calls RespondError
// for you. HTTP semantics (404 vs 400) belong at the gateway, which reads
// ce.Code (e.g. entity_not_found → 404) rather than substring-sniffing.
//
// # Footgun — plain Request() / RequestWithHeaders() still don't classify
//
// The plain Request() / RequestWithHeaders() methods return the raw reply body
// and do NOT inspect the X-Status header. A handler error reply carries the
// {message, detail} body with err == nil, so
//
//	data, err := c.Request(...)
//	if err != nil { return err }
//	json.Unmarshal(data, &resp)   // mis-decodes an error body as success
//
// silently mis-decodes failures. New callers MUST use RequestClassified /
// RequestWithRetryClassified (or run ClassifyReply on the reply themselves when
// they need to send custom headers). See
// feedback_silent_handler_error_payload_audit.md.
//
// # Sentinel chains do not survive the wire boundary
//
// classifiedFromHeader reconstructs a fresh *errs.ClassifiedError from the
// headers + body. An arbitrary inner sentinel chain (jetstream.ErrKeyNotFound,
// etc.) is LOST in transit; what round-trips is the {class, code, message,
// detail} contract — including errs.ErrRevisionMismatch by code. Surface any
// other distinction a consumer must branch on as a code (ce.Code).
package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Header keys for the header-classified error convention. A failure reply
// carries X-Status: error plus the class/code headers; ClassifyReply
// reconstructs the typed error from them.
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

// errorBody is the ADR-060 standard error reply body. The class and code ride
// headers (X-Error-Class / X-Error-Code); this body carries the human-readable
// message and the structured Detail (entity id, revisions). It replaces the
// legacy "error: <msg>" text body the framework used through PR-C.
type errorBody struct {
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// marshalErrorBody renders the standard error body for err, best-effort: if the
// Detail map holds an unmarshalable value (handlers never construct one), it
// falls back to a message-only body rather than failing the reply.
func marshalErrorBody(err error) []byte {
	eb := errorBody{Message: err.Error()}
	var ce *errs.ClassifiedError
	if errors.As(err, &ce) {
		eb.Detail = ce.Detail
	}
	b, mErr := json.Marshal(eb)
	if mErr != nil {
		b, _ = json.Marshal(errorBody{Message: err.Error()})
	}
	return b
}

// parseErrorBody extracts the message + detail from a standard error body. A
// body that doesn't parse as the envelope is treated as a raw message
// (defensive — a handler that set X-Status but emitted a non-envelope body).
func parseErrorBody(data []byte) (string, map[string]any) {
	var eb errorBody
	if err := json.Unmarshal(data, &eb); err == nil && eb.Message != "" {
		return eb.Message, eb.Detail
	}
	if len(data) == 0 {
		return "handler error", nil
	}
	return string(data), nil
}

// errMissingReplySubject is returned by RespondError when the inbound
// message had no reply subject — there's no one to respond to.
var errMissingReplySubject = errors.New("natsclient: message has no reply subject")

// RespondError writes a header-classified error reply to msg: the X-Status /
// X-Error-Class / X-Error-Code headers carry the class + code, and the body is
// the {message, detail} envelope (ADR-060).
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
	reply.Data = marshalErrorBody(err)

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

	headers := map[string]string{
		HeaderStatus:     HeaderStatusError,
		HeaderErrorClass: classForHeader(err),
	}
	if code := codeForHeader(err); code != "" {
		headers[HeaderErrorCode] = code
	}
	return c.ReplyWithHeaders(ctx, replyTo, marshalErrorBody(err), headers)
}

// ClassifyReply inspects a reply message and returns either the
// success body (when no error signal is present) or a classified
// error suitable for branching with errs.IsInvalid / IsTransient /
// IsFatal.
//
// ADR-060: a failure is signalled ONLY by the X-Status: error header. The body
// is the standard {message, detail} envelope; the message + detail + the
// X-Error-Class / X-Error-Code headers reconstruct a *errs.ClassifiedError so
// errors.As(err, &ce) reaches ce.Code/ce.Detail and errors.Is(err,
// errs.ErrRevisionMismatch) round-trips. The legacy "error: " body fallback is
// gone (every producer header-stamps via RespondError/ReplyError).
func ClassifyReply(msg *nats.Msg) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	if msg.Header.Get(HeaderStatus) == HeaderStatusError {
		message, detail := parseErrorBody(msg.Data)
		return nil, classifiedFromHeader(
			msg.Header.Get(HeaderErrorClass),
			msg.Header.Get(HeaderErrorCode),
			message,
			detail,
		)
	}

	return msg.Data, nil
}

// RequestClassified is the recommended caller-side replacement for
// Request + body-prefix sniffing. It performs the request, then
// runs the reply through ClassifyReply so the returned error covers
// both transport and handler failure modes uniformly.
//
// Transport failures (no responders, timeout) are returned as the underlying
// error and classify as ErrorTransient via pkg/errs.IsTransient. Classification
// does not authorize a retry; the operation's contract owns that decision.
//
// Handler failures arrive as a *errs.ClassifiedError reconstructed
// from the X-Error-Class / X-Error-Code headers + {message, detail} body.
// Caller branches on errs.IsInvalid / IsTransient / IsFatal.
//
// Use this for steady-state queries and canonical graph mutations. Graph
// create, reconcile, append, and delete always make one transport attempt;
// ambiguity is returned to the component. Use RequestWithRetryClassified only
// for a non-graph operation whose contract explicitly authorizes redelivery.
// See docs/operations/07-nats-request-retry.md.
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
// Use this only where an operation-specific contract explicitly authorizes
// redelivery after transport ambiguity. The classified contract round-trips per
// the same rules as RequestClassified: transport failures arrive as
// the underlying error after the retry budget exhausts (classifies
// as ErrorTransient via pkg/errs.IsTransient); handler failures
// arrive as *errs.ClassifiedError reconstructed from the
// X-Error-Class / X-Error-Code headers + {message, detail} body.
// Caller branches on errs.IsInvalid / IsTransient / IsFatal.
//
// Canonical graph mutations and steady-state queries use RequestClassified.
// Retrying graph mutations can duplicate effects after a lost reply; retrying a
// hung query masks responder problems as latency. See
// docs/operations/07-nats-request-retry.md.
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

// RequestReadyClassified is the classified sibling of RequestReady (the
// readiness-gated read — the third bucket of the request doctrine). It runs the
// short-timeout, budget-bounded readiness loop, then runs the final reply
// through ClassifyReply so transport failures (no-responders / probe timeout,
// retried until the budget) and handler failures (X-Error-Class/Code headers)
// arrive uniformly.
//
// Use for the FIRST read on boot / initial reconcile / after a reconnect, where
// a not-yet-subscribed responder must not degrade to a full-query-timeout hang.
// For steady-state reads use RequestClassified (timeout = real signal). See
// docs/operations/07-nats-request-retry.md.
func (c *Client) RequestReadyClassified(
	ctx context.Context, subject string, data []byte, probeTimeout, budget time.Duration,
) ([]byte, error) {
	msg, err := c.requestMsgReady(ctx, subject, data, probeTimeout, budget)
	if err != nil {
		return nil, err
	}
	return ClassifyReply(msg)
}

// IsNoResponders reports whether err is (or wraps) the NATS "no responders"
// transport error — the responder is not subscribed. This is TRANSIENT at
// startup / after a reconnect (retry via a readiness-gated read), and distinct
// from a timeout of an EXISTING responder (which signals it is hung — surface,
// don't retry). Note (per request_integration_test.go): whether an absent
// responder surfaces as ErrNoResponders vs a plain timeout is server-config
// dependent, so a false here does NOT prove a responder exists — it only
// confirms the fast-fail no-responders signal when the server sends it.
func IsNoResponders(err error) bool {
	return errors.Is(err, nats.ErrNoResponders)
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
// X-Error-Class header value, the X-Error-Code header value, and the
// {message, detail} error body (ADR-060). Uses the bare constructors
// (Classified / ClassifiedCode / ClassifiedCodeDetail) rather than the Wrap*
// family so the inner message survives verbatim — external surfaces (GraphQL
// responses, agent tool results) get the handler's clean text via err.Error()
// instead of a leaky framework-attribution prefix.
//
// When code is non-empty the reconstructed error carries it, so
// errors.Is(err, errs.ErrRevisionMismatch) and ce.Code discrimination work
// caller-side; detail (when present) is reachable via errors.As → ce.Detail.
//
// Unknown class strings fall back to invalid (the conservative choice — a
// caller seeing IsInvalid won't retry on a class they don't recognize, vs
// IsTransient which would loop).
func classifiedFromHeader(class, code, message string, detail map[string]any) error {
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
	if code != "" || len(detail) > 0 {
		return errs.ClassifiedCodeDetail(ec, code, detail, inner)
	}
	return errs.Classified(ec, inner)
}

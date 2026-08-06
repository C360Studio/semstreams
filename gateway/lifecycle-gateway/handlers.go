// Package lifecyclegateway — HTTP + WebSocket handlers.
//
// Single catch-all `serveHTTP` dispatches by trimmed path + method.
// Path-tree shape (ADR-047 § Operator API):
//
//	{root}                                  GET   → list workflow types + counts
//	{root}/{type}                           GET   → list instances (query params for filters) — or WS upgrade when ?stream=true
//	{root}/{type}                           POST  → create instance (birth lane; create-or-fail, 409 on duplicate)
//	{root}/{type}/{id}                      GET   → instance state
//	{root}/{type}/{id}/history              GET   → phase-transition history
//	{root}/{type}/{id}/children             GET   → child instances
//	{root}/{type}/{id}/state                POST  → operator patch
//	{root}/{type}/{id}/transition           POST  → operator transition
//
// Error model: every non-2xx response is `{"error": "..."}` so
// operator dashboards parse one envelope. The mapping from
// pkg/lifecycle errors → HTTP status is centralized in `errorToStatus`,
// which is the single place to read it — this comment used to restate
// the table and went stale the first time the table grew. Lifecycle 4xx
// errors keep their underlying message; typed mutation errors use stable
// operator codes and safe messages. Internal details stay in the log.
//
// Adding a route to this surface owes a re-audit of the sentinels its new
// callee can raise: a sentinel unreachable before a route existed becomes
// reachable the moment one does. ErrAlreadyExists was unmapped until the
// create route made strict birth conflicts reachable.
package lifecyclegateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"
)

// writeBodyReadError renders a request-body read/decode failure. An oversize
// body surfaces as *http.MaxBytesError through the same generic error path as a
// syntax error, so without this every lane reported 400 while the published
// interface advertised 413 — on all three body-carrying lanes, since before the
// create lane existed.
func (c *Component) writeBodyReadError(w http.ResponseWriter, err error, context string) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		c.writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("request body exceeds the configured limit of %d bytes", maxBytes.Limit))
		c.recordRequest(false, "body too large")
		return
	}
	c.writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: %s", context, err.Error()))
	c.recordRequest(false, "invalid body")
}

// errorResponse is the uniform error envelope. Dashboards may continue to
// render Error directly. Code is additive and present when the framework has
// a stable operator-facing classification; callers must not infer retry safety
// from HTTP status alone.
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// serveHTTP is the catch-all entrypoint registered on the parent
// mux. Dispatches by trimmed-path + method.
func (c *Component) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Trim the configured root prefix. The mux already routed us
	// here because the prefix matched, so we just need the
	// remainder.
	root := strings.TrimSuffix(r.URL.Path, "/")
	rootPrefix := c.routePrefix()
	relative := strings.TrimPrefix(root, strings.TrimSuffix(rootPrefix, "/"))
	relative = strings.Trim(relative, "/")

	// Segment splitting. Empty `relative` means root path.
	var segments []string
	if relative != "" {
		segments = strings.Split(relative, "/")
	}

	switch len(segments) {
	case 0:
		c.handleListWorkflows(w, r)
	case 1:
		// {type} — create (POST), list instances (GET), OR WebSocket
		// upgrade when ?stream=true. The WS path stays under the same
		// URL for CORS and discovery parity (operator dashboards point
		// at the workflow type, add ?stream=true to subscribe).
		if r.Method == http.MethodPost {
			c.handleCreateInstance(w, r, segments[0])
			return
		}
		// The stream upgrade is a GET affordance. Checking it before the method
		// let POST ?stream=true fall into the upgrade, skipping create and
		// answering with the upgrader's plain-text body — which breaks the
		// uniform {"error": ...} envelope this package guarantees.
		if r.Method == http.MethodGet && r.URL.Query().Get("stream") == "true" {
			c.handleWebSocket(w, r, segments[0])
			return
		}
		c.handleListInstances(w, r, segments[0])
	case 2:
		// {type}/{id} → instance state
		c.handleGetInstance(w, r, segments[0], segments[1])
	case 3:
		c.handleSubresource(w, r, segments[0], segments[1], segments[2])
	default:
		c.writeError(w, http.StatusNotFound,
			fmt.Sprintf("no route for path %q (lifecycle gateway expects 0-3 segments under the root)", r.URL.Path))
		c.recordRequest(false, "no route")
	}
}

// routePrefix returns the mounted root for path stripping. The root
// is computed once in RegisterHTTPHandlers from the parent prefix +
// config.PathPrefix and stored on c.cachedRoot; this getter reads
// it under mu.RLock so a hot-reload of the component (re-running
// RegisterHTTPHandlers with a different prefix) doesn't race
// in-flight serveHTTP readers. The RLock is uncontended in steady
// state.
func (c *Component) routePrefix() string {
	c.mu.RLock()
	root := c.cachedRoot
	c.mu.RUnlock()
	if root == "" {
		// Pre-RegisterHTTPHandlers calls (shouldn't happen in
		// production but possible in test fixtures that wire raw):
		// strip nothing and route from the bare URL.
		return ""
	}
	return root
}

// --- Endpoint handlers ---

// handleListWorkflows returns the registered workflow types with
// per-phase instance counts. Phase counts are computed via a
// best-effort List(workflow, ListOptions{}) — at high cardinality
// this is O(N) per workflow type so dashboards should poll
// sparingly. The structured counts give operators an "at a glance"
// overview without hitting individual workflow endpoints.
//
// Per-workflow List errors surface as a per-entry `counts_error`
// field rather than blowing up the whole response — a dashboard
// rendering 12 workflow types shouldn't lose all of them because
// one bucket is briefly unreachable. The loud signal is BOTH the
// per-entry `counts_error` string in the response AND a Warn log
// per failed workflow so operators see the partial-degradation
// clearly (B2 reviewer fix — silent err=nil-fallthrough was
// rendering "0 instances in every phase" with full operator
// confidence, contradicting ADR-047's loud-fail discipline).
func (c *Component) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		c.writeError(w, http.StatusMethodNotAllowed, "GET only")
		c.recordRequest(false, "method not allowed")
		return
	}
	defs := c.manager.ListWorkflows()
	type entry struct {
		Workflow string `json:"workflow"`
		// KVBucket reflects the workflow's EntityIDPattern under
		// ADR-049 (no per-workflow bucket). Field name kept for
		// operator-API back-compat.
		KVBucket               string         `json:"kv_bucket"`
		Phases                 []string       `json:"phases"`
		TerminalPhases         []string       `json:"terminal_phases"`
		OperatorWritableFields []string       `json:"operator_writable_fields"`
		Counts                 map[string]int `json:"counts_by_phase"`
		CountsError            string         `json:"counts_error,omitempty"`
	}
	out := make([]entry, 0, len(defs))
	for _, def := range defs {
		e := entry{
			Workflow:               def.Workflow,
			KVBucket:               def.EntityIDPattern,
			Phases:                 def.Transitions.Phases(),
			TerminalPhases:         def.Transitions.TerminalPhases(),
			OperatorWritableFields: def.OperatorWritableFields,
			Counts:                 map[string]int{},
		}
		instances, err := c.manager.List(r.Context(), def.Workflow, lifecycle.ListOptions{})
		if err != nil {
			e.CountsError = err.Error()
			c.logger.Warn("lifecycle-gateway: List failed for workflow during counts aggregation",
				slog.String("workflow", def.Workflow),
				slog.String("error", err.Error()))
		} else {
			for _, inst := range instances {
				e.Counts[inst.Phase()]++
			}
		}
		out = append(out, e)
	}
	c.writeJSON(w, http.StatusOK, out)
	c.recordRequest(true, "")
}

// handleListInstances pages through Manager.List with query-param
// filters. Field-equality Match values are accepted as `match.<field>=<value>`
// query params. Values stay string-typed at the gateway boundary;
// reflection-based comparison inside pkg/lifecycle handles type
// coercion against the registered struct.
func (c *Component) handleListInstances(w http.ResponseWriter, r *http.Request, workflow string) {
	if r.Method != http.MethodGet {
		// POST is routed to create before reaching here, so anything
		// arriving is neither — advertise both real verbs.
		w.Header().Set("Allow", "GET, POST")
		c.writeError(w, http.StatusMethodNotAllowed, "GET (list) or POST (create) only")
		c.recordRequest(false, "method not allowed")
		return
	}
	q := r.URL.Query()
	opts := lifecycle.ListOptions{
		Phase:  q.Get("phase"),
		Active: q.Get("active") == "true",
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.writeError(w, http.StatusBadRequest, "invalid limit (must be a non-negative integer)")
			c.recordRequest(false, "invalid limit")
			return
		}
		opts.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.writeError(w, http.StatusBadRequest, "invalid offset (must be a non-negative integer)")
			c.recordRequest(false, "invalid offset")
			return
		}
		opts.Offset = n
	}
	// match.<field>=value query params populate ListOptions.Match.
	for k, vs := range q {
		if !strings.HasPrefix(k, "match.") || len(vs) == 0 {
			continue
		}
		if opts.Match == nil {
			opts.Match = make(map[string]any, 1)
		}
		opts.Match[strings.TrimPrefix(k, "match.")] = vs[0]
	}

	instances, err := c.manager.List(r.Context(), workflow, opts)
	if err != nil {
		c.writeErrorFromLifecycle(w, "list", err)
		return
	}
	if instances == nil {
		instances = []lifecycle.Participant{}
	}
	c.writeJSON(w, http.StatusOK, instances)
	c.recordRequest(true, "")
}

// handleGetInstance returns the full state of one Participant.
func (c *Component) handleGetInstance(w http.ResponseWriter, r *http.Request, workflow, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		c.writeError(w, http.StatusMethodNotAllowed, "GET only")
		c.recordRequest(false, "method not allowed")
		return
	}
	p, err := c.manager.Get(r.Context(), workflow, entityID)
	if err != nil {
		c.writeErrorFromLifecycle(w, "get", err)
		return
	}
	c.writeJSON(w, http.StatusOK, p)
	c.recordRequest(true, "")
}

// handleSubresource dispatches /{type}/{id}/{subresource} routes.
func (c *Component) handleSubresource(w http.ResponseWriter, r *http.Request, workflow, entityID, sub string) {
	switch sub {
	case "history":
		c.handleHistory(w, r, workflow, entityID)
	case "children":
		c.handleChildren(w, r, workflow, entityID)
	case "state":
		c.handleStatePatch(w, r, workflow, entityID)
	case "transition":
		c.handleOperatorTransition(w, r, workflow, entityID)
	default:
		c.writeError(w, http.StatusNotFound,
			fmt.Sprintf("unknown subresource %q (supported: history, children, state, transition)", sub))
		c.recordRequest(false, "unknown subresource")
	}
}

// handleHistory returns the participant's bounded recent transition records.
func (c *Component) handleHistory(w http.ResponseWriter, r *http.Request, workflow, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		c.writeError(w, http.StatusMethodNotAllowed, "GET only")
		c.recordRequest(false, "method not allowed")
		return
	}
	events, err := c.manager.History(r.Context(), workflow, entityID)
	if err != nil {
		c.writeErrorFromLifecycle(w, "history", err)
		return
	}
	if events == nil {
		events = []lifecycle.TransitionEvent{}
	}
	c.writeJSON(w, http.StatusOK, events)
	c.recordRequest(true, "")
}

// handleChildren returns Participants whose ParentEntityID matches
// the given entity, across all registered workflows.
func (c *Component) handleChildren(w http.ResponseWriter, r *http.Request, _ /* workflow */, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		c.writeError(w, http.StatusMethodNotAllowed, "GET only")
		c.recordRequest(false, "method not allowed")
		return
	}
	// ADR-049 Children: enumerates the parent's ChildSpec
	// LinkPredicate triples; each child is projected via the child
	// workflow's Schema. The route's {workflow} segment refers to
	// the parent's workflow type; the value isn't load-bearing for
	// Children itself.
	kids, err := c.manager.Children(r.Context(), entityID, lifecycle.ChildOptions{})
	if err != nil {
		c.writeErrorFromLifecycle(w, "children", err)
		return
	}
	if kids == nil {
		kids = []lifecycle.ChildResult{}
	}
	c.writeJSON(w, http.StatusOK, kids)
	c.recordRequest(true, "")
}

// handleStatePatch applies a JSON-body operator patch via
// Manager.UpdateFromOperator. Validation against
// lifecycle:"operator_writable" tags runs in pkg/lifecycle; the
// gateway just shuttles the body.
func (c *Component) handleStatePatch(w http.ResponseWriter, r *http.Request, workflow, entityID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		c.writeError(w, http.StatusMethodNotAllowed, "POST only")
		c.recordRequest(false, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, c.config.MaxBodyBytes)
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		c.writeBodyReadError(w, err, "invalid JSON body")
		return
	}
	if err := c.manager.UpdateFromOperator(r.Context(), workflow, entityID, patch); err != nil {
		c.writeErrorFromLifecycle(w, "state_patch", err)
		return
	}
	c.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	c.recordRequest(true, "")
}

// handleCreateInstance creates a workflow instance from an
// operator-supplied initial state (gh#814).
//
// This is the BIRTH lane, and the only one carrying a full initial-state
// envelope — the must-exist lanes (state patch, transition) stay
// envelope-free and still require an existing entity. Nothing auto-vivifies.
//
// Create-or-fail: a duplicate ID is 409, never an overwrite. There is no
// upsert lane on the operator surface, so a retried request is
// distinguishable from a fresh one.
//
// Returns 201 with the authoritative committed state projected from the causal
// mutation response, not the request body echoed — a caller renders what landed.
func (c *Component) handleCreateInstance(w http.ResponseWriter, r *http.Request, workflow string) {
	r.Body = http.MaxBytesReader(w, r.Body, c.config.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeBodyReadError(w, err, "could not read request body")
		return
	}

	result, err := c.manager.CreateFromOperator(r.Context(), workflow, body)
	if err != nil {
		c.writeErrorFromLifecycle(w, "create", err)
		return
	}
	c.writeJSON(w, http.StatusCreated, result.Instance)
	c.recordRequest(true, "")
}

// transitionRequest is the body shape for POST .../transition.
// Phase is required; Note is optional operator commentary that lands
// in the TransitionEvent.Note for audit.
type transitionRequest struct {
	Phase string `json:"phase"`
	Note  string `json:"note,omitempty"`
}

// handleOperatorTransition runs an explicit operator-initiated
// transition. Phase must be declared in the registered transitions
// table; current→target must be a declared edge.
func (c *Component) handleOperatorTransition(w http.ResponseWriter, r *http.Request, workflow, entityID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		c.writeError(w, http.StatusMethodNotAllowed, "POST only")
		c.recordRequest(false, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, c.config.MaxBodyBytes)
	var req transitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.writeBodyReadError(w, err, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Phase) == "" {
		c.writeError(w, http.StatusBadRequest, "phase is required")
		c.recordRequest(false, "missing phase")
		return
	}
	if err := c.manager.Transition(r.Context(), workflow, entityID, req.Phase,
		lifecycle.TransitionSourceOperator, req.Note); err != nil {
		c.writeErrorFromLifecycle(w, "transition", err)
		return
	}
	c.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "phase": req.Phase})
	c.recordRequest(true, "")
}

// handleWebSocket upgrades the connection and streams Participants
// from Manager.Watch to the client. The Watch contract delivers a
// bootstrap snapshot followed by live updates; this handler
// JSON-encodes each Participant per-message.
//
// The client controls the lifetime: closing the WS or canceling
// the request context cancels Watch's ctx, which closes Manager's
// watcher goroutine + the underlying NATS subscription. Slow
// consumers exceed gorilla/websocket's internal write buffer; when
// WriteJSON fails the gateway closes the connection rather than
// blocking the watcher (slow consumer → loud close per ADR-047's
// loud-fail discipline).
func (c *Component) handleWebSocket(w http.ResponseWriter, r *http.Request, workflow string) {
	if !c.config.wsEnabled() {
		c.writeError(w, http.StatusForbidden,
			"websocket streaming disabled (set enable_websocket=true in config)")
		c.recordRequest(false, "websocket disabled")
		return
	}
	conn, err := c.upgrade.Upgrade(w, r, nil)
	if err != nil {
		c.logger.Warn("websocket upgrade failed",
			slog.String("workflow", workflow),
			slog.String("error", err.Error()))
		c.recordRequest(false, "ws upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()

	c.wsConnections.Add(1)
	defer c.wsConnections.Add(-1)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	updates, err := c.manager.Watch(ctx, workflow)
	if err != nil {
		// Best-effort send the error frame before closing; if the
		// client already dropped, the write fails silently.
		_ = conn.WriteJSON(errorResponse{Error: err.Error()})
		c.recordRequest(false, "watch error")
		return
	}
	c.recordRequest(true, "")

	// Reader goroutine: detect client-side close + drain control
	// frames. We don't consume client messages but must keep the
	// reader alive so gorilla/websocket processes pings.
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-updates:
			if !ok {
				return
			}
			if err := conn.WriteJSON(p); err != nil {
				// Slow consumer or peer drop. Cancel + return.
				return
			}
		}
	}
}

// --- helpers ---

func (c *Component) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		c.logger.Warn("response write failed",
			slog.String("error", err.Error()))
	}
}

func (c *Component) writeError(w http.ResponseWriter, status int, msg string) {
	c.writeJSON(w, status, errorResponse{Error: msg})
}

func (c *Component) writeCodedError(w http.ResponseWriter, status int, code, msg string) {
	c.writeJSON(w, status, errorResponse{Error: msg, Code: code})
}

// writeErrorFromLifecycle maps a pkg/lifecycle error to the right
// HTTP status and writes the envelope. Records the failure for
// metrics.
//
// Typed projection failures use mutationOperatorError's stable code and safe
// message. For mapped lifecycle sentinels (status != 500), the underlying
// message ships through verbatim — operators NEED "field=X is not
// operator_writable" / "transition from terminal" detail to fix their request.
// For unmapped errors (status 500, infra-shaped), the response carries a canned
// "internal error" + label so the wire doesn't leak NATS subjects, KV keys, or
// trace IDs to potentially untrusted operators; the full error is logged at
// Error level for post-hoc inspection.
func (c *Component) writeErrorFromLifecycle(w http.ResponseWriter, label string, err error) {
	if mapped, ok := mutationOperatorError(err); ok {
		if mapped.status == http.StatusInternalServerError {
			c.logger.Error("lifecycle-gateway: internal mutation error",
				slog.String("op", label),
				slog.String("error", err.Error()))
		}
		c.writeCodedError(w, mapped.status, mapped.code, fmt.Sprintf("%s: %s", label, mapped.message))
		c.recordRequest(false, err.Error())
		return
	}
	status := errorToStatus(err)
	if status == http.StatusInternalServerError {
		c.logger.Error("lifecycle-gateway: internal error",
			slog.String("op", label),
			slog.String("error", err.Error()))
		c.writeError(w, status,
			fmt.Sprintf("%s: internal error (operator details in server log)", label))
		c.recordRequest(false, err.Error())
		return
	}
	c.writeError(w, status, fmt.Sprintf("%s: %s", label, err.Error()))
	c.recordRequest(false, err.Error())
}

type operatorMutationError struct {
	status  int
	code    string
	message string
}

// mutationOperatorError translates the canonical mutation client's closed
// failure vocabulary into the lifecycle operator contract. In particular,
// commit_unknown means delivery occurred without a valid authoritative reply:
// the gateway cannot claim failure or success, and must never suggest a blind
// retry. The caller must inspect current authority state before deciding what
// action is safe.
func mutationOperatorError(err error) (operatorMutationError, bool) {
	var mutationErr *projection.MutationError
	if !errors.As(err, &mutationErr) {
		return operatorMutationError{}, false
	}

	code := mutationErr.Code
	switch mutationErr.Kind {
	case projection.MutationInvalid:
		if code == "" {
			code = graph.ErrorCodeInvalidRequest
		}
		return operatorMutationError{http.StatusBadRequest, code, "invalid graph mutation"}, true
	case projection.MutationNotFound:
		if code == "" {
			code = graph.ErrorCodeEntityNotFound
		}
		return operatorMutationError{http.StatusNotFound, code, "entity not found"}, true
	case projection.MutationConflict:
		if code == "" {
			code = graph.ErrorCodeEntityExists
		}
		return operatorMutationError{http.StatusConflict, code, "entity already exists"}, true
	case projection.MutationRevisionConflict:
		if code == "" {
			code = graph.ErrorCodeRevisionMismatch
		}
		return operatorMutationError{http.StatusConflict, code, "authority revision mismatch"}, true
	case projection.MutationUnavailable:
		return operatorMutationError{http.StatusServiceUnavailable, "unavailable", "mutation service unavailable; mutation was not committed"}, true
	case projection.MutationCommitUnknown:
		return operatorMutationError{
			status: http.StatusServiceUnavailable,
			code:   "commit_unknown",
			message: "mutation commit outcome is unknown; inspect authoritative state " +
				"before deciding whether another mutation is safe",
		}, true
	case projection.MutationInternal:
		return operatorMutationError{http.StatusInternalServerError, "internal", "internal mutation error (operator details in server log)"}, true
	default:
		return operatorMutationError{http.StatusInternalServerError, "internal", "internal mutation error (operator details in server log)"}, true
	}
}

// errorToStatus maps canonical mutation errors and pkg/lifecycle sentinels to
// HTTP status codes.
// The mapping is the operator-facing contract: when a 404 vs 400 vs
// 409 distinction changes here, dashboards must follow. Anything
// unrecognised maps to 500 — the gateway doesn't gloss errors.
func errorToStatus(err error) int {
	if mapped, ok := mutationOperatorError(err); ok {
		return mapped.status
	}
	switch {
	case errors.Is(err, lifecycle.ErrWorkflowNotRegistered):
		return http.StatusNotFound
	case errors.Is(err, lifecycle.ErrEntityNotFound):
		return http.StatusNotFound
	case errors.Is(err, lifecycle.ErrFieldNotOperatorWritable):
		return http.StatusBadRequest
	case errors.Is(err, lifecycle.ErrInvalidTransition):
		return http.StatusBadRequest
	case errors.Is(err, lifecycle.ErrTerminalPhase):
		return http.StatusBadRequest
	case errors.Is(err, lifecycle.ErrUpdateRetriesExhausted):
		return http.StatusConflict
	case errors.Is(err, lifecycle.ErrAlreadyExists):
		// Create-or-fail: a duplicate ID is a conflict, never an
		// overwrite. Without this arm it fell through to 500 and read
		// as a server fault instead of "that instance already exists".
		return http.StatusConflict
	case errors.Is(err, lifecycle.ErrInvalidInitialState):
		return http.StatusBadRequest
	case errors.Is(err, lifecycle.ErrEntityIDPatternMismatch):
		return http.StatusBadRequest
	case errors.Is(err, lifecycle.ErrEntityNotLifecycleManaged):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// setCachedRoot records the fully-resolved mount root for serveHTTP
// to subtract when computing path segments. Called by
// RegisterHTTPHandlers; protected by mu so a hot-reload of the
// component can safely re-register without racing serveHTTP readers.
func (c *Component) setCachedRoot(root string) {
	c.mu.Lock()
	c.cachedRoot = root
	c.mu.Unlock()
}

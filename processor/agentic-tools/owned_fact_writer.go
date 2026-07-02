package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// OwnedFactWriter is the write surface for agent tools that OWN a MUTABLE
// PACKAGE of graph facts on an already-born entity and must REPLACE that
// package on re-emission — a retry / re-plan that can shrink or reshape the
// package — rather than append a stale second copy.
//
// It is deliberately SEPARATE from [TriplePublisher] (append / birth):
// replace-by-predicate + clear is a sharp tool, and append-only tools (decide,
// scratchpad, emit_diagnosis) must NOT reach it. Least privilege is enforced by
// interface, not by convention — a tool that owns a mutable package (semspec
// emit_change, semteams emit_dev_via_test_plan) takes an OwnedFactWriter; a
// tool that only appends evidence keeps TriplePublisher. The same concrete
// natsclient adapter can satisfy both; callers depend on the narrow surface
// they need.
//
// Both methods drive the framework's atomic mutable-fact lane
// (graph.mutation.entity.update_with_triples for writes,
// graph.ingest.query.entity for the read-back) — the SAME primitive the rule
// engine's replace_owned action (processor/rule/triple_mutator.go ReplaceOwned)
// and pkg/lifecycle use. Prefer this over a hand-rolled remove-then-add loop:
// that is non-atomic (a watcher can observe the predicate absent between the
// remove and the add) and costs one RPC per predicate. gh#425.
//
// Forward-compat: as ADR-056 owner-token and projection-contract enforcement
// harden for tool writers, this surface gains the lease/contract hook (the
// underlying UpdateEntityWithTriplesRequest already carries OwnerToken). New
// consumers are all in-repo, so threading it later is a single additive change.
type OwnedFactWriter interface {
	// ReplaceTriples atomically upserts add (REPLACE-by-predicate: every
	// predicate present in add has its prior values on the entity fully
	// replaced — upsert, NOT append) and clears every predicate named in
	// removePredicates, on the already-born entity entityID, via ONE
	// update_with_triples mutation. removePredicates runs BEFORE the add merge,
	// so a predicate present in both is cleared then re-added (net: the new
	// values).
	//
	// Use removePredicates to drop the STALE owned predicates a shrinking
	// package no longer emits — the add merge alone cannot know a predicate
	// vanished. Name ONLY predicates your tool owns: the entity is typically
	// SHARED (lifecycle phase, other tools' facts), and a predicate named here
	// is deleted regardless of who wrote it. Pair with ReadOwnedPredicates to
	// compute the owned set safely.
	//
	// This lane is UPDATE-only and PRESERVE-envelope: it never sets or changes
	// the entity's MessageType/Version/StorageRef envelope. That is intentional
	// — the entity's identity envelope is set once at BIRTH by its owner
	// (create_with_triples / graph-ingest), and a package-writing tool must not
	// overwrite a birth-owner's envelope (the ADR-055 provenance / ADR-054
	// indexing-profile key). MUST-EXIST: on a never-created entity the handler
	// returns entity_not_found (no auto-vivify) and this returns an error
	// carrying that classified Code — born the entity first.
	ReplaceTriples(ctx context.Context, entityID string, add []message.Triple, removePredicates []string) error

	// ReadOwnedPredicates returns the DISTINCT predicates currently present on
	// entityID whose name begins with ownedPrefix, sorted. It is the read half
	// of the shrinking-package pattern: read your owned predicates, then pass
	// them as removePredicates to ReplaceTriples to clear the whole prior
	// package before writing the revised one — "clear my prefix, then write the
	// current package."
	//
	// ownedPrefix MUST be non-empty and scope the read to predicates your tool
	// owns (e.g. "change.<slug>."). An empty prefix is REJECTED with an error:
	// on a shared entity it would return every owner's predicates, which — fed
	// to ReplaceTriples's removePredicates — would delete facts your tool does
	// not own. There is no unscoped-read escape hatch by design.
	//
	// The read and the subsequent ReplaceTriples are two separate RPCs with no
	// revision fence between them, so the "clear my prefix, then write" pattern
	// assumes a SINGLE concurrent writer of the owned package (the retry/re-plan
	// case it exists for — a loop does not race itself). A second concurrent
	// writer of the same prefix is not serialized here.
	//
	// A never-created entity surfaces as an error carrying the classified
	// entity_not_found Code — the package's owning entity is expected to exist
	// by replace time.
	ReadOwnedPredicates(ctx context.Context, entityID string, ownedPrefix string) ([]string, error)
}

const (
	// ownedFactUpdateSubject is the atomic entity+triples update lane
	// (must match graphingest.SubjectEntityUpdateWithTriples). Drives
	// replace-by-predicate + clear in a single mutation.
	ownedFactUpdateSubject = "graph.mutation.entity.update_with_triples"
	// ownedFactQuerySubject is the single-entity read-back lane used by
	// ReadOwnedPredicates (must match graphingest query.go).
	ownedFactQuerySubject = "graph.ingest.query.entity"
	// ownedFactTimeout bounds a single owned-fact round-trip. Matches the 5s
	// budget the sibling mutation surfaces (add / add_batch / replace_owned)
	// use — both are fast KV ops.
	ownedFactTimeout = 5 * time.Second
)

// entityQueryRequest is the wire shape graph.ingest.query.entity expects
// ({"id": "..."}). Kept local: graph exposes the handler, not a request type.
type entityQueryRequest struct {
	ID string `json:"id"`
}

// natsOwnedFactWriter adapts natsclient.Client to OwnedFactWriter, routing
// replace-by-predicate writes through graph.mutation.entity.update_with_triples
// and predicate read-back through graph.ingest.query.entity.
type natsOwnedFactWriter struct {
	client *natsclient.Client
}

// NewNATSOwnedFactWriter builds an OwnedFactWriter backed by the shared graph
// mutation/query NATS surfaces. Wire this into tools that own a mutable
// graph-backed fact package (gh#425).
func NewNATSOwnedFactWriter(client *natsclient.Client) OwnedFactWriter {
	return &natsOwnedFactWriter{client: client}
}

func (w *natsOwnedFactWriter) ReplaceTriples(ctx context.Context, entityID string, add []message.Triple, removePredicates []string) error {
	// A bare Entity{ID} delta leaves MessageType/Version/StorageRef at zero, so
	// graph-ingest's preserve-when-zero path keeps the birth-owner's envelope
	// intact (this lane never re-stamps identity).
	req := graph.UpdateEntityWithTriplesRequest{
		Entity:        &graph.EntityState{ID: entityID},
		AddTriples:    add,
		RemoveTriples: removePredicates,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal update_with_triples request: %w", err)
	}
	// RequestWithRetryClassified handles transient no-responders while
	// graph-ingest restarts or its subscription is propagating. The replace is
	// idempotent (replace-by-predicate converges to the same state on a
	// duplicate delivery), so retry is safe — this is a MUTATION, per the
	// natsclient mutation-vs-query rule.
	// gh#93 Phase 2 / ADR-060: handler failures (entity_not_found, internal)
	// arrive as the classified err below, not an in-body Success=false.
	respData, err := w.client.RequestWithRetryClassified(ctx, ownedFactUpdateSubject, reqData, ownedFactTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		// Surface the stable Code so callers/operators distinguish a must-exist
		// failure from transport without substring-matching the message.
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code != "" {
			return fmt.Errorf("replace_triples mutation failed [%s]: %w", ce.Code, err)
		}
		return fmt.Errorf("request %s: %w", ownedFactUpdateSubject, err)
	}
	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal update_with_triples response: %w", err)
	}
	return nil
}

func (w *natsOwnedFactWriter) ReadOwnedPredicates(ctx context.Context, entityID string, ownedPrefix string) ([]string, error) {
	// Reject the unscoped read: an empty prefix would return every owner's
	// predicates, and feeding those to ReplaceTriples would clear facts this
	// tool does not own. Force explicit ownership scoping.
	if ownedPrefix == "" {
		return nil, fmt.Errorf("read owned predicates: ownedPrefix must be non-empty (unscoped reads are rejected to prevent clearing predicates you do not own)")
	}
	reqData, err := json.Marshal(entityQueryRequest{ID: entityID})
	if err != nil {
		return nil, fmt.Errorf("marshal entity query: %w", err)
	}
	// RequestClassified (NOT the retry variant): this is a QUERY — retrying a
	// hung query masks a responder problem as latency (natsclient
	// mutation-vs-query rule). Matches the sibling query.entity reader in
	// agentic-loop/todos.go. Handler failures (entity_not_found) still arrive
	// classified.
	respData, err := w.client.RequestClassified(ctx, ownedFactQuerySubject, reqData, ownedFactTimeout)
	if err != nil {
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code != "" {
			return nil, fmt.Errorf("read owned predicates failed [%s]: %w", ce.Code, err)
		}
		return nil, fmt.Errorf("request %s: %w", ownedFactQuerySubject, err)
	}
	var entity graph.EntityState
	if err := json.Unmarshal(respData, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity query: %w", err)
	}
	return ownedPredicates(entity.Triples, ownedPrefix), nil
}

// ownedPredicates returns the distinct predicates in triples that begin with
// prefix, sorted for deterministic output. Empty predicates are skipped. Pure —
// unit-tested without NATS. (The public ReadOwnedPredicates rejects an empty
// prefix before reaching here; the empty-prefix-matches-all branch is the
// natural pure-filter semantics, not a supported call shape.)
func ownedPredicates(triples []message.Triple, prefix string) []string {
	seen := make(map[string]struct{})
	for _, tr := range triples {
		if tr.Predicate == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(tr.Predicate, prefix) {
			continue
		}
		seen[tr.Predicate] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

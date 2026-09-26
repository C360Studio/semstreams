package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/nats-io/nats.go/jetstream"
)

// The oracle contains semantic facts, not index keys or production projections.
// In particular, relationship identity is explicit in this input grammar.
const (
	modelNamePredicate = "core.identity.name"
	modelLiteralA      = "robotics.status.armed"
	modelLiteralB      = "robotics.status.disarmed"
	modelRelationA     = "robotics.assigned.mission"
	modelRelationB     = "robotics.related.peer"
)

var modelIDs = [...]string{
	"acme.ops.robotics.gcs.drone.001",
	"acme.ops.robotics.gcs.drone.002",
	"acme.ops.robotics.gcs.drone.003",
	"acme.ops.robotics.gcs.drone.004",
}

var modelNames = [...]string{"Alpha", "ALPHA", "Beta", "Gamma"}
var modelPredicates = [...]string{
	modelNamePredicate, modelLiteralA, modelLiteralB, modelRelationA, modelRelationB,
}

// This table states the finite input grammar's name equivalence independently
// of the implementation's normalizer and hashed storage representation.
var modelNameClass = [...]int{0, 0, 1, 2}

type modelEdge struct {
	predicate int // 0 or 1, selecting one of the two relationship predicates
	target    int // index into modelIDs; a target need not currently exist
}

type modelEntity struct {
	name     int // -1 for absent, otherwise index into modelNames
	literals [2]bool
	edges    []modelEdge
}

type modelFacts map[int]modelEntity

func cloneModelFacts(in modelFacts) modelFacts {
	out := make(modelFacts, len(in))
	for id, entity := range in {
		entity.edges = append([]modelEdge(nil), entity.edges...)
		out[id] = entity
	}
	return out
}

// encodeModelEntity is only the authority adapter. Expected answers below never
// use these triples, graph.EntityState, or any production index/key builder.
func encodeModelEntity(id int, entity modelEntity) ([]byte, error) {
	triples := make([]message.Triple, 0, 1+len(entity.literals)+len(entity.edges))
	if entity.name >= 0 {
		triples = append(triples, message.Triple{
			Subject: modelIDs[id], Predicate: modelNamePredicate, Object: modelNames[entity.name],
		})
	}
	for i, present := range entity.literals {
		if !present {
			continue
		}
		predicate := modelLiteralA
		if i == 1 {
			predicate = modelLiteralB
		}
		triples = append(triples, message.Triple{Subject: modelIDs[id], Predicate: predicate, Object: true})
	}
	for _, edge := range entity.edges {
		predicate := modelRelationA
		if edge.predicate == 1 {
			predicate = modelRelationB
		}
		triples = append(triples, message.Triple{
			Subject: modelIDs[id], Predicate: predicate, Object: modelIDs[edge.target],
		})
	}
	return json.Marshal(graph.EntityState{ID: modelIDs[id], Triples: triples})
}

type modelAnswers struct {
	predicates map[string][]string
	names      map[string][]graph.NameMatch
	incoming   map[string][]graph.IncomingEntry
	outgoing   map[string][]graph.OutgoingEntry
	present    map[string]bool
}

func expectedModelAnswers(facts modelFacts) modelAnswers {
	want := modelAnswers{
		predicates: make(map[string][]string, len(modelPredicates)),
		names:      make(map[string][]graph.NameMatch, len(modelNames)),
		incoming:   make(map[string][]graph.IncomingEntry, len(modelIDs)),
		outgoing:   make(map[string][]graph.OutgoingEntry, len(modelIDs)),
		present:    make(map[string]bool, len(modelIDs)),
	}
	for _, predicate := range modelPredicates {
		want.predicates[predicate] = []string{}
	}
	for _, name := range modelNames {
		want.names[name] = []graph.NameMatch{}
	}
	for _, id := range modelIDs {
		want.incoming[id] = []graph.IncomingEntry{}
		want.outgoing[id] = []graph.OutgoingEntry{}
	}
	for index, entity := range facts {
		id := modelIDs[index]
		want.present[id] = true
		if entity.name >= 0 {
			want.predicates[modelNamePredicate] = append(want.predicates[modelNamePredicate], id)
			for queryIndex, query := range modelNames {
				if modelNameClass[queryIndex] == modelNameClass[entity.name] {
					want.names[query] = append(want.names[query], graph.NameMatch{
						EntityID: id, MatchedName: modelNames[entity.name], Predicate: modelNamePredicate,
						ExactCase: query == modelNames[entity.name],
					})
				}
			}
		}
		for literal, present := range entity.literals {
			if present {
				want.predicates[modelPredicates[literal+1]] = append(want.predicates[modelPredicates[literal+1]], id)
			}
		}
		seenRelations := [2]bool{}
		for _, edge := range entity.edges {
			predicate := modelPredicates[edge.predicate+3]
			if !seenRelations[edge.predicate] {
				want.predicates[predicate] = append(want.predicates[predicate], id)
				seenRelations[edge.predicate] = true
			}
			want.incoming[modelIDs[edge.target]] = append(want.incoming[modelIDs[edge.target]], graph.IncomingEntry{
				FromEntityID: id, Predicate: predicate,
			})
			want.outgoing[id] = append(want.outgoing[id], graph.OutgoingEntry{
				ToEntityID: modelIDs[edge.target], Predicate: predicate,
			})
		}
	}
	for predicate := range want.predicates {
		sort.Strings(want.predicates[predicate])
	}
	for name := range want.names {
		sort.Slice(want.names[name], func(i, j int) bool {
			a, b := want.names[name][i], want.names[name][j]
			if a.ExactCase != b.ExactCase {
				return a.ExactCase
			}
			return a.EntityID < b.EntityID
		})
	}
	for id := range want.incoming {
		sort.Slice(want.incoming[id], func(i, j int) bool {
			a, b := want.incoming[id][i], want.incoming[id][j]
			if a.FromEntityID != b.FromEntityID {
				return a.FromEntityID < b.FromEntityID
			}
			return a.Predicate < b.Predicate
		})
	}
	for id := range want.outgoing {
		sort.Slice(want.outgoing[id], func(i, j int) bool {
			a, b := want.outgoing[id][i], want.outgoing[id][j]
			if a.ToEntityID != b.ToEntityID {
				return a.ToEntityID < b.ToEntityID
			}
			return a.Predicate < b.Predicate
		})
	}
	return want
}

func checkModelEntity(entity modelEntity) error {
	if entity.name < -1 || entity.name >= len(modelNames) {
		return fmt.Errorf("name index %d outside grammar", entity.name)
	}
	if len(entity.edges) > 2 {
		return fmt.Errorf("%d edges outside grammar", len(entity.edges))
	}
	seen := make(map[modelEdge]bool, 2)
	for _, edge := range entity.edges {
		if edge.predicate < 0 || edge.predicate > 1 || edge.target < 0 || edge.target >= len(modelIDs) || seen[edge] {
			return fmt.Errorf("edge %+v outside grammar or repeated", edge)
		}
		seen[edge] = true
	}
	return nil
}

type modelRecord struct {
	data     []byte
	revision uint64
}

type modelFixture struct {
	ctx       context.Context
	cancel    context.CancelFunc
	comp      *Component
	authority map[string]modelRecord
	facts     modelFacts
	head      uint64
	outgoing  *mockKVBucket
	incoming  *mockKVBucket
	predicate *mockKVBucket
	name      *mockKVBucket
	faultPut  bool
	faultHits int
}

func newModelFixture() (*modelFixture, error) {
	ctx, cancel := context.WithCancel(context.Background())
	nc, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		cancel()
		return nil, err
	}
	configBytes, err := json.Marshal(DefaultConfig())
	if err != nil {
		cancel()
		return nil, err
	}
	created, err := CreateGraphIndex(configBytes, component.Dependencies{NATSClient: nc})
	if err != nil {
		cancel()
		return nil, err
	}
	f := &modelFixture{
		ctx: ctx, cancel: cancel, comp: created.(*Component),
		authority: make(map[string]modelRecord), facts: make(modelFacts),
		outgoing: newMockKVBucket(), incoming: newMockKVBucket(),
		predicate: newMockKVBucket(), name: newMockKVBucket(),
	}
	f.comp.outgoingBucket = nc.NewKVStore(f.outgoing)
	f.comp.incomingBucket = nc.NewKVStore(f.incoming)
	f.comp.predicateBucket = nc.NewKVStore(f.predicate)
	f.comp.nameBucket = nc.NewKVStore(f.name)
	f.comp.aliasBucket = nc.NewKVStore(newMockKVBucket())
	f.comp.namePredicates = map[string]int{modelNamePredicate: 0}
	f.comp.watermark = revlag.New()
	authority := newMockKVBucket()
	authority.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		record, exists := f.authority[key]
		if !exists {
			return nil, jetstream.ErrKeyNotFound
		}
		return &orderedTestEntry{
			key: key, value: append([]byte(nil), record.data...), revision: record.revision,
			op: jetstream.KeyValuePut,
		}, nil
	}
	f.comp.entityStatesBucket = authority
	f.predicate.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if f.faultPut {
			f.faultHits++
			return 0, errors.New("injected persistent predicate Put failure")
		}
		f.predicate.mu.Lock()
		f.predicate.data[key] = append([]byte(nil), value...)
		f.predicate.mu.Unlock()
		return 1, nil
	}
	return f, nil
}

func (f *modelFixture) write(id int, entity modelEntity) (uint64, error) {
	if err := checkModelEntity(entity); err != nil {
		return 0, err
	}
	data, err := encodeModelEntity(id, entity)
	if err != nil {
		return 0, err
	}
	f.head++
	f.authority[modelIDs[id]] = modelRecord{data: data, revision: f.head}
	f.facts[id] = entity
	f.comp.watermark.Observe(f.head, modelIDs[id], time.Now())
	return f.head, nil
}

func (f *modelFixture) remove(id int) uint64 {
	f.head++
	delete(f.authority, modelIDs[id])
	delete(f.facts, id)
	f.comp.watermark.Observe(f.head, modelIDs[id], time.Now())
	return f.head
}

func (f *modelFixture) work(id int, revision uint64) {
	f.comp.processEntityWork(f.ctx, entityIndexWork{
		entityID: modelIDs[id], completionRevision: revision,
	})
}

// Production projection functions below are the subjects. The expected health
// verdicts in runModelHistory come from pending work and injected failure events.
func (f *modelFixture) status() (graph.IndexStatusResponse, bool) {
	indexed, at := f.comp.watermark.IndexedAt()
	status := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed: indexed, Target: f.head, IndexedAt: at,
	})
	status = applyKnownIncompleteOverrides(status, f.head,
		f.comp.initialEnumerationComplete.Load(), f.comp.failedCount.Load())
	status = f.comp.latchBootstrap(status)
	proceed, _ := graph.EvaluateReadinessGate(graph.StatusReading{Status: status, Fresh: true})
	return status, proceed
}

func (f *modelFixture) queryRefused() error {
	requests := []struct {
		name string
		call func(context.Context, []byte) ([]byte, error)
		body string
	}{
		{"outgoing", f.comp.handleQueryOutgoingNATS, fmt.Sprintf(`{"entity_id":%q}`, modelIDs[0])},
		{"incoming", f.comp.handleQueryIncomingNATS, fmt.Sprintf(`{"entity_id":%q}`, modelIDs[0])},
		{"predicate", f.comp.handleQueryPredicateNATS, fmt.Sprintf(`{"predicate":%q}`, modelLiteralA)},
		{"byName", f.comp.handleQueryByNameNATS, `{"name":"Alpha"}`},
	}
	for _, request := range requests {
		_, err := request.call(f.ctx, []byte(request.body))
		var classified *errs.ClassifiedError
		if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeIndexNotReady || !errs.IsTransient(err) {
			return fmt.Errorf("%s admitted unresolved failure or returned wrong error: %v", request.name, err)
		}
	}
	return nil
}

func modelQuery[T any](ctx context.Context, call func(context.Context, []byte) ([]byte, error), body string) (T, error) {
	var zero T
	data, err := call(ctx, []byte(body))
	if err != nil {
		return zero, err
	}
	var response graph.QueryResponse[T]
	if err := json.Unmarshal(data, &response); err != nil {
		return zero, err
	}
	return response.Data, nil
}

func equalUniqueSet[T comparable](got, want []T) error {
	seen := make(map[T]bool, len(got))
	for _, item := range got {
		if seen[item] {
			return fmt.Errorf("duplicate result %+v", item)
		}
		seen[item] = true
	}
	if len(got) != len(want) {
		return fmt.Errorf("got %+v, want %+v", got, want)
	}
	for _, item := range want {
		if !seen[item] {
			return fmt.Errorf("got %+v, want %+v", got, want)
		}
	}
	return nil
}

func (f *modelFixture) parity() error {
	status, proceed := f.status()
	if !proceed || !status.BootstrapComplete || f.comp.failedCount.Load() != 0 {
		return fmt.Errorf("caught-up boundary not healthy: status=%+v proceed=%t", status, proceed)
	}
	if !status.Ready || status.IndexedRevision != f.head || status.TargetRevision != f.head {
		return fmt.Errorf("completed work did not cover committed head %d: status=%+v", f.head, status)
	}
	return f.queryParity()
}

func (f *modelFixture) pendingParity(oldRevision, newestRevision uint64) error {
	status, proceed := f.status()
	if !proceed || !status.BootstrapComplete || status.Ready ||
		status.IndexedRevision != oldRevision || status.TargetRevision != newestRevision {
		return fmt.Errorf("older work completed newer pending revision: old=%d newest=%d status=%+v proceed=%t",
			oldRevision, newestRevision, status, proceed)
	}
	return f.queryParity()
}

func (f *modelFixture) queryParity() error {
	want := expectedModelAnswers(f.facts)
	for _, predicate := range modelPredicates {
		got, err := modelQuery[graph.PredicateData](f.ctx, f.comp.handleQueryPredicateNATS,
			fmt.Sprintf(`{"predicate":%q}`, predicate))
		if err != nil {
			return fmt.Errorf("predicate %s: %w", predicate, err)
		}
		if err := equalUniqueSet(got.Entities, want.predicates[predicate]); err != nil {
			return fmt.Errorf("predicate %s: %w", predicate, err)
		}
	}
	for _, name := range modelNames {
		got, err := modelQuery[graph.NameData](f.ctx, f.comp.handleQueryByNameNATS,
			fmt.Sprintf(`{"name":%q}`, name))
		if err != nil {
			return fmt.Errorf("name %s: %w", name, err)
		}
		if err := equalUniqueSet(got.Matches, want.names[name]); err != nil {
			return fmt.Errorf("name %s: %w", name, err)
		}
	}
	for _, id := range modelIDs {
		incoming, err := modelQuery[graph.IncomingRelationshipsData](f.ctx, f.comp.handleQueryIncomingNATS,
			fmt.Sprintf(`{"entity_id":%q}`, id))
		if err != nil {
			return fmt.Errorf("incoming %s: %w", id, err)
		}
		if err := equalUniqueSet(incoming.Relationships, want.incoming[id]); err != nil {
			return fmt.Errorf("incoming %s: %w", id, err)
		}
		outgoing, err := modelQuery[graph.OutgoingRelationshipsData](f.ctx, f.comp.handleQueryOutgoingNATS,
			fmt.Sprintf(`{"entity_id":%q}`, id))
		if err != nil {
			return fmt.Errorf("outgoing %s: %w", id, err)
		}
		if err := equalUniqueSet(outgoing.Relationships, want.outgoing[id]); err != nil {
			return fmt.Errorf("outgoing %s: %w", id, err)
		}
		f.outgoing.mu.Lock()
		value, present := f.outgoing.data[id]
		f.outgoing.mu.Unlock()
		if present != want.present[id] {
			return fmt.Errorf("outgoing owner %s present=%t want=%t", id, present, want.present[id])
		}
		if present && len(want.outgoing[id]) == 0 && !reflect.DeepEqual(value, []byte("[]")) {
			return fmt.Errorf("outgoing owner %s empty encoding=%q, want []", id, value)
		}
	}
	return nil
}

type modelActivation struct {
	cold, replacement, ownership, stale, duplicate int
	failure, repair, hydration                     int
}

func (a *modelActivation) add(other modelActivation) {
	a.cold += other.cold
	a.replacement += other.replacement
	a.ownership += other.ownership
	a.stale += other.stale
	a.duplicate += other.duplicate
	a.failure += other.failure
	a.repair += other.repair
	a.hydration += other.hydration
}

func (a modelActivation) complete() error {
	if a.cold == 0 || a.replacement == 0 || a.ownership == 0 || a.stale == 0 ||
		a.duplicate == 0 || a.failure == 0 || a.repair == 0 || a.hydration == 0 {
		return fmt.Errorf("mandatory observation not activated: %+v", a)
	}
	return nil
}

type modelRunResult struct {
	activation modelActivation
	err        error
}

func (f *modelFixture) writeWork(id int, entity modelEntity) error {
	revision, err := f.write(id, entity)
	if err != nil {
		return err
	}
	f.work(id, revision)
	return f.parity()
}

func (f *modelFixture) deleteWork(id int) error {
	revision := f.remove(id)
	f.work(id, revision)
	return f.parity()
}

func (f *modelFixture) expectCold() error {
	status, proceed := f.status()
	if proceed || status.BootstrapComplete || status.Ready || status.State != graph.IndexStateBuilding {
		return fmt.Errorf("pending initial build admitted: status=%+v proceed=%t", status, proceed)
	}
	return nil
}

func (f *modelFixture) failAndRepair(id int, entity modelEntity) error {
	// Clear first so a missing PREDICATE membership must be Put; an existing
	// identical row could otherwise make the injected backend fault unobservable.
	if err := f.writeWork(id, modelEntity{name: -1}); err != nil {
		return fmt.Errorf("prepare required Put: %w", err)
	}
	entity.literals[0] = true
	f.faultPut = true
	hitsBefore := f.faultHits
	revision, err := f.write(id, entity)
	if err != nil {
		return err
	}
	f.work(id, revision)
	if f.faultHits-hitsBefore < indexWriteMaxAttempts {
		return fmt.Errorf("persistent fault hit %d attempts, want at least %d",
			f.faultHits-hitsBefore, indexWriteMaxAttempts)
	}
	status, proceed := f.status()
	if proceed || status.State != graph.IndexStateDegraded || status.Ready ||
		f.comp.failedCount.Load() != 1 || f.comp.watermark.Indexed() != revision {
		return fmt.Errorf("failed required Put did not degrade after completion: status=%+v proceed=%t failed=%d indexed=%d revision=%d",
			status, proceed, f.comp.failedCount.Load(), f.comp.watermark.Indexed(), revision)
	}
	if err := f.queryRefused(); err != nil {
		return err
	}
	f.faultPut = false
	f.comp.repairFailedEntities(f.ctx)
	if f.comp.failedCount.Load() != 0 {
		return fmt.Errorf("repair left %d failed entities", f.comp.failedCount.Load())
	}
	return f.parity()
}

func (f *modelFixture) hydrate() (*modelFixture, error) {
	// A history-1 replay can only assert a head floor if a surviving record
	// carries the latest head. Bump one live owner before taking this snapshot.
	if len(f.facts) == 0 {
		return nil, fmt.Errorf("hydration requires nonempty current authority")
	}
	newest := -1
	for id := range modelIDs {
		if _, exists := f.facts[id]; exists {
			newest = id
			break
		}
	}
	if newest < 0 {
		return nil, fmt.Errorf("hydration found no bounded owner")
	}
	if err := f.writeWork(newest, f.facts[newest]); err != nil {
		return nil, fmt.Errorf("head-bearing replay record: %w", err)
	}
	other, err := newModelFixture()
	if err != nil {
		return nil, err
	}
	other.head = f.head
	other.facts = cloneModelFacts(f.facts)
	type retained struct {
		id       int
		revision uint64
	}
	entries := make([]retained, 0, len(f.authority))
	for id, record := range f.authority {
		other.authority[id] = modelRecord{data: append([]byte(nil), record.data...), revision: record.revision}
		for index, knownID := range modelIDs {
			if id == knownID {
				entries = append(entries, retained{id: index, revision: record.revision})
				break
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].revision < entries[j].revision })
	for _, entry := range entries {
		other.comp.watermark.Observe(entry.revision, modelIDs[entry.id], time.Now())
	}
	other.comp.bootstrapTarget.Store(other.head)
	other.comp.initialEnumerationComplete.Store(true)
	if err := other.expectCold(); err != nil {
		other.cancel()
		return nil, fmt.Errorf("fresh-owner incomplete hydration: %w", err)
	}
	for index, entry := range entries {
		other.work(entry.id, entry.revision)
		if index < len(entries)-1 {
			if err := other.expectCold(); err != nil {
				other.cancel()
				return nil, fmt.Errorf("fresh-owner partial hydration: %w", err)
			}
		}
	}
	if err := other.parity(); err != nil {
		other.cancel()
		return nil, fmt.Errorf("fresh-owner final parity: %w", err)
	}
	return other, nil
}

func runModelHistory(actions []modelAction) modelRunResult {
	f, err := newModelFixture()
	if err != nil {
		return modelRunResult{err: err}
	}
	defer func() { f.cancel() }()
	result := modelRunResult{}
	fail := func(stage string, err error) modelRunResult {
		return modelRunResult{activation: result.activation, err: fmt.Errorf("%s: %w", stage, err)}
	}
	// Two distinct owners are delivered before enumeration completes. Neither
	// an empty bucket nor one completed owner can license a partial answer.
	first := modelEntity{name: 0, literals: [2]bool{true, false}, edges: []modelEdge{{predicate: 0, target: 2}}}
	second := modelEntity{name: 2, edges: []modelEdge{{predicate: 1, target: 0}}}
	rev0, err := f.write(0, first)
	if err != nil {
		return fail("initial source", err)
	}
	rev1, err := f.write(1, second)
	if err != nil {
		return fail("initial second source", err)
	}
	f.comp.bootstrapTarget.Store(f.head)
	f.comp.initialEnumerationComplete.Store(true)
	if err := f.expectCold(); err != nil {
		return fail("cold before work", err)
	}
	f.work(0, rev0)
	if err := f.expectCold(); err != nil {
		return fail("cold after one owner", err)
	}
	f.work(1, rev1)
	if err := f.parity(); err != nil {
		return fail("initial convergence", err)
	}
	result.activation.cold++

	if err := f.writeWork(0, modelEntity{name: 2, literals: [2]bool{false, true},
		edges: []modelEdge{{predicate: 1, target: 3}}}); err != nil {
		return fail("A-to-B replacement", err)
	}
	if err := f.writeWork(0, modelEntity{name: -1}); err != nil {
		return fail("B-to-empty replacement", err)
	}
	result.activation.replacement++
	if err := f.deleteWork(0); err != nil {
		return fail("target deletion preserving source assertion", err)
	}
	if err := f.deleteWork(1); err != nil {
		return fail("source deletion retracting owned assertion", err)
	}
	result.activation.ownership++
	if err := f.writeWork(0, modelEntity{name: -1}); err != nil {
		return fail("restore first source", err)
	}
	if err := f.writeWork(1, modelEntity{name: -1}); err != nil {
		return fail("restore second source", err)
	}

	old, err := f.write(0, modelEntity{name: 0, literals: [2]bool{false, true}})
	if err != nil {
		return fail("stale observed work", err)
	}
	latest, err := f.write(0, modelEntity{name: 2, literals: [2]bool{false, true},
		edges: []modelEdge{{predicate: 0, target: 3}}})
	if err != nil {
		return fail("new authority before stale execution", err)
	}
	f.work(0, old)
	if err := f.pendingParity(old, latest); err != nil {
		return fail("stale work refetch", err)
	}
	f.work(0, latest)
	if err := f.parity(); err != nil {
		return fail("new work completion", err)
	}
	result.activation.stale++
	f.work(0, latest)
	if err := f.parity(); err != nil {
		return fail("duplicate work", err)
	}
	result.activation.duplicate++
	if err := f.failAndRepair(0, modelEntity{name: 0}); err != nil {
		return fail("persistent Put failure and repair", err)
	}
	result.activation.failure++
	result.activation.repair++
	other, err := f.hydrate()
	if err != nil {
		return fail("fresh owner hydration", err)
	}
	f.cancel()
	f = other
	result.activation.hydration++

	for step, action := range actions {
		stage := fmt.Sprintf("suffix %d %s", step, action)
		switch action.kind {
		case modelUpsert:
			err = f.writeWork(action.id, action.entity)
		case modelClear:
			err = f.writeWork(action.id, modelEntity{name: -1})
		case modelDelete:
			err = f.deleteWork(action.id)
		case modelDuplicate:
			f.work(action.id, 0)
			err = f.parity()
			if err == nil {
				result.activation.duplicate++
			}
		case modelStale:
			var oldRevision, latestRevision uint64
			oldEntity, exists := f.facts[action.id]
			if !exists {
				oldEntity = modelEntity{name: -1}
			}
			oldRevision, err = f.write(action.id, oldEntity)
			if err == nil {
				latestRevision, err = f.write(action.id, action.entity)
			}
			if err == nil {
				f.work(action.id, oldRevision)
				err = f.pendingParity(oldRevision, latestRevision)
			}
			if err == nil {
				f.work(action.id, latestRevision)
				err = f.parity()
			}
			if err == nil {
				result.activation.stale++
			}
		case modelFailureRepair:
			err = f.failAndRepair(action.id, action.entity)
			if err == nil {
				result.activation.failure++
				result.activation.repair++
			}
		case modelHydrate:
			var hydrated *modelFixture
			hydrated, err = f.hydrate()
			if err == nil {
				f.cancel()
				f = hydrated
				result.activation.hydration++
			}
		default:
			err = fmt.Errorf("unknown action kind %d", action.kind)
		}
		if err != nil {
			return fail(stage, err)
		}
	}
	if err := result.activation.complete(); err != nil {
		return fail("activation", err)
	}
	return result
}

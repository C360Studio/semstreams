package graphindex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// spec: graph-index / Every surviving derived index declares storage responsibility and reconciliation capability
// spec: graph-index / INCOMING rows are retracted by their source owner
// spec: graph-index / The index buckets rebuild from entity state on boot after the format cutover
func TestGraphIndexModelRequiredHistory(t *testing.T) {
	history := []modelAction{
		{kind: modelUpsert, id: 2, entity: modelEntity{name: 3, literals: [2]bool{true, true},
			edges: []modelEdge{{predicate: 0, target: 0}, {predicate: 1, target: 3}}}},
		{kind: modelClear, id: 2},
		{kind: modelDelete, id: 2},
		{kind: modelDuplicate, id: 2},
		{kind: modelHydrate},
	}
	var result modelRunResult
	synctest.Test(t, func(_ *testing.T) { result = runModelHistory(history) })
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := result.activation.complete(); err != nil {
		t.Fatal(err)
	}
	t.Logf("mandatory model observations: %+v", result.activation)
}

// spec: graph-index / Readiness is authoritative and consumers fail closed on incomplete indexes
// spec: graph-index-readiness / The envelope reports bootstrap completion
// spec: graph-index-readiness / Read consumers retry the readiness transient
func TestGraphIndexModelEmptyAndHealthyLag(t *testing.T) {
	var runErr error
	synctest.Test(t, func(_ *testing.T) {
		f, err := newModelFixture()
		if err != nil {
			runErr = err
			return
		}
		defer f.cancel()
		f.comp.bootstrapTarget.Store(0)
		f.comp.initialEnumerationComplete.Store(true)
		status, proceed := f.status()
		if !proceed || !status.Ready || !status.BootstrapComplete {
			runErr = fmt.Errorf("authoritatively empty graph did not bootstrap: %+v proceed=%t", status, proceed)
			return
		}
		if err := f.parity(); err != nil {
			runErr = fmt.Errorf("empty graph parity: %w", err)
			return
		}
		_, err = f.write(0, modelEntity{name: 0, literals: [2]bool{true, false}})
		if err != nil {
			runErr = err
			return
		}
		status, proceed = f.status()
		if !proceed || status.Ready || !status.BootstrapComplete || status.State != graph.IndexStateBuilding {
			runErr = fmt.Errorf("healthy post-bootstrap lag wrongly gated: %+v proceed=%t", status, proceed)
			return
		}
		_, err = modelQuery[graph.PredicateData](f.ctx, f.comp.handleQueryPredicateNATS,
			fmt.Sprintf(`{"predicate":%q}`, modelLiteralA))
		if err != nil {
			runErr = fmt.Errorf("healthy lag read refused: %w", err)
		}
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
}

// spec: graph-index / Every surviving derived index declares storage responsibility and reconciliation capability
func TestGraphIndexModelStableNameKeyCaseReplacement(t *testing.T) {
	var runErr error
	synctest.Test(t, func(_ *testing.T) {
		f, err := newModelFixture()
		if err != nil {
			runErr = err
			return
		}
		defer f.cancel()
		rev, err := f.write(0, modelEntity{name: 0})
		if err != nil {
			runErr = err
			return
		}
		f.comp.bootstrapTarget.Store(f.head)
		f.comp.initialEnumerationComplete.Store(true)
		f.work(0, rev)
		if err := f.parity(); err != nil {
			runErr = fmt.Errorf("initial Alpha: %w", err)
			return
		}
		if err := f.writeWork(0, modelEntity{name: 1}); err != nil {
			runErr = fmt.Errorf("Alpha to ALPHA stable key: %w", err)
		}
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
}

// spec: graph-index / Readiness is authoritative and consumers fail closed on incomplete indexes
// spec: graph-index / INCOMING rows are retracted by their source owner
func TestGraphIndexModelRequiredDeleteFailureAndRepair(t *testing.T) {
	var runErr error
	synctest.Test(t, func(_ *testing.T) {
		f, err := newModelFixture()
		if err != nil {
			runErr = err
			return
		}
		defer f.cancel()
		rev, err := f.write(0, modelEntity{name: -1, edges: []modelEdge{{predicate: 0, target: 2}}})
		if err != nil {
			runErr = err
			return
		}
		f.comp.bootstrapTarget.Store(f.head)
		f.comp.initialEnumerationComplete.Store(true)
		f.work(0, rev)
		if err := f.parity(); err != nil {
			runErr = err
			return
		}
		hits := 0
		f.incoming.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
			hits++
			return errors.New("injected persistent incoming Delete failure")
		}
		rev = f.remove(0)
		f.work(0, rev)
		status, proceed := f.status()
		if hits == 0 || proceed || status.State != graph.IndexStateDegraded ||
			f.comp.failedCount.Load() != 1 || f.comp.watermark.Indexed() != rev {
			runErr = fmt.Errorf("required Delete failure not retained: hits=%d status=%+v proceed=%t failed=%d indexed=%d",
				hits, status, proceed, f.comp.failedCount.Load(), f.comp.watermark.Indexed())
			return
		}
		if err := f.queryRefused(); err != nil {
			runErr = err
			return
		}
		f.incoming.deleteFunc = nil
		f.comp.repairFailedEntities(f.ctx)
		if err := f.parity(); err != nil {
			runErr = fmt.Errorf("required Delete repair: %w", err)
		}
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
}

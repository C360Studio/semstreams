package graphindex

import (
	"flag"
	"fmt"
	"testing"
	"testing/synctest"

	"pgregory.net/rapid"
)

type modelActionKind uint8

const (
	modelUpsert modelActionKind = iota
	modelClear
	modelDelete
	modelDuplicate
	modelStale
	modelFailureRepair
	modelHydrate
)

type modelAction struct {
	kind   modelActionKind
	id     int
	entity modelEntity
}

func (a modelAction) String() string {
	return fmt.Sprintf("kind=%d id=%d entity=%+v", a.kind, a.id, a.entity)
}

func drawModelEntity(t *rapid.T) modelEntity {
	entity := modelEntity{name: rapid.IntRange(-1, len(modelNames)-1).Draw(t, "name")}
	entity.literals[0] = rapid.Bool().Draw(t, "literalA")
	entity.literals[1] = rapid.Bool().Draw(t, "literalB")
	count := rapid.IntRange(0, 2).Draw(t, "edgeCount")
	for len(entity.edges) < count {
		edge := modelEdge{
			predicate: rapid.IntRange(0, 1).Draw(t, "edgePredicate"),
			target:    rapid.IntRange(0, len(modelIDs)-1).Draw(t, "edgeTarget"),
		}
		if len(entity.edges) > 0 && edge == entity.edges[0] {
			edge.target = (edge.target + 1) % len(modelIDs)
		}
		entity.edges = append(entity.edges, edge)
	}
	return entity
}

func drawModelHistory(t *rapid.T) []modelAction {
	length := rapid.IntRange(0, 12).Draw(t, "suffixLength")
	actions := make([]modelAction, 0, length)
	known := modelFacts{
		0: {name: -1},
		1: {name: -1},
	}
	failureUsed, hydrationUsed := false, false
	for i := 0; i < length; i++ {
		choices := []modelActionKind{modelUpsert, modelClear, modelDelete, modelDuplicate, modelStale}
		if !failureUsed {
			choices = append(choices, modelFailureRepair)
		}
		if !hydrationUsed && len(known) > 0 {
			choices = append(choices, modelHydrate)
		}
		kind := rapid.SampledFrom(choices).Draw(t, fmt.Sprintf("kind_%02d", i))
		id := rapid.IntRange(0, len(modelIDs)-1).Draw(t, fmt.Sprintf("id_%02d", i))
		action := modelAction{kind: kind, id: id}
		switch kind {
		case modelUpsert:
			action.entity = drawModelEntity(t)
			known[id] = action.entity
		case modelClear:
			if _, present := known[id]; !present {
				kind = modelUpsert
				action.kind = kind
			}
			action.entity = modelEntity{name: -1}
			known[id] = action.entity
		case modelDelete:
			if _, present := known[id]; !present {
				// Deleting an absent source would not exercise owned-row retraction.
				action.kind = modelDuplicate
				break
			}
			delete(known, id)
		case modelDuplicate:
			// Any delivered entity can be reconciled again from current authority.
		case modelStale:
			action.entity = drawModelEntity(t)
			known[id] = action.entity
		case modelFailureRepair:
			failureUsed = true
			action.entity = drawModelEntity(t)
			action.entity.literals[0] = true // a required PREDICATE Put must run
			known[id] = action.entity
		case modelHydrate:
			hydrationUsed = true
		}
		actions = append(actions, action)
	}
	return actions
}

// spec: graph-index / Every surviving derived index declares storage responsibility and reconciliation capability
// spec: graph-index / INCOMING rows are retracted by their source owner
// spec: graph-index / Readiness is authoritative and consumers fail closed on incomplete indexes
// spec: graph-index / The index buckets rebuild from entity state on boot after the format cutover
// spec: graph-index-readiness / The envelope reports bootstrap completion
// spec: graph-index-readiness / Read consumers retry the readiness transient
func TestPropGraphIndexReconciliation(t *testing.T) {
	t.Logf("rapid seed=%s", flag.Lookup("rapid.seed").Value.String())
	checks := 0
	totals := modelActivation{}
	rapid.Check(t, func(rt *rapid.T) {
		history := drawModelHistory(rt)
		var result modelRunResult
		synctest.Test(t, func(_ *testing.T) {
			result = runModelHistory(history)
		})
		if result.err != nil {
			rt.Fatalf("history=%v: %v", history, result.err)
		}
		checks++
		totals.add(result.activation)
	})
	t.Logf("valid checks=%d activation=%+v", checks, totals)
}

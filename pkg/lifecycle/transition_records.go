package lifecycle

import (
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/google/uuid"
)

// transitionHistoryLimit is the fixed operator-history window retained in a
// participant's current entity value. It is deliberately not configurable:
// History is an operational window, not an unbounded audit log. Sixty-four
// records covers long-running workflows while bounding each record to five
// small triples on the already-hot authoritative entity.
const transitionHistoryLimit = 64

const (
	transitionPredicateFrom   = vocabulary.LifecycleTransitionFrom
	transitionPredicateTo     = vocabulary.LifecycleTransitionTo
	transitionPredicateAt     = vocabulary.LifecycleTransitionAt
	transitionPredicateSource = vocabulary.LifecycleTransitionSource
	transitionPredicateNote   = vocabulary.LifecycleTransitionNote
)

var transitionRecordPredicates = []string{
	transitionPredicateFrom,
	transitionPredicateTo,
	transitionPredicateAt,
	transitionPredicateSource,
	transitionPredicateNote,
}

type transitionRecord struct {
	id    string
	event TransitionEvent
}

func newTransitionRecord(from, to string, at time.Time, source TransitionSource, note string) transitionRecord {
	return transitionRecord{
		id: uuid.NewString(),
		event: TransitionEvent{
			From: from, To: to, At: at, Triggered: source, Note: note,
		},
	}
}

func isTransitionRecordPredicate(predicate string) bool {
	switch predicate {
	case transitionPredicateFrom, transitionPredicateTo, transitionPredicateAt,
		transitionPredicateSource, transitionPredicateNote:
		return true
	default:
		return false
	}
}

func appendTransitionRecord(records []transitionRecord, record transitionRecord) []transitionRecord {
	records = append(records, record)
	sortTransitionRecords(records)
	if len(records) > transitionHistoryLimit {
		records = records[len(records)-transitionHistoryLimit:]
	}
	return records
}

// nextTransitionTimestamp preserves the causal order recorded in the entity
// when the wall clock repeats or moves backwards. The timestamp remains a
// wall-clock value, but never sorts a new transition before the state it
// observed and advanced.
func nextTransitionTimestamp(records []transitionRecord, observed time.Time) time.Time {
	if len(records) == 0 {
		return observed
	}
	latest := records[len(records)-1].event.At
	if observed.After(latest) {
		return observed
	}
	return latest.Add(time.Nanosecond)
}

func sortTransitionRecords(records []transitionRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].event.At.Equal(records[j].event.At) {
			return records[i].id < records[j].id
		}
		return records[i].event.At.Before(records[j].event.At)
	})
}

func transitionRecordsToTriples(entityID string, records []transitionRecord) []message.Triple {
	triples := make([]message.Triple, 0, len(records)*len(transitionRecordPredicates))
	for _, record := range records {
		appendField := func(predicate string, object any) {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: predicate, Object: object,
				Timestamp: record.event.At, Confidence: 1, Context: record.id,
			})
		}
		appendField(transitionPredicateFrom, record.event.From)
		appendField(transitionPredicateTo, record.event.To)
		appendField(transitionPredicateAt, record.event.At.Format(time.RFC3339Nano))
		appendField(transitionPredicateSource, string(record.event.Triggered))
		if record.event.Note != "" {
			appendField(transitionPredicateNote, record.event.Note)
		}
	}
	return triples
}

func decodeTransitionRecords(entityID string, triples []message.Triple) ([]transitionRecord, error) {
	type fields struct {
		values map[string]string
	}
	groups := make(map[string]*fields)
	for _, item := range triples {
		if !isTransitionRecordPredicate(item.Predicate) {
			continue
		}
		if item.Subject != entityID {
			return nil, fmt.Errorf("%w: predicate %q has subject %q, want %q",
				ErrInvalidTransitionRecord, item.Predicate, item.Subject, entityID)
		}
		if item.Context == "" {
			return nil, fmt.Errorf("%w: predicate %q has no occurrence context",
				ErrInvalidTransitionRecord, item.Predicate)
		}
		value, ok := item.Object.(string)
		if !ok {
			return nil, fmt.Errorf("%w: occurrence %q predicate %q has non-string value %T",
				ErrInvalidTransitionRecord, item.Context, item.Predicate, item.Object)
		}
		group := groups[item.Context]
		if group == nil {
			group = &fields{values: make(map[string]string)}
			groups[item.Context] = group
		}
		if _, duplicate := group.values[item.Predicate]; duplicate {
			return nil, fmt.Errorf("%w: occurrence %q repeats predicate %q",
				ErrInvalidTransitionRecord, item.Context, item.Predicate)
		}
		group.values[item.Predicate] = value
	}
	if len(groups) > transitionHistoryLimit {
		return nil, fmt.Errorf("%w: entity carries %d occurrences, fixed limit is %d",
			ErrInvalidTransitionRecord, len(groups), transitionHistoryLimit)
	}

	records := make([]transitionRecord, 0, len(groups))
	for id, group := range groups {
		to, ok := group.values[transitionPredicateTo]
		if !ok || to == "" {
			return nil, fmt.Errorf("%w: occurrence %q has no target phase", ErrInvalidTransitionRecord, id)
		}
		from, ok := group.values[transitionPredicateFrom]
		if !ok {
			return nil, fmt.Errorf("%w: occurrence %q has no source phase field", ErrInvalidTransitionRecord, id)
		}
		atText, ok := group.values[transitionPredicateAt]
		if !ok {
			return nil, fmt.Errorf("%w: occurrence %q has no timestamp", ErrInvalidTransitionRecord, id)
		}
		at, err := time.Parse(time.RFC3339Nano, atText)
		if err != nil {
			return nil, fmt.Errorf("%w: occurrence %q timestamp %q: %v",
				ErrInvalidTransitionRecord, id, atText, err)
		}
		sourceText, ok := group.values[transitionPredicateSource]
		if !ok || !isTransitionSource(TransitionSource(sourceText)) {
			return nil, fmt.Errorf("%w: occurrence %q has invalid source %q",
				ErrInvalidTransitionRecord, id, sourceText)
		}
		records = append(records, transitionRecord{
			id: id,
			event: TransitionEvent{
				From: from, To: to, At: at, Triggered: TransitionSource(sourceText),
				Note: group.values[transitionPredicateNote],
			},
		})
	}
	sortTransitionRecords(records)
	return records, nil
}

func validateTransitionRecordChain(records []transitionRecord, currentPhase string) error {
	if len(records) == 0 {
		return fmt.Errorf("%w: lifecycle-managed entity has no birth record", ErrInvalidTransitionRecord)
	}
	for i := 1; i < len(records); i++ {
		if records[i].event.From != records[i-1].event.To {
			return fmt.Errorf("%w: occurrence %q starts at %q after %q ended at %q",
				ErrInvalidTransitionRecord, records[i].id, records[i].event.From,
				records[i-1].id, records[i-1].event.To)
		}
	}
	if records[len(records)-1].event.To != currentPhase {
		return fmt.Errorf("%w: latest occurrence ends at %q while current phase is %q",
			ErrInvalidTransitionRecord, records[len(records)-1].event.To, currentPhase)
	}
	return nil
}

func isTransitionSource(source TransitionSource) bool {
	switch source {
	case TransitionSourceRule, TransitionSourceOperator, TransitionSourceComponent, TransitionSourceFramework:
		return true
	default:
		return false
	}
}

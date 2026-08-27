package graph

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
)

func TestEventValidateContract(t *testing.T) {
	primary := eventTestID(t, "primary")
	target := eventTestID(t, "target")
	metadata := eventTestMetadata()
	valid := Event{
		Type:       EventRelationshipCreate,
		EntityID:   primary,
		TargetID:   target,
		Properties: map[string]any{"edge_type": "owns"},
		Metadata:   metadata,
		Confidence: 1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"unknown type", func(event *Event) { event.Type = EventType("future") }},
		{"malformed primary", func(event *Event) { event.EntityID = "three.part.id" }},
		{"missing relationship target", func(event *Event) { event.TargetID = "" }},
		{"malformed relationship target", func(event *Event) { event.TargetID = "three.part.id" }},
		{"entity event target", func(event *Event) { event.Type = EventEntityUpdate }},
		{"negative confidence", func(event *Event) { event.Confidence = -0.01 }},
		{"high confidence", func(event *Event) { event.Confidence = 1.01 }},
		{"nan confidence", func(event *Event) { event.Confidence = math.NaN() }},
		{"positive infinity", func(event *Event) { event.Confidence = math.Inf(1) }},
		{"negative infinity", func(event *Event) { event.Confidence = math.Inf(-1) }},
		{"missing rule", func(event *Event) { event.Metadata.RuleName = "" }},
		{"missing timestamp", func(event *Event) { event.Metadata.Timestamp = time.Time{} }},
		{"missing source", func(event *Event) { event.Metadata.Source = "" }},
		{"missing reason", func(event *Event) { event.Metadata.Reason = "" }},
		{"missing version", func(event *Event) { event.Metadata.Version = "" }},
		{"unknown version", func(event *Event) { event.Metadata.Version = "2.0.0" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneEvent(valid)
			test.mutate(&candidate)
			before := cloneEvent(candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !eventContractEqual(candidate, before) {
				t.Fatalf("Validate mutated event\nbefore: %#v\nafter:  %#v", before, candidate)
			}
		})
	}

	for _, key := range []string{"entity_id", "target_id", "confidence", "metadata"} {
		t.Run("reserved "+key, func(t *testing.T) {
			candidate := cloneEvent(valid)
			candidate.Properties[key] = "shadow"
			before := cloneEvent(candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("Validate mutated properties")
			}
		})
	}

	var nilEvent *Event
	if err := nilEvent.Validate(); err == nil {
		t.Fatal("nil Event.Validate() = nil, want error")
	}
}

func TestGraphEventConstructorsFailClosed(t *testing.T) {
	validID := eventTestID(t, "source")
	targetID := eventTestID(t, "target")
	metadata := eventTestMetadata()
	badID := "three.part.id"

	tests := []struct {
		name      string
		construct func(string) (*Event, error)
	}{
		{"entity update", func(id string) (*Event, error) {
			return NewEntityUpdateEvent(id, map[string]any{"state": "ready"}, metadata)
		}},
		{"entity create", func(id string) (*Event, error) {
			return NewEntityCreateEvent(id, "worker", map[string]any{"state": "ready"}, metadata)
		}},
		{"entity delete", func(id string) (*Event, error) {
			return NewEntityDeleteEvent(id, "retired", metadata)
		}},
		{"relationship create source", func(id string) (*Event, error) {
			return NewRelationshipCreateEvent(id, targetID, "owns", metadata)
		}},
		{"relationship create target", func(id string) (*Event, error) {
			return NewRelationshipCreateEvent(validID, id, "owns", metadata)
		}},
		{"relationship delete source", func(id string) (*Event, error) {
			return NewRelationshipDeleteEvent(id, targetID, "owns", metadata)
		}},
		{"relationship delete target", func(id string) (*Event, error) {
			return NewRelationshipDeleteEvent(validID, id, "owns", metadata)
		}},
		{"alert source", func(id string) (*Event, error) {
			return NewAlertEvent("acme", "dep1", "threshold", id, map[string]any{"observed": 42}, metadata)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := test.construct(badID)
			if err == nil || event != nil {
				t.Fatalf("construct() = (%#v, %v), want (nil, error)", event, err)
			}
		})
	}
}

func TestConstructorsOwnTopLevelPropertiesOnly(t *testing.T) {
	metadata := eventTestMetadata()
	nestedMap := map[string]any{"value": 1}
	nestedSlice := []string{"a", "b"}
	caller := map[string]any{"nested_map": nestedMap, "nested_slice": nestedSlice, "status_text": "new"}
	before := cloneProperties(caller)

	event, err := NewAlertEvent("acme", "dep1", "threshold", eventTestID(t, "source"), caller, metadata)
	if err != nil {
		t.Fatalf("NewAlertEvent: %v", err)
	}
	if !reflect.DeepEqual(caller, before) {
		t.Fatalf("constructor mutated caller properties\nbefore: %#v\nafter:  %#v", before, caller)
	}
	if reflect.ValueOf(caller).Pointer() == reflect.ValueOf(event.Properties).Pointer() {
		t.Fatal("constructor retained caller top-level map")
	}
	if reflect.ValueOf(caller["nested_map"]).Pointer() != reflect.ValueOf(event.Properties["nested_map"]).Pointer() {
		t.Fatal("constructor deep-copied nested map; nested values must remain caller-owned")
	}
	if reflect.ValueOf(caller["nested_slice"]).Pointer() != reflect.ValueOf(event.Properties["nested_slice"]).Pointer() {
		t.Fatal("constructor deep-copied nested slice; nested values must remain caller-owned")
	}

	caller["later"] = true
	if _, exists := event.Properties["later"]; exists {
		t.Fatal("later caller top-level mutation changed event")
	}
	event.Properties["event_only"] = true
	if _, exists := caller["event_only"]; exists {
		t.Fatal("later event top-level mutation changed caller")
	}

	for _, key := range []string{"alert_type", "source_entity", "status", "entity_id", "target_id", "confidence", "metadata"} {
		t.Run(key, func(t *testing.T) {
			input := map[string]any{key: "collision", "nested": nestedMap}
			inputBefore := cloneProperties(input)
			got, constructErr := NewAlertEvent("acme", "dep1", "threshold", eventTestID(t, "source"), input, metadata)
			if constructErr == nil || got != nil {
				t.Fatalf("collision construct = (%#v, %v), want (nil, error)", got, constructErr)
			}
			if !reflect.DeepEqual(input, inputBefore) {
				t.Fatal("failed construction mutated caller properties")
			}
		})
	}
}

func TestConstructorOwnedPropertyCollisions(t *testing.T) {
	metadata := eventTestMetadata()
	id := eventTestID(t, "source")
	if event, err := NewEntityCreateEvent(id, "worker", map[string]any{"type": "collision"}, metadata); err == nil || event != nil {
		t.Fatalf("entity type collision = (%#v, %v), want (nil, error)", event, err)
	}
	if event, err := NewRelationshipCreateEvent(id, eventTestID(t, "target"), "", metadata); err == nil || event != nil {
		t.Fatalf("empty relationship type = (%#v, %v), want (nil, error)", event, err)
	}
	if event, err := NewAlertEvent("acme", "dep1", "", id, nil, metadata); err == nil || event != nil {
		t.Fatalf("empty alert type = (%#v, %v), want (nil, error)", event, err)
	}
}

func TestConstructorMetadataVersionIsLocalAndClosed(t *testing.T) {
	metadata := eventTestMetadata()
	metadata.Version = ""
	event, err := NewEntityUpdateEvent(eventTestID(t, "version-default"), nil, metadata)
	if err != nil {
		t.Fatalf("NewEntityUpdateEvent: %v", err)
	}
	if event.Metadata.Version != eventMetadataVersion {
		t.Fatalf("event metadata version = %q, want %q", event.Metadata.Version, eventMetadataVersion)
	}
	if metadata.Version != "" {
		t.Fatalf("constructor mutated caller metadata version to %q", metadata.Version)
	}

	metadata.Version = "2.0.0"
	if event, err = NewEntityUpdateEvent(eventTestID(t, "version-rejected"), nil, metadata); err == nil || event != nil {
		t.Fatalf("unknown metadata version = (%#v, %v), want (nil, error)", event, err)
	}
}

func TestNewAlertEventDigestContract(t *testing.T) {
	metadata := EventMetadata{
		RuleName:  "battery-rule",
		Timestamp: time.Date(2026, time.July, 16, 12, 34, 56, 789123456, time.UTC),
		Source:    "rule-processor",
		Reason:    "battery below threshold",
	}
	sourceID := eventTestID(t, "drone-001")
	event, err := NewAlertEvent("acme", "dep1", "battery_low", sourceID, map[string]any{"value": 10}, metadata)
	if err != nil {
		t.Fatalf("NewAlertEvent: %v", err)
	}
	const golden = "acme.dep1.rules.graph.alert.3c18b02fde7a5bdd8e7ab45cd2309936067799121833df9a2b579a4bd8080ce4"
	if event.EntityID != golden {
		t.Fatalf("alert ID = %q, want %q", event.EntityID, golden)
	}
	if len(event.EntityID) != 92 {
		t.Fatalf("alert ID length = %d, want 92", len(event.EntityID))
	}
	if event.Metadata.Version != eventMetadataVersion || metadata.Version != "" {
		t.Fatalf("local version default failed: event=%q caller=%q", event.Metadata.Version, metadata.Version)
	}

	repeated, err := NewAlertEvent("acme", "dep1", "battery_low", sourceID, map[string]any{"different": true}, EventMetadata{
		RuleName:  metadata.RuleName,
		Timestamp: metadata.Timestamp.In(time.FixedZone("other", -7*60*60)),
		Source:    metadata.Source,
		Reason:    "different mutable reason",
		Version:   eventMetadataVersion,
	})
	if err != nil {
		t.Fatalf("repeat NewAlertEvent: %v", err)
	}
	if repeated.EntityID != event.EntityID {
		t.Fatalf("same instant/default-equivalent metadata changed identity: %q != %q", repeated.EntityID, event.EntityID)
	}

	changes := []struct {
		name      string
		sourceID  string
		alertType string
		metadata  EventMetadata
	}{
		{"source", eventTestID(t, "drone-002"), "battery_low", metadata},
		{"alert type", sourceID, "temperature_high", metadata},
		{"rule name", sourceID, "battery_low", withRuleName(metadata, "other-rule")},
		{"source component", sourceID, "battery_low", withSource(metadata, "other-processor")},
		{"timestamp", sourceID, "battery_low", withTimestamp(metadata, metadata.Timestamp.Add(time.Nanosecond))},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			changed, changedErr := NewAlertEvent("acme", "dep1", change.alertType, change.sourceID, nil, change.metadata)
			if changedErr != nil {
				t.Fatalf("NewAlertEvent: %v", changedErr)
			}
			if changed.EntityID == event.EntityID {
				t.Fatal("identity-bearing input change did not change digest")
			}
		})
	}
}

func TestNewAlertEventMaximumSource(t *testing.T) {
	metadata := eventTestMetadata()
	maximum := "a.b.c.d.e." + strings.Repeat("x", 246)
	if len(maximum) != 256 {
		t.Fatalf("test source length = %d, want 256", len(maximum))
	}
	event, err := NewAlertEvent("acme", "dep1", "maximum", maximum, nil, metadata)
	if err != nil {
		t.Fatalf("NewAlertEvent maximum source: %v", err)
	}
	if len(event.EntityID) != 92 || !strings.HasPrefix(event.EntityID, "acme.dep1.rules.graph.alert.") {
		t.Fatalf("alert ID = %q (len %d), want fixed canonical form", event.EntityID, len(event.EntityID))
	}
	repeated, err := NewAlertEvent("acme", "dep1", "maximum", maximum, nil, metadata)
	if err != nil {
		t.Fatalf("repeat NewAlertEvent maximum source: %v", err)
	}
	if repeated.EntityID != event.EntityID {
		t.Fatalf("maximum-source replay ID = %q, want %q", repeated.EntityID, event.EntityID)
	}

	tooLong := "a.b.c.d.e." + strings.Repeat("x", 247)
	if event, err = NewAlertEvent("acme", "dep1", "maximum", tooLong, nil, metadata); err == nil || event != nil {
		t.Fatalf("257-byte source = (%#v, %v), want (nil, error)", event, err)
	}
}

func TestEventSubjectAndPayload(t *testing.T) {
	event, err := NewRelationshipCreateEvent(
		eventTestID(t, "source"),
		eventTestID(t, "target"),
		"owns",
		eventTestMetadata(),
	)
	if err != nil {
		t.Fatalf("NewRelationshipCreateEvent: %v", err)
	}
	if got, want := event.Subject(), "graph.events.relationship.create"; got != want {
		t.Fatalf("Subject() = %q, want %q", got, want)
	}
	payload := event.Payload()
	if payload["entity_id"] != event.EntityID || payload["target_id"] != event.TargetID || payload["edge_type"] != "owns" {
		t.Fatalf("Payload() = %#v", payload)
	}
}

func eventTestID(t testing.TB, instance string) string {
	t.Helper()
	return semantictest.EntityID(t, "test", "graph", "events", "rules", "entity", instance)
}

func eventTestMetadata() EventMetadata {
	return EventMetadata{
		RuleName:  "contract-rule",
		Timestamp: time.Date(2026, time.July, 16, 12, 0, 0, 123, time.UTC),
		Source:    "rule-processor",
		Reason:    "contract test",
		Version:   eventMetadataVersion,
	}
}

func cloneEvent(event Event) Event {
	event.Properties = cloneProperties(event.Properties)
	return event
}

func eventContractEqual(left, right Event) bool {
	confidenceEqual := left.Confidence == right.Confidence ||
		math.IsNaN(left.Confidence) && math.IsNaN(right.Confidence)
	return left.Type == right.Type &&
		left.EntityID == right.EntityID &&
		left.TargetID == right.TargetID &&
		reflect.DeepEqual(left.Properties, right.Properties) &&
		reflect.DeepEqual(left.Metadata, right.Metadata) &&
		confidenceEqual
}

func cloneProperties(properties map[string]any) map[string]any {
	result := make(map[string]any, len(properties))
	for key, value := range properties {
		result[key] = value
	}
	return result
}

func withRuleName(metadata EventMetadata, value string) EventMetadata {
	metadata.RuleName = value
	return metadata
}

func withSource(metadata EventMetadata, value string) EventMetadata {
	metadata.Source = value
	return metadata
}

func withTimestamp(metadata EventMetadata, value time.Time) EventMetadata {
	metadata.Timestamp = value
	return metadata
}

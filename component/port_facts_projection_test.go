package component

import "testing"

func TestPortFactsProjectionResolvesEveryCanonicalFactAndReturnsCopies(t *testing.T) {
	contract := &InterfaceContract{Type: "example.events", Version: "v1", Compatible: []string{"v0"}}
	definition := PortDefinition{
		Name: "events",
		Config: JetStreamPort{
			StreamName:        "EVENTS",
			Subjects:          []string{"events.>", "audit.events.>"},
			Storage:           "file",
			RetentionPolicy:   "limits",
			RetentionDays:     7,
			MaxSizeGB:         3,
			Replicas:          2,
			ConsumerName:      "worker",
			DeliverPolicy:     "all",
			AckPolicy:         "explicit",
			MaxDeliver:        5,
			AckWait:           "2s",
			HeartbeatInterval: "1s",
			MaxAckPending:     23,
			Interface:         contract,
		},
	}

	port, err := definition.Resolve(DirectionOutput)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := port.Facts()
	if err != nil {
		t.Fatal(err)
	}
	if facts.Kind() != PortKindJetStream || facts.ResourceID() != "jetstream:EVENTS" || facts.IsExclusive() {
		t.Fatalf("base facts = kind:%q resource:%q exclusive:%v", facts.Kind(), facts.ResourceID(), facts.IsExclusive())
	}
	if facts.InteractionPattern() != PatternStream {
		t.Fatalf("interaction = %q, want %q", facts.InteractionPattern(), PatternStream)
	}
	assertStrings(t, facts.ConnectionIDs(), []string{"EVENTS", "events.>", "audit.events.>"})
	assertStrings(t, facts.NATSSubjects(), []string{"events.>", "audit.events.>"})
	gotContract, ok := facts.Interface()
	if !ok || gotContract.Type != "example.events" || gotContract.Version != "v1" {
		t.Fatalf("interface = %+v, %v", gotContract, ok)
	}
	stream, ok := facts.Stream()
	if !ok {
		t.Fatal("stream facts absent")
	}
	if stream.Name() != "EVENTS" || stream.Storage() != "file" || stream.RetentionPolicy() != "limits" ||
		stream.RetentionDays() != 7 || stream.MaxSizeGB() != 3 || stream.Replicas() != 2 ||
		stream.ConsumerName() != "worker" || stream.DeliverPolicy() != "all" || stream.AckPolicy() != "explicit" ||
		stream.MaxDeliver() != 5 || stream.AckWait() != "2s" || stream.HeartbeatInterval() != "1s" ||
		stream.MaxAckPending() != 23 {
		t.Fatalf("stream facts did not preserve the canonical JetStream declaration")
	}
	assertStrings(t, stream.Subjects(), []string{"events.>", "audit.events.>"})

	connections := facts.ConnectionIDs()
	connections[0] = "corrupt"
	subjects := facts.NATSSubjects()
	subjects[0] = "corrupt"
	streamSubjects := stream.Subjects()
	streamSubjects[0] = "corrupt"
	gotContract.Compatible[0] = "corrupt"

	again, err := port.Facts()
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, again.ConnectionIDs(), []string{"EVENTS", "events.>", "audit.events.>"})
	assertStrings(t, again.NATSSubjects(), []string{"events.>", "audit.events.>"})
	againStream, _ := again.Stream()
	assertStrings(t, againStream.Subjects(), []string{"events.>", "audit.events.>"})
	againContract, _ := again.Interface()
	assertStrings(t, againContract.Compatible, []string{"v0"})
}

func TestPortFactsProjectionUsesCanonicalInteractionForExactKVRead(t *testing.T) {
	port, err := (PortDefinition{Name: "entities", Config: KVReadPort{Bucket: "ENTITY_STATES"}}).Resolve(DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := port.Facts()
	if err != nil {
		t.Fatal(err)
	}
	if facts.InteractionPattern() != PatternWatch {
		t.Fatalf("interaction = %q, want %q", facts.InteractionPattern(), PatternWatch)
	}
	assertStrings(t, facts.ConnectionIDs(), []string{"kv:ENTITY_STATES"})
}

func TestPortFactsProjectionRevalidatesMutableRuntimePort(t *testing.T) {
	_, err := (Port{Name: "broken", Direction: DirectionInput, Config: NATSPort{}}).Facts()
	if err == nil {
		t.Fatal("Facts accepted a runtime port whose mutable config no longer resolves")
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("strings[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

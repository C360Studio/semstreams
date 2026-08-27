package payloadregistry

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/projection/contract"
	"github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// attrPayload is a schema-bearing stub so validateSchemaConsistency runs on it.
type attrPayload struct{ key types.Type }

func (p *attrPayload) Schema() types.Type { return p.key }

func attrRegistration(domain, category, version string) *Registration {
	key := types.Type{Domain: domain, Category: category, Version: version}
	return &Registration{
		Domain: domain, Category: category, Version: version,
		Description: "attribute test type",
		Factory:     func() any { return &attrPayload{key: key} },
	}
}

func lessonLikeContract(name string, messageType types.Type) contract.Contract {
	return contract.Contract{
		Name: name, MessageType: messageType, EntityPattern: "*.*.agent.lesson.record.*",
		BirthPredicates: []string{"agent.lesson.category"},
	}
}

func TestRegisterRejectsInvalidIndexingProfile(t *testing.T) {
	reg := New()
	r := attrRegistration("agentic", "agent_lesson", "v1")
	r.IndexingProfile = "prose"
	err := reg.Register(r)
	if err == nil {
		t.Fatal("Register accepted indexing profile \"prose\"")
	}
	if !strings.Contains(err.Error(), "prose") {
		t.Fatalf("error does not name the rejected value: %v", err)
	}
	if _, ok := reg.GetRegistration("agentic.agent_lesson.v1"); ok {
		t.Fatal("a rejected registration was stored")
	}

	t.Run("contract profile must agree with the type floor (O-13)", func(t *testing.T) {
		reg := New()
		r := attrRegistration("agentic", "agent_lesson", "v1")
		r.IndexingProfile = vocabulary.IndexingProfileContent
		c := lessonLikeContract("agentic.lesson-record", types.Type{})
		c.IndexingProfile = vocabulary.IndexingProfileControl
		r.Contracts = []contract.Contract{c}
		err := reg.Register(r)
		if err == nil {
			t.Fatal("Register accepted a contract profile that disagrees with the type's floor")
		}
		if !strings.Contains(err.Error(), vocabulary.IndexingProfileContent) ||
			!strings.Contains(err.Error(), vocabulary.IndexingProfileControl) {
			t.Fatalf("error does not name both profiles: %v", err)
		}
	})
}

func TestRegisterFillsAndChecksContractMessageType(t *testing.T) {
	reg := New()
	r := attrRegistration("agentic", "agent_lesson", "v1")
	r.Contracts = []contract.Contract{lessonLikeContract("agentic.lesson-record", types.Type{})}
	if err := reg.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := reg.GetRegistration("agentic.agent_lesson.v1")
	if !ok {
		t.Fatal("registration missing")
	}
	if len(got.Contracts) != 1 || got.Contracts[0].MessageType.Key() != "agentic.agent_lesson.v1" {
		t.Fatalf("stored contracts = %#v, want one contract keyed agentic.agent_lesson.v1", got.Contracts)
	}

	t.Run("a contract naming another key is refused", func(t *testing.T) {
		reg := New()
		r := attrRegistration("agentic", "agent_lesson", "v1")
		r.Contracts = []contract.Contract{lessonLikeContract("agentic.lesson-record", types.Type{Domain: "agentic", Category: "loop_execution", Version: "v1"})}
		err := reg.Register(r)
		if err == nil {
			t.Fatal("Register accepted a contract naming a different key")
		}
		for _, key := range []string{"agentic.agent_lesson.v1", "agentic.loop_execution.v1"} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error does not name %s: %v", key, err)
			}
		}
		if _, ok := reg.GetRegistration("agentic.agent_lesson.v1"); ok {
			t.Fatal("a rejected registration was stored")
		}
	})

	t.Run("duplicate contract names within one registration are refused", func(t *testing.T) {
		reg := New()
		r := attrRegistration("agentic", "agent_lesson", "v1")
		r.Contracts = []contract.Contract{
			lessonLikeContract("agentic.lesson-record", types.Type{}),
			lessonLikeContract("agentic.lesson-record", types.Type{}),
		}
		if err := reg.Register(r); err == nil || !strings.Contains(err.Error(), "agentic.lesson-record") {
			t.Fatalf("Register = %v, want duplicate contract name error", err)
		}
	})

	t.Run("a contract with an invalid shape is refused", func(t *testing.T) {
		reg := New()
		r := attrRegistration("agentic", "agent_lesson", "v1")
		r.Contracts = []contract.Contract{lessonLikeContract("", types.Type{})}
		if err := reg.Register(r); err == nil {
			t.Fatal("Register accepted a contract with no name")
		}
	})
}

func TestGetRegistrationCopiesAttributes(t *testing.T) {
	const key = "agentic.agent_lesson.v1"
	reg := New()
	r := attrRegistration("agentic", "agent_lesson", "v1")
	r.IndexingProfile = vocabulary.IndexingProfileContent
	r.Contracts = []contract.Contract{lessonLikeContract("agentic.lesson-record", types.Type{})}
	if err := reg.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first, ok := reg.GetRegistration(key)
	if !ok {
		t.Fatal("registration missing")
	}
	if first.IndexingProfile != vocabulary.IndexingProfileContent {
		t.Fatalf("IndexingProfile = %q, want content", first.IndexingProfile)
	}
	first.Contracts[0].BirthPredicates[0] = "mutated"
	first.Contracts[0].Name = "mutated"

	second, _ := reg.GetRegistration(key)
	if second.Contracts[0].BirthPredicates[0] == "mutated" || second.Contracts[0].Name == "mutated" {
		t.Fatal("GetRegistration leaked its contract slice to the caller")
	}

	listed, ok := reg.List()[key]
	if !ok || listed.IndexingProfile != vocabulary.IndexingProfileContent || len(listed.Contracts) != 1 {
		t.Fatalf("List() entry = %#v, want profile and one contract", listed)
	}
	byDomain := reg.ListByDomain("agentic")
	if len(byDomain) != 1 || byDomain[0].IndexingProfile != vocabulary.IndexingProfileContent || len(byDomain[0].Contracts) != 1 {
		t.Fatalf("ListByDomain() = %#v, want profile and one contract", byDomain)
	}
}

func TestContractsReturnsIndependentSortedCopies(t *testing.T) {
	reg := New()
	loop := attrRegistration("agentic", "loop_execution", "v1")
	loopContract := func(name string) contract.Contract {
		return contract.Contract{
			Name: name, EntityPattern: "*.*.agent.agentic-loop.execution.*",
			BirthPredicates: []string{"agent.loop.role"},
		}
	}
	loop.Contracts = []contract.Contract{loopContract("zeta"), loopContract("alpha")}
	lesson := attrRegistration("agentic", "agent_lesson", "v1")
	lesson.Contracts = []contract.Contract{lessonLikeContract("agentic.lesson-record", types.Type{})}
	if err := reg.Register(loop); err != nil {
		t.Fatalf("Register loop: %v", err)
	}
	if err := reg.Register(lesson); err != nil {
		t.Fatalf("Register lesson: %v", err)
	}

	got := reg.Contracts()
	wantNames := []string{"agentic.lesson-record", "alpha", "zeta"}
	wantKeys := []string{"agentic.agent_lesson.v1", "agentic.loop_execution.v1", "agentic.loop_execution.v1"}
	if len(got) != len(wantNames) {
		t.Fatalf("Contracts() returned %d contracts, want %d", len(got), len(wantNames))
	}
	for i := range got {
		if got[i].Name != wantNames[i] || got[i].MessageType.Key() != wantKeys[i] {
			t.Fatalf("Contracts()[%d] = %s/%s, want %s/%s (ordered by key then name)",
				i, got[i].MessageType.Key(), got[i].Name, wantKeys[i], wantNames[i])
		}
	}

	got[0].BirthPredicates[0] = "mutated"
	got[0].Name = "mutated"
	again := reg.Contracts()
	if again[0].BirthPredicates[0] == "mutated" || again[0].Name == "mutated" {
		t.Fatal("Contracts() leaked its slices to the caller")
	}
}

func TestIndexingProfileFor(t *testing.T) {
	reg := New()
	withFloor := attrRegistration("agentic", "request", "v1")
	withFloor.IndexingProfile = vocabulary.IndexingProfileTrace
	noFloor := attrRegistration("test", "nofloor", "v1")
	for _, r := range []*Registration{withFloor, noFloor} {
		if err := reg.Register(r); err != nil {
			t.Fatalf("Register %s: %v", r.MessageType(), err)
		}
	}
	cases := []struct {
		key        string
		profile    string
		registered bool
	}{
		{"agentic.request.v1", vocabulary.IndexingProfileTrace, true},
		{"test.nofloor.v1", "", true},
		// A binary that did not register graph research knows no research floor.
		{"research.result.v1", "", false},
	}
	for _, tc := range cases {
		profile, registered := reg.IndexingProfileFor(tc.key)
		if profile != tc.profile || registered != tc.registered {
			t.Errorf("IndexingProfileFor(%q) = (%q, %v), want (%q, %v)",
				tc.key, profile, registered, tc.profile, tc.registered)
		}
	}
}

// TestRegisterRejectsSchemaMismatch pins the existing factory/registration
// agreement check (validateSchemaConsistency) named by the payload-registry
// delta; GREEN at baseline.
func TestRegisterRejectsSchemaMismatch(t *testing.T) {
	reg := New()
	r := &Registration{
		Domain: "agentic", Category: "agent_lesson", Version: "v1",
		Factory: func() any {
			return &attrPayload{key: types.Type{Domain: "agentic", Category: "loop_execution", Version: "v1"}}
		},
	}
	err := reg.Register(r)
	if err == nil {
		t.Fatal("Register accepted a factory whose Schema() disagrees with the registration")
	}
	for _, tuple := range []string{"loop_execution", "agent_lesson"} {
		if !strings.Contains(err.Error(), tuple) {
			t.Errorf("error does not name %s: %v", tuple, err)
		}
	}
}

// TestRegisterRejectsMalformedComponent (Codex round, MEDIUM): a component
// holding the key separator would register a key nothing can bind a contract
// to; Register refuses it at boot, naming the component.
func TestRegisterRejectsMalformedComponent(t *testing.T) {
	for _, tc := range []struct{ name, domain, category, version string }{
		{"dotted domain", "bad.domain", "kind", "v1"},
		{"dotted category", "domain", "bad.kind", "v1"},
		{"dotted version", "domain", "kind", "v.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := New()
			err := reg.Register(&Registration{
				Domain: tc.domain, Category: tc.category, Version: tc.version,
				Factory: func() any { return &struct{}{} },
			})
			if err == nil {
				t.Fatalf("Register accepted a component containing the separator")
			}
			if !strings.Contains(err.Error(), `"."`) {
				t.Fatalf("error does not name the separator: %v", err)
			}
			if len(reg.List()) != 0 {
				t.Fatal("a rejected registration was stored")
			}
		})
	}
}

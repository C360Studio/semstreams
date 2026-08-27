package types

import (
	"strings"
	"testing"
)

// TestTypeValidateOwnsComponentGrammar: Validate is the one owner of message
// type component grammar — every component non-empty and free of the "."
// separator, so a Type always round-trips through Key().
func TestTypeValidateOwnsComponentGrammar(t *testing.T) {
	if err := (Type{Domain: "agentic", Category: "agent_lesson", Version: "v1"}).Validate(); err != nil {
		t.Fatalf("well-formed type rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		mt   Type
		want string
	}{
		{"empty domain", Type{Category: "kind", Version: "v1"}, "domain"},
		{"empty category", Type{Domain: "d", Version: "v1"}, "category"},
		{"empty version", Type{Domain: "d", Category: "kind"}, "version"},
		{"dotted domain", Type{Domain: "bad.domain", Category: "kind", Version: "v1"}, "bad.domain"},
		{"dotted category", Type{Domain: "d", Category: "a.b", Version: "v1"}, "a.b"},
		{"dotted version", Type{Domain: "d", Category: "kind", Version: "v.1"}, "v.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mt.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want an error naming %q", err, tc.want)
			}
		})
	}
}

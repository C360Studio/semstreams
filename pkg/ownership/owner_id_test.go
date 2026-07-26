package ownership

import (
	"errors"
	"testing"
)

func TestValidateOwnerIDUsesCanonicalSubjectSafetyRules(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"owner-a", "Owner_2", "component.writer=v1"} {
		if err := ValidateOwnerID(owner); err != nil {
			t.Fatalf("ValidateOwnerID(%q): %v", owner, err)
		}
	}
	for _, owner := range []string{"", "owner with space", "owner.*", "owner>"} {
		if err := ValidateOwnerID(owner); !errors.Is(err, ErrInvalidClaim) {
			t.Fatalf("ValidateOwnerID(%q) = %v, want ErrInvalidClaim", owner, err)
		}
	}
}

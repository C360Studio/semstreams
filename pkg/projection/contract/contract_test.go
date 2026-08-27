package contract

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

func TestContractValidateUsesVocabularyProfiles(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	base := Contract{
		Name: "test", EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.value.name"},
	}
	for _, profile := range []string{
		vocabulary.IndexingProfileContent, vocabulary.IndexingProfileControl,
		vocabulary.IndexingProfileSignal, vocabulary.IndexingProfileTrace,
	} {
		c := base
		c.IndexingProfile = profile
		if err := c.Validate(); err != nil {
			t.Errorf("Validate() with profile %q = %v, want nil", profile, err)
		}
		if err := c.ValidateShape(); err != nil {
			t.Errorf("ValidateShape() with profile %q = %v, want nil", profile, err)
		}
	}
	c := base
	c.IndexingProfile = "prose"
	if err := c.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("Validate() with profile \"prose\" = %v, want invalid contract", err)
	}
	if err := c.ValidateShape(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("ValidateShape() with profile \"prose\" = %v, want invalid contract", err)
	}
}

// ValidateShape is everything Validate does except predicate declaration, so a
// registration can validate a contract before the vocabulary is populated.
func TestValidateShapeSkipsPredicateDeclaration(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	c := Contract{
		Name: "test", EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.undeclared.name"},
	}
	if err := c.ValidateShape(); err != nil {
		t.Fatalf("ValidateShape() = %v, want nil for an undeclared but well-formed predicate", err)
	}
	if err := c.Validate(); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("Validate() = %v, want invalid contract for an undeclared predicate", err)
	}
}

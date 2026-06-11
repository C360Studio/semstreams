package vocabulary

import "testing"

func TestIsValidIndexingProfile(t *testing.T) {
	valid := []string{
		IndexingProfileContent,
		IndexingProfileControl,
		IndexingProfileSignal,
		IndexingProfileTrace,
	}
	for _, v := range valid {
		if !IsValidIndexingProfile(v) {
			t.Errorf("IsValidIndexingProfile(%q) = false, want true", v)
		}
	}

	invalid := []string{"", "Content", "CONTROL", "audit", "lifecycle", " content", "content "}
	for _, v := range invalid {
		if IsValidIndexingProfile(v) {
			t.Errorf("IsValidIndexingProfile(%q) = true, want false", v)
		}
	}
}

// The reserved predicate must stay on the documented dotted path so consumers
// and the grammar-collision audit agree on it.
func TestEntityIndexingProfilePredicate(t *testing.T) {
	if EntityIndexingProfile != "entity.indexing.profile" {
		t.Errorf("EntityIndexingProfile = %q, want entity.indexing.profile", EntityIndexingProfile)
	}
}

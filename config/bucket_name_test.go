package config

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/c360studio/semstreams/pkg/errs"
)

// natsBucketName is the NATS KV bucket grammar, transcribed from the pinned
// client (nats.go v1.52.0 jetstream/kv.go validBucketRe), not from BucketName.
var natsBucketName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

const segmentAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

// drawSegment draws one canonical entity-ID segment of exactly n bytes:
// alphanumeric first, then alphanumeric, '_' or '-' (pkg/types entity_id.go).
func drawSegment(t *rapid.T, n int, label string) string {
	var b strings.Builder
	b.WriteByte(segmentAlphabet[rapid.IntRange(0, 61).Draw(t, label+"First")])
	for i := 1; i < n; i++ {
		b.WriteByte(segmentAlphabet[rapid.IntRange(0, len(segmentAlphabet)-1).Draw(t, label+"Rest")])
	}
	return b.String()
}

// TestBucketNameIsLegalForEveryValidPair: every pair configuration load admits
// names a legal NATS bucket in the ruled format. The pair length is drawn
// boundary-hugging so the 163-byte declared budget itself is always reachable.
//
// spec: component-runtime-config / The configuration bucket is named by the declared authority pair
func TestBucketNameIsLegalForEveryValidPair(t *testing.T) {
	maxPair := maxDeclarableAuthorityPairBytes()
	rapid.Check(t, func(t *rapid.T) {
		pair := rapid.OneOf(
			rapid.IntRange(2, maxPair),
			rapid.SampledFrom([]int{2, maxPair - 1, maxPair}),
		).Draw(t, "pairBytes")
		orgBytes := rapid.IntRange(1, pair-1).Draw(t, "orgBytes")
		org := drawSegment(t, orgBytes, "org")
		stem := drawSegment(t, pair-orgBytes, "stem")
		if validateDeclaredAuthorityPair(org, stem) != nil || validateAuthorityPair(org, stem) != nil {
			t.Fatalf("the generator must stay inside what load admits: %q/%q", org, stem)
		}

		name, err := BucketName(org, stem)
		if err != nil {
			t.Fatalf("BucketName(%q, %q): %v", org, stem, err)
		}
		if !natsBucketName.MatchString(name) {
			t.Fatalf("%q is not a legal NATS bucket name", name)
		}
		if len(name) > 252 {
			t.Fatalf("%q is %d bytes; NATS accepts at most 252", name, len(name))
		}
		if want := "semstreams_config_" + org + "_" + stem; name != want {
			t.Fatalf("BucketName(%q, %q) = %q, want the ruled format %q", org, stem, name, want)
		}
	})
}

func TestBucketNameRefusesWhatNATSCannotName(t *testing.T) {
	for _, tt := range []struct{ name, org, stem string }{
		{"empty org", "", "dep"},
		{"empty stem", "acme", ""},
		{"dot is not legal in a bucket name", "acme", "dep.one"},
		{"space", "acme", "dep one"},
		{"wildcard", "acme", "dep*"},
		{"non-ASCII", "acmé", "dep"},
		{"longer than NATS accepts", strings.Repeat("a", 200), strings.Repeat("b", 40)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BucketName(tt.org, tt.stem)
			if err == nil {
				t.Fatalf("BucketName(%q, %q) must refuse", tt.org, tt.stem)
			}
			if !errs.IsInvalid(err) {
				t.Fatalf("refusal must be an invalid-configuration error, got %v", err)
			}
		})
	}
}

// FuzzBucketName: never panics; a success is legal and in the ruled format; a
// refusal is a typed invalid-configuration error.
func FuzzBucketName(f *testing.F) {
	for _, seed := range [][2]string{
		{"acme", "dep"}, {"c360", "semstreams-e2e-structural"}, {"a_b", "c"}, {"A", "B"},
		{"", "dep"}, {"acme", ""}, {"acme", "dep.one"}, {"acme", "dep>"}, {"acme", "dep*"},
		{"acmé", "dep"}, {strings.Repeat("a", 200), strings.Repeat("b", 40)}, {"acme", "dep\x00"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, org, stem string) {
		name, err := BucketName(org, stem)
		if err != nil {
			if !errs.IsInvalid(err) {
				t.Fatalf("refusal must be an invalid-configuration error, got %v", err)
			}
			return
		}
		if !natsBucketName.MatchString(name) || len(name) > 252 {
			t.Fatalf("BucketName(%q, %q) = %q is not a legal NATS bucket name", org, stem, name)
		}
		if name != "semstreams_config_"+org+"_"+stem {
			t.Fatalf("BucketName(%q, %q) = %q is not the ruled format", org, stem, name)
		}
	})
}

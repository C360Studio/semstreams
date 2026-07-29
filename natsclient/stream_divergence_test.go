package natsclient

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffDeclaredStream(t *testing.T) {
	live := jetstream.StreamConfig{
		Name:       "SHARED",
		Subjects:   []string{"shared.>"},
		Storage:    jetstream.MemoryStorage,
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     48 * time.Hour,
		MaxBytes:   8 << 20,
		Discard:    jetstream.DiscardOld,
		Duplicates: 2 * time.Minute,
		Replicas:   1,
	}

	t.Run("a declaration that matches reports nothing", func(t *testing.T) {
		assert.Empty(t, DiffDeclaredStream(live, live))
	})

	// The motivating case: two components declaring one stream with different
	// retention. Whoever booted first won, and before this nothing said so.
	t.Run("a diverging declared field is reported with both values", func(t *testing.T) {
		declared := jetstream.StreamConfig{
			Name:     "SHARED",
			Subjects: []string{"shared.>"},
			MaxAge:   24 * time.Hour,
		}

		got := DiffDeclaredStream(declared, live)

		require.Len(t, got, 1)
		assert.Equal(t, "MaxAge", got[0].Field)
		assert.Equal(t, "24h0m0s", got[0].Declared)
		assert.Equal(t, "48h0m0s", got[0].Observed)
	})

	// A zero field is SILENCE, not a declaration of zero. Comparing it would fire
	// on nearly every bind — which is how a real signal gets tuned out.
	t.Run("an undeclared field is not a divergence", func(t *testing.T) {
		declared := jetstream.StreamConfig{Name: "SHARED", Subjects: []string{"shared.>"}}

		assert.Empty(t, DiffDeclaredStream(declared, live),
			"omitting MaxAge is not asking for unlimited retention")
	})

	t.Run("subjects are compared as a set", func(t *testing.T) {
		reordered := jetstream.StreamConfig{
			Name:     "SHARED",
			Subjects: []string{"b.>", "a.>"},
		}
		observed := jetstream.StreamConfig{Subjects: []string{"a.>", "b.>"}}
		assert.Empty(t, DiffDeclaredStream(reordered, observed),
			"subject order is not meaningful to JetStream; reporting a reordering would be noise")

		missing := jetstream.StreamConfig{Name: "SHARED", Subjects: []string{"a.>", "c.>"}}
		got := DiffDeclaredStream(missing, observed)
		require.Len(t, got, 1)
		assert.Equal(t, "Subjects", got[0].Field,
			"a stream that does not capture the caller's subject fails every publish")
	})

	// An observed zero is the server saying "no limit", and rendering it as "0s"
	// or "0" invites a reader to see a bound of zero instead of none. -1 is what a
	// server actually reports for a byte limit nobody set.
	t.Run("an unlimited observed value is named, not printed as zero", func(t *testing.T) {
		declared := jetstream.StreamConfig{Name: "S", MaxAge: time.Hour, MaxBytes: 1 << 20}
		observed := jetstream.StreamConfig{MaxAge: 0, MaxBytes: -1}

		got := DiffDeclaredStream(declared, observed)

		require.Len(t, got, 2)
		byField := map[string]StreamFieldDivergence{}
		for _, d := range got {
			byField[d.Field] = d
		}
		assert.Equal(t, "unlimited", byField["MaxAge"].Observed)
		assert.Equal(t, "unlimited (-1)", byField["MaxBytes"].Observed)
	})

	// NOTE: this asserts the compared set is what the implementation INTENDS, not
	// that it is exhaustive over jetstream.StreamConfig — it cannot, since it lists
	// the same names the implementation does. The fields deliberately left out are
	// named in DiffDeclaredStream's doc comment; widening the set means editing both.
	t.Run("every field in the compared set is reported", func(t *testing.T) {
		declared := jetstream.StreamConfig{
			Name:              "S",
			MaxAge:            time.Hour,
			MaxBytes:          1 << 20,
			Duplicates:        time.Minute,
			MaxMsgs:           10,
			MaxMsgsPerSubject: 2,
			MaxMsgSize:        1024,
			Replicas:          3,
			Storage:           jetstream.MemoryStorage,
			Retention:         jetstream.WorkQueuePolicy,
			Discard:           jetstream.DiscardNew,
		}
		observed := jetstream.StreamConfig{
			MaxAge:            2 * time.Hour,
			MaxBytes:          2 << 20,
			Duplicates:        2 * time.Minute,
			MaxMsgs:           20,
			MaxMsgsPerSubject: 4,
			MaxMsgSize:        2048,
			Replicas:          1,
			Storage:           jetstream.FileStorage,
			Retention:         jetstream.LimitsPolicy,
			Discard:           jetstream.DiscardOld,
		}

		got := DiffDeclaredStream(declared, observed)

		fields := make([]string, 0, len(got))
		for _, d := range got {
			fields = append(fields, d.Field)
			assert.NotEmpty(t, d.Declared, "%s must render what was asked for", d.Field)
			assert.NotEmpty(t, d.Observed, "%s must render what is live", d.Field)
		}
		assert.ElementsMatch(t, []string{
			"MaxAge", "MaxBytes", "Duplicates", "MaxMsgs", "MaxMsgsPerSubject",
			"MaxMsgSize", "Replicas", "Storage", "Retention", "Discard",
		}, fields)
	})

	// The honest limit, asserted so it is a known property rather than a surprise.
	// jetstream.StreamConfig's enum zero values are MEANINGFUL, so a caller who
	// deliberately wanted delete-oldest, file storage or limits retention is
	// byte-identical to one who said nothing, and the divergence cannot be seen.
	t.Run("a zero-valued enum declaration is invisible in one direction", func(t *testing.T) {
		wantedOld := jetstream.StreamConfig{Name: "S", Discard: jetstream.DiscardOld}
		liveNew := jetstream.StreamConfig{Discard: jetstream.DiscardNew}

		assert.Empty(t, DiffDeclaredStream(wantedOld, liveNew),
			"DiscardOld is the field's zero value, so this declaration is unobservable")

		// The other direction IS visible, which is the half that can be reported.
		wantedNew := jetstream.StreamConfig{Name: "S", Discard: jetstream.DiscardNew}
		liveOld := jetstream.StreamConfig{Discard: jetstream.DiscardOld}
		got := DiffDeclaredStream(wantedNew, liveOld)
		require.Len(t, got, 1)
		assert.Equal(t, "new", got[0].Declared)
		assert.Equal(t, "old", got[0].Observed)
	})
}

func TestStreamFieldDivergence_RendersBothSides(t *testing.T) {
	d := StreamFieldDivergence{Field: "MaxAge", Declared: "24h0m0s", Observed: "unlimited"}

	assert.Equal(t, "MaxAge: declared=24h0m0s observed=unlimited", d.String())
	assert.Equal(t, []string{d.String()}, DivergenceLabels([]StreamFieldDivergence{d}))
}

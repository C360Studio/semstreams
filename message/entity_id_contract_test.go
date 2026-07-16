package message

import (
	"strings"
	"testing"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestEntityIDDelegatorsMatchCanonicalAuthority(t *testing.T) {
	t.Parallel()

	boundary := func(size int) string {
		return "a.a.a.a.a." + strings.Repeat("x", size-10)
	}
	corpus := []string{
		"a.b.c.d.e.f",
		boundary(255),
		boundary(256),
		boundary(257),
		"a._b.c.d.e.f",
		"a.b.c.d.e.fé",
		"a.b.c.d.e.*",
		"a..c.d.e.f",
		"",
	}
	for _, value := range corpus {
		t.Run(value, func(t *testing.T) {
			want := semtypes.IsValidEntityID(value)
			assert.Equal(t, want, IsValidEntityID(value))
			got, gotErr := ParseEntityID(value)
			canonical, canonicalErr := semtypes.ParseEntityID(value)
			assert.Equal(t, canonicalErr == nil, gotErr == nil)
			assert.Equal(t, canonical, got)
		})
	}
}

package fixtures_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/cmd/e2e-semstreams/fixtures"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/vocabulary"
)

// TestFixturesRegisterEveryE2EStamp: every key an e2e scenario stamps on
// entity.create registers into a fresh registry with floor control and
// round-trips through the production decoder as a verbatim carrier.
func TestFixturesRegisterEveryE2EStamp(t *testing.T) {
	reg := payloadregistry.NewWithSubset(t, fixtures.RegisterPayloads)
	keys := []string{
		"test.fixture.v1",
		"e2e.probe.v1",
		"e2e.eventtime.v1",
		"e2e.canonical_create_contract.v1",
		"e2e.relationship_contract.v1",
		"research.e2e_search_seed.v1",
	}
	const id = "c360.e2e.fixture.system.widget.001"
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			registration, ok := reg.GetRegistration(key)
			require.Truef(t, ok, "%s is not registered by the e2e fixtures", key)
			assert.Equal(t, vocabulary.IndexingProfileControl, registration.IndexingProfile)

			parts := strings.SplitN(key, ".", 3)
			mt := message.Type{Domain: parts[0], Category: parts[1], Version: parts[2]}
			carrier := &fixtures.Carrier{Type: mt, ID: id, Facts: []message.Triple{
				{Subject: id, Predicate: "test.state.value", Object: "born", Source: "e2e", Timestamp: at, Confidence: 1},
			}}
			require.NoError(t, carrier.Validate())

			base := message.NewBaseMessage(carrier.Schema(), carrier, "test")
			data, err := json.Marshal(base)
			require.NoError(t, err)
			decoded, err := message.NewDecoder(reg).Decode(data)
			require.NoError(t, err)
			got, ok := decoded.Payload().(*fixtures.Carrier)
			require.Truef(t, ok, "decoded payload must be *fixtures.Carrier, got %T", decoded.Payload())
			assert.Equal(t, mt, got.Schema())
			assert.Equal(t, id, got.EntityID())
			assert.Equal(t, carrier.Facts, got.Triples())
		})
	}
}

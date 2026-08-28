//go:build integration

package lifecycle_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
)

// gateMission is the participant this test births; it mirrors the package's
// internal fixture so the production projection path is exercised unchanged.
type gateMission struct {
	ID         string    `json:"entity_id" lifecycle:"id"`
	PhaseF     string    `json:"phase" lifecycle:"phase,predicate=mission.lifecycle.phase"`
	OwnerOrgID string    `json:"owner_org_id,omitempty" lifecycle:"operator_writable,predicate=mission.identity.owner-org-id"`
	LastAt     time.Time `json:"last_at,omitempty" lifecycle:"readonly,predicate=mission.transition.at"`
}

func (m *gateMission) EntityID() string       { return m.ID }
func (m *gateMission) Workflow() string       { return "gate-fixture" }
func (m *gateMission) Phase() string          { return m.PhaseF }
func (m *gateMission) IsTerminal() bool       { return false }
func (m *gateMission) ParentEntityID() string { return "" }

func gateWorkflow() lifecycle.Workflow {
	return lifecycle.Workflow{
		Name:            "gate-fixture",
		EntityIDPattern: "*.*.lifecycle.gcs.mission.*",
		Phases:          []string{"planning", "flying", "completed"},
		Transitions: lifecycle.Transitions{
			"planning":  {"flying"},
			"flying":    {"completed"},
			"completed": {},
		},
		PhasePredicate: "mission.lifecycle.phase",
		Schema:         reflect.TypeOf(gateMission{}),
		OperatorWritablePredicates: []string{
			"mission.identity.owner-org-id",
		},
		AuditPredicates: lifecycle.AuditSpec{ // predicate-audit:unrelated {"column":20,"surface":"go-field:AuditPredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
			Source: "mission.transition.source",
			At:     "mission.transition.at",
			From:   "mission.transition.from",
			Note:   "mission.transition.note",
		},
	}
}

// unregisteredRejections sums graph-ingest's process-global
// mutation_rejections_total{reason="message_type_unregistered"} across
// subjects, read from the default registerer graph-ingest registers on when
// no MetricsRegistry is injected.
func unregisteredRejections(t *testing.T) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var total float64
	for _, family := range families {
		if family.GetName() != "semstreams_graph_ingest_mutation_rejections_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "reason" && label.GetValue() == graph.ErrorCodeMessageTypeUnregistered {
					total += metric.GetCounter().GetValue()
				}
			}
		}
	}
	return total
}

// TestHarnessBirthPassesRegisteredTypeGate drives Manager.Create against a
// REAL graph-ingest constructed with the builtin payload set: the birth is
// admitted, the stored stamp is lifecycle.harness.v1, and the
// message_type_unregistered rejection counter does not move. A
// HarnessMessageType() that the builtin set does not register fails here.
func TestHarnessBirthPassesRegisteredTypeGate(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}),
	)
	client := tc.Client

	configJSON, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{
		NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		// graph-ingest refuses an absent deployment authority (ADR-102 d5) and
		// rejects any subject outside it, so the fixture pair must match the
		// entity IDs this file uses.
		Platform: component.PlatformMeta{Org: "c360", Platform: "platform1"},
	})
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	t.Cleanup(func() { _ = ingest.Stop(context.Background()) })
	require.NoError(t, tc.GetNativeConnection().Flush())

	before := unregisteredRejections(t)

	mgr := lifecycle.NewManager(client, nil)
	require.NoError(t, mgr.Register(gateWorkflow()))
	const id = "c360.platform1.lifecycle.gcs.mission.gate-001"
	require.NoError(t, mgr.Create(ctx, &gateMission{ID: id, PhaseF: "planning", OwnerOrgID: "acme"}),
		"a harness birth must pass graph-ingest's registered-type gate")

	js, err := client.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	entry, err := kv.Get(ctx, id)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &stored))
	assert.Equal(t, lifecycle.HarnessMessageType(), stored.MessageType)
	assert.Equal(t, "lifecycle.harness.v1", stored.MessageType.Key())

	assert.Equal(t, before, unregisteredRejections(t),
		"mutation_rejections_total{reason=message_type_unregistered} must not move for a harness birth")
}

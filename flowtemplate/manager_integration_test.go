//go:build integration

package flowtemplate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360studio/semstreams/natsclient"
)

type ManagerIntegrationSuite struct {
	suite.Suite
	testClient *natsclient.TestClient
	natsClient *natsclient.Client
	manager    *Manager
	ctx        context.Context
	cancel     context.CancelFunc
}

func (s *ManagerIntegrationSuite) SetupSuite() {
	s.testClient = natsclient.NewTestClient(s.T(),
		natsclient.WithJetStream(),
		natsclient.WithKV())
	s.natsClient = s.testClient.Client
}

func (s *ManagerIntegrationSuite) SetupTest() {
	var err error
	s.manager, err = NewManager(s.natsClient)
	s.Require().NoError(err)
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *ManagerIntegrationSuite) TearDownTest() {
	// Clean up records between tests using a fresh context — s.ctx is
	// the test's context and cancelling it first would short-circuit the
	// Keys/Delete calls below.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if keys, err := s.manager.kvStore.Keys(cleanupCtx); err == nil {
		for _, k := range keys {
			_ = s.manager.kvStore.Delete(cleanupCtx, k)
		}
	}
	s.cancel()
}

// validBody is a minimal template body that survives Validate (text/template
// parse) and Instantiate (JSON unmarshal into flowstore.Flow). Keeps the
// integration suite focused on Manager KV behaviour, not template syntax.
const validBody = `{"id": "{{.FlowID}}", "name": "{{.FlowName}}", "nodes": [], "connections": []}`

// TestCreateAndGet — stored template round-trips every field including
// nested Parameters.
func (s *ManagerIntegrationSuite) TestCreateAndGet() {
	t := &Template{
		ID:          "research",
		Name:        "Research pipeline",
		Description: "Parameterised research flow.",
		Body:        validBody,
		Parameters: []Parameter{
			{Name: "FlowID", Default: "research-flow"},
			{Name: "FlowName", Default: "Research", Required: false},
		},
	}

	s.Require().NoError(s.manager.Create(s.ctx, t))

	got, err := s.manager.Get(s.ctx, "research")
	s.Require().NoError(err)
	s.Equal("research", got.ID)
	s.Equal("Research pipeline", got.Name)
	s.Equal("Parameterised research flow.", got.Description)
	s.Equal(validBody, got.Body)
	s.Len(got.Parameters, 2)
	s.Equal("FlowID", got.Parameters[0].Name)
	s.Equal("research-flow", got.Parameters[0].Default)
}

func (s *ManagerIntegrationSuite) TestCreateRejectsDuplicate() {
	t := &Template{ID: "dup", Name: "x", Body: validBody}
	s.Require().NoError(s.manager.Create(s.ctx, t))

	again := &Template{ID: "dup", Name: "different", Body: validBody}
	err := s.manager.Create(s.ctx, again)
	s.Require().Error(err)
	s.Contains(err.Error(), "already exists")
}

func (s *ManagerIntegrationSuite) TestCreateRejectsInvalid() {
	// Missing body.
	err := s.manager.Create(s.ctx, &Template{ID: "x", Name: "y"})
	s.Require().Error(err)
	s.Contains(err.Error(), "body is required")

	// Malformed template body (unclosed action).
	err = s.manager.Create(s.ctx, &Template{ID: "x", Name: "y", Body: "{{.Unclosed"})
	s.Require().Error(err)
	s.Contains(err.Error(), "text/template")
}

func (s *ManagerIntegrationSuite) TestUpdateReplacesFields() {
	orig := &Template{ID: "t", Name: "v1", Body: validBody}
	s.Require().NoError(s.manager.Create(s.ctx, orig))

	updated := &Template{
		ID: "t", Name: "v2",
		Description: "updated",
		Body:        validBody,
	}
	s.Require().NoError(s.manager.Update(s.ctx, updated))

	got, err := s.manager.Get(s.ctx, "t")
	s.Require().NoError(err)
	s.Equal("v2", got.Name)
	s.Equal("updated", got.Description)
}

func (s *ManagerIntegrationSuite) TestUpdateMissingFails() {
	err := s.manager.Update(s.ctx, &Template{ID: "does-not-exist", Name: "x", Body: validBody})
	s.Require().Error(err)
}

func (s *ManagerIntegrationSuite) TestDelete() {
	t := &Template{ID: "transient", Name: "x", Body: validBody}
	s.Require().NoError(s.manager.Create(s.ctx, t))
	s.Require().NoError(s.manager.Delete(s.ctx, "transient"))

	_, err := s.manager.Get(s.ctx, "transient")
	s.Require().Error(err)
}

func (s *ManagerIntegrationSuite) TestListPopulatedReturnsAll() {
	s.Require().NoError(s.manager.Create(s.ctx, &Template{ID: "a", Name: "A", Body: validBody}))
	s.Require().NoError(s.manager.Create(s.ctx, &Template{ID: "b", Name: "B", Body: validBody}))

	all, err := s.manager.List(s.ctx)
	s.Require().NoError(err)
	s.Len(all, 2)
	s.Equal("A", all["a"].Name)
	s.Equal("B", all["b"].Name)
}

// TestInstantiateRoundTrip — end-to-end: stored template renders into a
// concrete flow via Manager.Get + Template.Instantiate. Catches regressions
// where serialisation drops the Body contents or Parameters reordering.
func (s *ManagerIntegrationSuite) TestInstantiateRoundTrip() {
	t := &Template{
		ID:   "rt",
		Name: "Round-trip",
		Body: validBody,
		Parameters: []Parameter{
			{Name: "FlowID", Default: "rt-flow"},
			{Name: "FlowName", Default: "Round Trip"},
		},
	}
	s.Require().NoError(s.manager.Create(s.ctx, t))

	loaded, err := s.manager.Get(s.ctx, "rt")
	s.Require().NoError(err)

	flow, err := loaded.Instantiate(map[string]string{"FlowID": "overridden"})
	s.Require().NoError(err)
	s.Equal("overridden", flow.ID)
	s.Equal("Round Trip", flow.Name) // default preserved
}

func TestManagerIntegrationSuite(t *testing.T) {
	suite.Run(t, new(ManagerIntegrationSuite))
}

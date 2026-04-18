//go:build integration

package persona

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
	// Clean up PERSONAS bucket with a fresh context — s.ctx is the
	// test's context; cancelling it first would short-circuit cleanup.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if keys, err := s.manager.kvStore.Keys(cleanupCtx); err == nil {
		for _, k := range keys {
			_ = s.manager.kvStore.Delete(cleanupCtx, k)
		}
	}
	s.cancel()
}

// TestCreateAndGet — happy path: persona stored, retrievable, round-trips
// every field cleanly (the nil-vs-empty-slice and description-omitempty
// edges in Persona JSON tags).
func (s *ManagerIntegrationSuite) TestCreateAndGet() {
	p := &Persona{
		ID:          "role-researcher",
		Category:    100,
		Priority:    5,
		Content:     "You are a research agent.",
		Roles:       []string{"researcher", "analyst"},
		Description: "Stock researcher persona.",
	}

	s.Require().NoError(s.manager.Create(s.ctx, p))

	got, err := s.manager.Get(s.ctx, "role-researcher")
	s.Require().NoError(err)
	s.Equal("role-researcher", got.ID)
	s.Equal(100, got.Category)
	s.Equal(5, got.Priority)
	s.Equal("You are a research agent.", got.Content)
	s.ElementsMatch([]string{"researcher", "analyst"}, got.Roles)
	s.Equal("Stock researcher persona.", got.Description)
}

// TestCreateRejectsDuplicate — second Create with same ID must fail.
// The KV.Create path (not Put) is what enforces this; regression guard
// for the "oops we used Put" refactor hazard.
func (s *ManagerIntegrationSuite) TestCreateRejectsDuplicate() {
	p := &Persona{ID: "dup", Content: "first"}
	s.Require().NoError(s.manager.Create(s.ctx, p))

	again := &Persona{ID: "dup", Content: "second attempt"}
	err := s.manager.Create(s.ctx, again)
	s.Require().Error(err)
	s.Contains(err.Error(), "already exists")
}

// TestCreateRejectsInvalid — Validate fails before touching KV.
func (s *ManagerIntegrationSuite) TestCreateRejectsInvalid() {
	// Missing content.
	err := s.manager.Create(s.ctx, &Persona{ID: "x"})
	s.Require().Error(err)
	s.Contains(err.Error(), "content is required")

	// Missing id.
	err = s.manager.Create(s.ctx, &Persona{Content: "..."})
	s.Require().Error(err)
	s.Contains(err.Error(), "id is required")
}

// TestUpdateReplacesFields — updates apply to subsequent Get results.
// Also verifies Update without prior Create fails, matching the "no
// silent upsert" contract Manager.Update enforces via Get-first.
func (s *ManagerIntegrationSuite) TestUpdateReplacesFields() {
	orig := &Persona{ID: "role-reviewer", Content: "v1", Category: 100}
	s.Require().NoError(s.manager.Create(s.ctx, orig))

	updated := &Persona{ID: "role-reviewer", Content: "v2", Category: 100, Description: "added"}
	s.Require().NoError(s.manager.Update(s.ctx, updated))

	got, err := s.manager.Get(s.ctx, "role-reviewer")
	s.Require().NoError(err)
	s.Equal("v2", got.Content)
	s.Equal("added", got.Description)
}

func (s *ManagerIntegrationSuite) TestUpdateMissingFails() {
	err := s.manager.Update(s.ctx, &Persona{ID: "does-not-exist", Content: "x"})
	s.Require().Error(err)
}

// TestDelete — deleted personas are no longer Get-able.
func (s *ManagerIntegrationSuite) TestDelete() {
	p := &Persona{ID: "gone-soon", Content: "x"}
	s.Require().NoError(s.manager.Create(s.ctx, p))
	s.Require().NoError(s.manager.Delete(s.ctx, "gone-soon"))

	_, err := s.manager.Get(s.ctx, "gone-soon")
	s.Require().Error(err)
}

// TestDeleteEmptyID — guard against the empty-key footgun that would
// silently no-op on some KV backends or error on others.
func (s *ManagerIntegrationSuite) TestDeleteEmptyID() {
	err := s.manager.Delete(s.ctx, "")
	s.Require().Error(err)
}

// TestListEmpty — fresh bucket returns an empty map, not nil.
func (s *ManagerIntegrationSuite) TestListEmpty() {
	personas, err := s.manager.List(s.ctx)
	s.Require().NoError(err)
	s.NotNil(personas)
	s.Empty(personas)
}

// TestListPopulated — List round-trips every Create'd persona.
func (s *ManagerIntegrationSuite) TestListPopulated() {
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{ID: "a", Content: "aa"}))
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{ID: "b", Content: "bb", Roles: []string{"r1"}}))
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{ID: "c", Content: "cc"}))

	personas, err := s.manager.List(s.ctx)
	s.Require().NoError(err)
	s.Len(personas, 3)
	s.Equal("aa", personas["a"].Content)
	s.ElementsMatch([]string{"r1"}, personas["b"].Roles)
	s.Equal("cc", personas["c"].Content)
}

func TestManagerIntegrationSuite(t *testing.T) {
	suite.Run(t, new(ManagerIntegrationSuite))
}

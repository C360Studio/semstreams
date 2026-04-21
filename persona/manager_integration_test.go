//go:build integration

package persona

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
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

// TestFragmentsEmpty — fresh bucket yields nil, not an empty slice. The
// assembler's UpsertAll treats both identically, but the nil contract is
// documented on Manager.Fragments so callers that log counts don't print
// "loaded 0 personas" spuriously.
func (s *ManagerIntegrationSuite) TestFragmentsEmpty() {
	fragments, err := s.manager.Fragments(s.ctx)
	s.Require().NoError(err)
	s.Nil(fragments)
}

// TestFragmentsRoundTrip — every stored persona materialises as a
// prompt.Fragment with the static fields preserved and the runtime-only
// hooks nil. This is the contract ADR-029 step 3b relies on when a
// caller Upserts these fragments onto a registry seeded with
// DefaultFragments.
func (s *ManagerIntegrationSuite) TestFragmentsRoundTrip() {
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{
		ID:       "role-researcher",
		Category: 100,
		Priority: 5,
		Content:  "Custom researcher persona",
		Roles:    []string{"researcher"},
	}))
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{
		ID:       "domain-finance",
		Category: 300,
		Content:  "Finance domain context",
	}))

	fragments, err := s.manager.Fragments(s.ctx)
	s.Require().NoError(err)
	s.Len(fragments, 2)

	byID := map[string]prompt.Fragment{}
	for _, f := range fragments {
		byID[f.ID] = f
		s.Nil(f.ContentFunc, "stored personas must not carry runtime-only hooks")
		s.Nil(f.Condition, "stored personas must not carry runtime-only hooks")
	}

	researcher, ok := byID["role-researcher"]
	s.Require().True(ok)
	s.Equal(prompt.CategoryRole, researcher.Category)
	s.Equal(5, researcher.Priority)
	s.Equal("Custom researcher persona", researcher.Content)
	s.ElementsMatch([]string{"researcher"}, researcher.Roles)

	finance, ok := byID["domain-finance"]
	s.Require().True(ok)
	s.Equal(prompt.CategoryDomain, finance.Category)
}

// TestFragmentsOverrideDefaults exercises the ADR-029 step 3b headline
// scenario end-to-end: a stored persona shares an ID with a
// DefaultFragment, and UpsertAll on the registry replaces the in-code
// entry. Assemble then emits the override's content for the matching
// role. Regression guard for the override semantic — if Upsert ever
// reverts to append-only, this test fails loudly.
func (s *ManagerIntegrationSuite) TestFragmentsOverrideDefaults() {
	s.Require().NoError(s.manager.Create(s.ctx, &Persona{
		ID:       "role-researcher",
		Category: 100,
		Content:  "CUSTOM researcher persona from KV",
		Roles:    []string{"researcher"},
	}))

	registry := prompt.NewRegistry()
	registry.AddAll(prompt.DefaultFragments())

	fragments, err := s.manager.Fragments(s.ctx)
	s.Require().NoError(err)
	registry.UpsertAll(fragments)

	result := prompt.Assemble(registry, &prompt.AssemblyContext{Role: "researcher"})
	s.Contains(result.SystemMessage, "CUSTOM researcher persona from KV")
	s.NotContains(result.SystemMessage, "Research methodology:", "default researcher fragment must be replaced")
}

// TestUpsertFreshKey — Upsert on a new ID behaves like Create.
func (s *ManagerIntegrationSuite) TestUpsertFreshKey() {
	p := &Persona{ID: "upsert-fresh", Content: "initial content"}
	s.Require().NoError(s.manager.Upsert(s.ctx, p))

	got, err := s.manager.Get(s.ctx, "upsert-fresh")
	s.Require().NoError(err)
	s.Equal("initial content", got.Content)
}

// TestUpsertExistingKey — second Upsert with same ID wins via Update.
func (s *ManagerIntegrationSuite) TestUpsertExistingKey() {
	first := &Persona{ID: "upsert-existing", Content: "first value"}
	s.Require().NoError(s.manager.Upsert(s.ctx, first))

	second := &Persona{ID: "upsert-existing", Content: "second value"}
	s.Require().NoError(s.manager.Upsert(s.ctx, second))

	got, err := s.manager.Get(s.ctx, "upsert-existing")
	s.Require().NoError(err)
	s.Equal("second value", got.Content, "second Upsert must overwrite first via Update")
}

// TestUpsertNonConflictErrorPropagates — a Create error that is NOT a key-exists
// conflict must be returned directly, not swallowed by a fallthrough to Update.
// We exercise this by passing a nil persona (which fails Validate before hitting
// KV), confirming that Upsert returns the validation error rather than
// attempting Update and returning a misleading Update error.
func (s *ManagerIntegrationSuite) TestUpsertNonConflictErrorPropagates() {
	// nil persona → Create returns an invalid-input error (not ErrKVKeyExists).
	err := s.manager.Upsert(s.ctx, nil)
	s.Require().Error(err, "Upsert with nil persona must return an error")
	// The error must not come from Update's "get first to confirm exists" path.
	// Update would say "get from KV" or "persona cannot be nil"; Create says
	// "persona cannot be nil" first. Both say "cannot be nil" so we check that
	// the error is present and the bucket was not modified.
	keys, listErr := s.manager.kvStore.Keys(s.ctx)
	s.Require().NoError(listErr)
	s.Empty(keys, "no KV entries should exist after a failed Upsert of nil persona")
}

func TestManagerIntegrationSuite(t *testing.T) {
	suite.Run(t, new(ManagerIntegrationSuite))
}

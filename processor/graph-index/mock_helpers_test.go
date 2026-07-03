package graphindex

import "sync"

// mockRefs holds references to mock KV buckets injected into a Component during tests.
// Since Component fields are *natsclient.KVStore (not an interface), type assertions are
// impossible after injection. This registry maps the component pointer to its underlying
// mock buckets so tests can inspect state and inject errors.
type mockRefs struct {
	outgoing         *mockKVBucket
	incoming         *mockKVBucket
	alias            *mockKVBucket
	predicate        *mockKVBucket
	predicateCatalog *mockKVBucket
}

var (
	mockRegistry sync.Map // map[*Component]*mockRefs
)

// registerMocks associates the given mock buckets with the component for later retrieval.
func registerMocks(comp *Component, refs *mockRefs) {
	mockRegistry.Store(comp, refs)
}

// getMocks returns the mock refs associated with a component.
// Panics if none are registered — only valid for components created via createTestComponentWithMockKV.
func getMocks(comp *Component) *mockRefs {
	v, ok := mockRegistry.Load(comp)
	if !ok {
		panic("getMocks: no mock refs registered for component — was it created with createTestComponentWithMockKV?")
	}
	return v.(*mockRefs)
}

// outgoingMock retrieves the mock outgoing bucket for a test component.
func outgoingMock(comp *Component) *mockKVBucket {
	return getMocks(comp).outgoing
}

// incomingMock retrieves the mock incoming bucket for a test component.
func incomingMock(comp *Component) *mockKVBucket {
	return getMocks(comp).incoming
}

// aliasMock retrieves the mock alias bucket for a test component.
func aliasMock(comp *Component) *mockKVBucket {
	return getMocks(comp).alias
}

// predicateMock retrieves the mock predicate bucket for a test component.
func predicateMock(comp *Component) *mockKVBucket {
	return getMocks(comp).predicate
}

// predicateCatalogMock retrieves the mock predicate catalog bucket for a test component.
func predicateCatalogMock(comp *Component) *mockKVBucket {
	return getMocks(comp).predicateCatalog
}

package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// mockLoopsKV is an in-memory LoopsKVReader for unit tests. Keys use the
// same COMPLETE_{loopID} shape the agentic-loop component writes to in
// production so seeding matches the real contract.
type mockLoopsKV struct {
	data map[string][]byte
}

func newMockLoopsKV() *mockLoopsKV {
	return &mockLoopsKV{data: make(map[string][]byte)}
}

func (m *mockLoopsKV) Put(key string, value []byte) {
	m.data[key] = value
}

func (m *mockLoopsKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	value, ok := m.data[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: value, Revision: 1}, nil
}

// seedCompletion puts a LoopCompletedEvent into the mock KV under the
// COMPLETE_{loopID} key the agentic-loop component writes to in production.
func seedCompletion(t *testing.T, kv *mockLoopsKV, loopID string, event agentic.LoopCompletedEvent) {
	t.Helper()
	data, err := json.Marshal(&event)
	if err != nil {
		t.Fatalf("marshal completion event: %v", err)
	}
	kv.Put(completeKeyPrefix+loopID, data)
}

func TestReadLoopResultExecutor_ListTools(t *testing.T) {
	e := NewReadLoopResultExecutor(newMockLoopsKV(), nil)
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != ReadLoopResultToolName {
		t.Errorf("expected tool name %q, got %q", ReadLoopResultToolName, tools[0].Name)
	}
	if tools[0].Description == "" {
		t.Errorf("description must be non-empty")
	}
	if tools[0].Parameters == nil {
		t.Errorf("parameters schema must be non-nil")
	}
}

// TestReadLoopResultExecutor_HappyPath verifies a small completion event is
// returned whole with its metadata populated.
func TestReadLoopResultExecutor_HappyPath(t *testing.T) {
	kv := newMockLoopsKV()
	now := time.Now().UTC().Truncate(time.Second)
	seedCompletion(t, kv, "loop-42", agentic.LoopCompletedEvent{
		LoopID:      "loop-42",
		TaskID:      "task-42",
		Role:        "researcher",
		Outcome:     "success",
		Result:      "hello world",
		CompletedAt: now,
	})

	e := NewReadLoopResultExecutor(kv, nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": "loop-42",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("unexpected tool error: %s (kind=%s)", res.Error, res.ErrorKind)
	}
	if res.Content != "hello world" {
		t.Errorf("expected full content returned, got %q", res.Content)
	}
	if got, want := res.Metadata["total_bytes"], len("hello world"); got != want {
		t.Errorf("total_bytes = %v, want %d", got, want)
	}
	if got := res.Metadata["has_more"]; got != false {
		t.Errorf("has_more = %v, want false", got)
	}
	if got := res.Metadata["role"]; got != "researcher" {
		t.Errorf("role metadata = %v, want researcher", got)
	}
}

// TestReadLoopResultExecutor_Paging verifies that a long result can be
// retrieved in slices via offset + max_bytes, with has_more + next_offset
// driving the caller's pagination.
func TestReadLoopResultExecutor_Paging(t *testing.T) {
	kv := newMockLoopsKV()
	body := strings.Repeat("x", 10_000)
	seedCompletion(t, kv, "loop-big", agentic.LoopCompletedEvent{
		LoopID:  "loop-big",
		TaskID:  "task-big",
		Role:    "researcher",
		Outcome: "success",
		Result:  body,
	})

	e := NewReadLoopResultExecutor(kv, nil)

	// First page: default max_bytes (4KB) from offset 0.
	first, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": "loop-big",
		},
	})
	if err != nil {
		t.Fatalf("first page error: %v", err)
	}
	if len(first.Content) != defaultReadLoopResultChunk {
		t.Errorf("first page len = %d, want %d", len(first.Content), defaultReadLoopResultChunk)
	}
	if got := first.Metadata["has_more"]; got != true {
		t.Errorf("first page has_more = %v, want true", got)
	}
	nextOffset, ok := first.Metadata["next_offset"].(int)
	if !ok {
		t.Fatalf("next_offset missing or wrong type: %T", first.Metadata["next_offset"])
	}

	// Explicit max_bytes on the second call.
	second, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c2",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id":   "loop-big",
			"offset":    float64(nextOffset), // simulate JSON decode
			"max_bytes": float64(2000),
		},
	})
	if err != nil {
		t.Fatalf("second page error: %v", err)
	}
	if len(second.Content) != 2000 {
		t.Errorf("second page len = %d, want 2000", len(second.Content))
	}

	// Final call beyond the end: empty slice, has_more=false.
	tail, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c3",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id":   "loop-big",
			"offset":    float64(len(body)),
			"max_bytes": float64(1000),
		},
	})
	if err != nil {
		t.Fatalf("tail page error: %v", err)
	}
	if len(tail.Content) != 0 {
		t.Errorf("tail page should be empty, got %d bytes", len(tail.Content))
	}
	if got := tail.Metadata["has_more"]; got != false {
		t.Errorf("tail has_more = %v, want false", got)
	}
}

// TestReadLoopResultExecutor_NotFound verifies a missing completion record
// returns a structured not-found error that the agent can react to.
func TestReadLoopResultExecutor_NotFound(t *testing.T) {
	e := NewReadLoopResultExecutor(newMockLoopsKV(), nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": "never-existed",
		},
	})
	if err != nil {
		t.Fatalf("unexpected wrapped error: %v", err)
	}
	if res.Error == "" {
		t.Errorf("expected a tool-level error message")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("error kind = %v, want ToolErrorNotFound", res.ErrorKind)
	}
}

// TestReadLoopResultExecutor_InvalidArgs verifies missing or wrong-typed
// loop_id surfaces an invalid-args error without trying the KV.
func TestReadLoopResultExecutor_InvalidArgs(t *testing.T) {
	e := NewReadLoopResultExecutor(newMockLoopsKV(), nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing", map[string]any{}},
		{"empty_string", map[string]any{"loop_id": ""}},
		{"wrong_type", map[string]any{"loop_id": 42}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.Execute(context.Background(), agentic.ToolCall{
				ID:        "c1",
				Name:      ReadLoopResultToolName,
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("unexpected wrapped error: %v", err)
			}
			if res.ErrorKind != agentic.ToolErrorInvalidArgs {
				t.Errorf("error kind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
			}
		})
	}
}

// TestReadLoopResultExecutor_MalformedRecord verifies a KV entry that isn't
// a LoopCompletedEvent surfaces ToolErrorInternal rather than silently
// returning garbage.
func TestReadLoopResultExecutor_MalformedRecord(t *testing.T) {
	kv := newMockLoopsKV()
	kv.Put(completeKeyPrefix+"loop-bad", []byte("not json"))

	e := NewReadLoopResultExecutor(kv, nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": "loop-bad",
		},
	})
	if err == nil {
		t.Errorf("expected wrapped error for malformed record")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("error kind = %v, want ToolErrorInternal", res.ErrorKind)
	}
}

// TestReadLoopResultExecutor_NATSTransportError verifies that a NATS transport
// failure (connection dropped, server unavailable) is classified as
// ToolErrorNetwork — not ToolErrorExternal — so the default retry policy
// applies and the agent can recover on the next attempt.
func TestReadLoopResultExecutor_NATSTransportError(t *testing.T) {
	kv := &errKV{err: errors.New("nats: connection closed")}

	e := NewReadLoopResultExecutor(kv, nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": "loop-42",
		},
	})
	if err == nil {
		t.Errorf("expected a wrapped transient error from NATS failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want ToolErrorNetwork (NATS is a transport error)", res.ErrorKind)
	}
}

// errKV is a LoopsKVReader that always returns a transport-level error,
// simulating a dropped NATS connection.
type errKV struct{ err error }

func (e *errKV) Get(_ context.Context, _ string) (*natsclient.KVEntry, error) {
	return nil, e.err
}

// TestReadLoopResultExecutor_UnknownTool verifies the executor rejects calls
// for tools other than read_loop_result.
func TestReadLoopResultExecutor_UnknownTool(t *testing.T) {
	e := NewReadLoopResultExecutor(newMockLoopsKV(), nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "not_the_right_tool",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Errorf("expected wrapped error for unknown tool")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("error kind = %v, want ToolErrorNotFound", res.ErrorKind)
	}
}

// TestNormalizeLoopID covers the three shapes a loop_id argument can
// arrive in: the bare-UUID form (the canonical contract), the full
// 6-part federated form a $entity.id substitution produces, and a
// no-dot fallback. Documented as the migration path for rule prompts
// that haven't moved to $entity.instance yet.
func TestNormalizeLoopID(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"bare uuid", "c1e90237-1cd5-4def-99ab-aabbccddeeff", "c1e90237-1cd5-4def-99ab-aabbccddeeff"},
		{"full entity id", "c360.osh-demo-001.agent.agentic-loop.execution.c1e90237-1cd5", "c1e90237-1cd5"},
		{"three parts", "agentic-loop.execution.uuid-here", "uuid-here"},
		{"empty stays empty", "", ""},
		{"single char", "x", "x"},
		// Trailing dot pins the contract: strip-after-last-dot returns
		// empty. Not LLM-reachable in practice, but locks the behaviour
		// against a future "trim trailing dots first" tweak that would
		// quietly change semantics.
		{"trailing dot", "foo.", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLoopID(tt.in); got != tt.want {
				t.Errorf("normalizeLoopID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestReadLoopResultExecutor_FullEntityIDLoopArg proves the end-to-end
// fix: a tool call whose loop_id argument is the full 6-part federated
// entity ID (which is what the LLM produces when handed $entity.id)
// resolves to the bare-uuid bucket key without the caller doing any
// extra work. semspec hit this wedge in production against beta.34;
// this regression test guards against re-introduction.
func TestReadLoopResultExecutor_FullEntityIDLoopArg(t *testing.T) {
	kv := newMockLoopsKV()
	bareLoopID := "c1e90237-1cd5-4def-99ab-aabbccddeeff"
	fullEntityID := "c360.osh-demo-001.agent.agentic-loop.execution." + bareLoopID
	seedCompletion(t, kv, bareLoopID, agentic.LoopCompletedEvent{
		LoopID:  bareLoopID,
		Role:    "researcher",
		Outcome: "success",
		Result:  "research findings",
	})

	e := NewReadLoopResultExecutor(kv, nil)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: ReadLoopResultToolName,
		Arguments: map[string]any{
			"loop_id": fullEntityID,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("expected lookup to succeed via normalize, got error: %s (kind=%s)", res.Error, res.ErrorKind)
	}
	if res.Content != "research findings" {
		t.Errorf("content = %q, want %q", res.Content, "research findings")
	}
	if got := res.Metadata["loop_id"]; got != bareLoopID {
		t.Errorf("metadata.loop_id = %v, want bare uuid %q (normalize must apply before downstream uses)", got, bareLoopID)
	}
}

// --- Offloaded-result hydration (payload-size-chokepoints D4) ---

// fakeContentFetcher is an in-memory LoopContentFetcher.
type fakeContentFetcher struct {
	byKey   map[string]*objectstore.StoredContent
	failErr error
}

func (f *fakeContentFetcher) FetchContent(_ context.Context, ref *message.StorageReference) (*objectstore.StoredContent, error) {
	if f.failErr != nil {
		return nil, f.failErr
	}
	sc, ok := f.byKey[ref.Key]
	if !ok {
		return nil, errs.WrapInvalid(errors.New("no such content"), "fake", "FetchContent", "lookup")
	}
	return sc, nil
}

// seedOffloadedCompletion writes a ref-bearing completion value plus its
// content-store body, mirroring exactly what the agentic-loop component
// persists after an offload.
func seedOffloadedCompletion(t *testing.T, kv *mockLoopsKV, fetcher *fakeContentFetcher, loopID, body string) {
	t.Helper()
	key := "content_c360.ops.agent.agentic-loop.result." + loopID
	seedCompletion(t, kv, loopID, agentic.LoopCompletedEvent{
		LoopID:        loopID,
		Role:          "researcher",
		Outcome:       "success",
		Result:        "", // offloaded
		ResultRef:     &message.StorageReference{StorageInstance: "objectstore", Key: key},
		ResultPreview: body[:10],
		ResultSize:    len(body),
	})
	if fetcher.byKey == nil {
		fetcher.byKey = map[string]*objectstore.StoredContent{}
	}
	fetcher.byKey[key] = &objectstore.StoredContent{
		EntityID:      "c360.ops.agent.agentic-loop.result." + loopID,
		Fields:        map[string]string{"result": body},
		ContentFields: map[string]string{message.ContentRoleBody: "result"},
	}
}

// TestReadLoopResult_OffloadedResult_PagesOverHydratedContent proves the
// paging contract is unchanged for ref-bearing values: max_bytes/offset walk
// the HYDRATED body, total_bytes reports the full size, and the pages
// reassemble to the exact original content.
func TestReadLoopResult_OffloadedResult_PagesOverHydratedContent(t *testing.T) {
	kv := newMockLoopsKV()
	fetcher := &fakeContentFetcher{}
	body := strings.Repeat("abcdefghij", 100) // 1000 bytes
	seedOffloadedCompletion(t, kv, fetcher, "loop-off-1", body)

	e := NewReadLoopResultExecutor(kv, fetcher)

	var rebuilt strings.Builder
	offset := 0
	pages := 0
	for {
		res, err := e.Execute(context.Background(), agentic.ToolCall{
			ID:   "c1",
			Name: ReadLoopResultToolName,
			Arguments: map[string]any{
				"loop_id":   "loop-off-1",
				"max_bytes": 256,
				"offset":    offset,
			},
		})
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		if res.Error != "" {
			t.Fatalf("page at offset %d returned tool error: %s", offset, res.Error)
		}
		if res.Metadata["result_offloaded"] != true {
			t.Fatal("metadata must mark the result as offloaded")
		}
		if got := res.Metadata[agentic.MetadataKeyTotalBytes]; got != len(body) {
			t.Fatalf("total_bytes must report the HYDRATED size, got %v want %d", got, len(body))
		}
		rebuilt.WriteString(res.Content)
		pages++
		hasMore, _ := res.Metadata[agentic.MetadataKeyHasMore].(bool)
		if !hasMore {
			break
		}
		next, ok := res.Metadata[agentic.MetadataKeyNextOffset].(int)
		if !ok {
			t.Fatalf("next_offset missing or wrong type: %v", res.Metadata[agentic.MetadataKeyNextOffset])
		}
		offset = next
	}
	if pages < 2 {
		t.Fatalf("expected multiple pages for 1000 bytes at 256/page, got %d", pages)
	}
	if rebuilt.String() != body {
		t.Fatal("paged reads must reassemble to the exact original content")
	}
}

// TestReadLoopResult_OffloadedResult_NoFetcherIsTypedError: without a
// content store the tool must fail loud with a typed error — never serve the
// preview as if it were the full result, never return empty content.
func TestReadLoopResult_OffloadedResult_NoFetcherIsTypedError(t *testing.T) {
	kv := newMockLoopsKV()
	fetcher := &fakeContentFetcher{}
	seedOffloadedCompletion(t, kv, fetcher, "loop-off-2", strings.Repeat("x", 100))

	e := NewReadLoopResultExecutor(kv, nil) // no content store wired
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": "loop-off-2"},
	})
	if err == nil {
		t.Fatal("expected classified error when hydration is impossible")
	}
	if res.Error == "" || res.ErrorKind != agentic.ToolErrorInternal {
		t.Fatalf("expected internal tool error naming the gap, got kind=%s err=%q", res.ErrorKind, res.Error)
	}
	if res.Content != "" {
		t.Fatal("no content may be served when the body cannot be hydrated (preview is not the result)")
	}
}

// TestReadLoopResult_OffloadedResult_TransientFetchFailure maps a transient
// store failure onto the network error kind so the agent retries rather than
// concluding the result is gone.
func TestReadLoopResult_OffloadedResult_TransientFetchFailure(t *testing.T) {
	kv := newMockLoopsKV()
	fetcher := &fakeContentFetcher{failErr: errs.WrapTransient(errors.New("objectstore timeout"), "fake", "FetchContent", "fetch")}
	seedOffloadedCompletion(t, kv, fetcher, "loop-off-3", strings.Repeat("x", 100))
	// Re-arm failure AFTER seeding (seed writes to the same fetcher).
	fetcher.failErr = errs.WrapTransient(errors.New("objectstore timeout"), "fake", "FetchContent", "fetch")

	e := NewReadLoopResultExecutor(kv, fetcher)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": "loop-off-3"},
	})
	if err == nil {
		t.Fatal("expected classified error on fetch failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Fatalf("transient fetch failure must map to network kind, got %s", res.ErrorKind)
	}
}

// TestReadLoopResult_OffloadedResult_MissingBodyRoleFailsClosed: a stored
// envelope without the body role is a broken contract — typed error, not an
// empty-string result.
func TestReadLoopResult_OffloadedResult_MissingBodyRoleFailsClosed(t *testing.T) {
	kv := newMockLoopsKV()
	fetcher := &fakeContentFetcher{}
	seedOffloadedCompletion(t, kv, fetcher, "loop-off-4", strings.Repeat("x", 100))
	// Corrupt the stored envelope: drop the body role mapping.
	for _, sc := range fetcher.byKey {
		sc.ContentFields = map[string]string{}
	}

	e := NewReadLoopResultExecutor(kv, fetcher)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": "loop-off-4"},
	})
	if err == nil {
		t.Fatal("expected classified error for missing body role")
	}
	if res.ErrorKind != agentic.ToolErrorInternal || res.Content != "" {
		t.Fatalf("missing role must fail closed with internal kind, got kind=%s content=%q", res.ErrorKind, res.Content)
	}
}

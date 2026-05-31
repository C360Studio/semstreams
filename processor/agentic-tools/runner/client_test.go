package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateWorktree_WireFormat_ReadOnlyPaths pins the JSON envelope
// CreateWorktree sends when callers attach read_only_paths metadata.
// Sister projects (semspec) read this contract on the wire — a silent
// rename would break their server-side enforcement.
func TestCreateWorktree_WireFormat_ReadOnlyPaths(t *testing.T) {
	var captured struct {
		TaskID        string   `json:"task_id"`
		ReadOnlyPaths []string `json:"read_only_paths,omitempty"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/worktree" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"status":"ok","path":"/wt","branch":"main"}`))
	}))
	defer server.Close()

	c := NewClient(server.URL)
	_, err := c.CreateWorktree(context.Background(), "task-1", CreateWorktreeOptions{
		ReadOnlyPaths: []string{"src/", "configs/foo.json"},
	})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if captured.TaskID != "task-1" {
		t.Errorf("task_id = %q, want %q", captured.TaskID, "task-1")
	}
	wantPaths := []string{"src/", "configs/foo.json"}
	if len(captured.ReadOnlyPaths) != len(wantPaths) {
		t.Fatalf("read_only_paths len = %d, want %d (%v)", len(captured.ReadOnlyPaths), len(wantPaths), captured.ReadOnlyPaths)
	}
	for i, p := range wantPaths {
		if captured.ReadOnlyPaths[i] != p {
			t.Errorf("[%d] = %q, want %q", i, captured.ReadOnlyPaths[i], p)
		}
	}
}

// Zero-value opts must omit read_only_paths from the wire entirely so
// servers that don't recognize the field don't see an unexpected key.
func TestCreateWorktree_WireFormat_ZeroOptsOmitsField(t *testing.T) {
	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &raw)
		_, _ = w.Write([]byte(`{"status":"ok","path":"/wt","branch":"main"}`))
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if _, err := c.CreateWorktree(context.Background(), "task-1", CreateWorktreeOptions{}); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if _, present := raw["read_only_paths"]; present {
		t.Errorf("zero opts must omit read_only_paths from wire envelope; got: %v", raw)
	}
}

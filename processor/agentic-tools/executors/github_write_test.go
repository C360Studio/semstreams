package executors

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// --- ListTools ---

func TestGitHubWriteExecutor_ListTools(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	tools := e.ListTools()

	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}

	expected := map[string]bool{
		"github_create_branch": true,
		"github_commit_file":   true,
		"github_create_pr":     true,
		"github_add_comment":   true,
		"github_add_label":     true,
	}
	for _, tool := range tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		delete(expected, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("tool %s has nil parameters", tool.Name)
		}
	}
	for name := range expected {
		t.Errorf("missing expected tool: %s", name)
	}
}

// --- UnknownTool ---

func TestGitHubWriteExecutor_UnknownTool(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "w0",
		Name:      "github_does_not_exist",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Error("expected Go error for unknown tool")
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for unknown tool")
	}
	if result.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, agentic.ToolErrorNotFound)
	}
}

func TestGitHubWriteExecutor_InvalidArgs_TaggedAsInvalidArgs(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w-bad",
		Name: "github_create_branch",
		// missing owner/repo/branch/base_sha
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("did not expect Go error for validation failure: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected ToolResult.Error for missing args")
	}
	if result.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, agentic.ToolErrorInvalidArgs)
	}
}

// --- github_create_branch ---

func TestGitHubWriteExecutor_CreateBranch_Success(t *testing.T) {
	mock := &mockGitHubClient{
		createBranchFunc: func(_ context.Context, owner, repo, branch, baseSHA string) error {
			if owner != "acme" || repo != "ops" {
				t.Errorf("unexpected repo: %s/%s", owner, repo)
			}
			if branch != "feat/issue-42" {
				t.Errorf("unexpected branch: %s", branch)
			}
			if baseSHA != "deadbeef" {
				t.Errorf("unexpected SHA: %s", baseSHA)
			}
			return nil
		},
	}

	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w1",
		Name: "github_create_branch",
		Arguments: map[string]any{
			"owner":    "acme",
			"repo":     "ops",
			"branch":   "feat/issue-42",
			"base_sha": "deadbeef",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected ToolResult.Error: %s", result.Error)
	}
	if result.CallID != "w1" {
		t.Errorf("expected CallID w1, got %s", result.CallID)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if parsed["branch"] != "feat/issue-42" {
		t.Errorf("unexpected branch in content: %v", parsed["branch"])
	}
	if parsed["status"] != "created" {
		t.Errorf("expected status 'created', got %v", parsed["status"])
	}
}

func TestGitHubWriteExecutor_CreateBranch_MissingBranch(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w2",
		Name: "github_create_branch",
		Arguments: map[string]any{
			"owner":    "acme",
			"repo":     "ops",
			"base_sha": "abc",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing branch")
	}
}

func TestGitHubWriteExecutor_CreateBranch_MissingBaseSHA(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w3",
		Name: "github_create_branch",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"branch": "feat/x",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing base_sha")
	}
}

func TestGitHubWriteExecutor_CreateBranch_ClientError(t *testing.T) {
	mock := &mockGitHubClient{
		createBranchFunc: func(_ context.Context, _, _, _, _ string) error {
			return errors.New("branch already exists")
		},
	}
	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w4",
		Name: "github_create_branch",
		Arguments: map[string]any{
			"owner":    "acme",
			"repo":     "ops",
			"branch":   "main",
			"base_sha": "abc",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for client failure")
	}
}

// --- github_commit_file ---

func TestGitHubWriteExecutor_CommitFile_Success(t *testing.T) {
	mock := &mockGitHubClient{
		commitFileFunc: func(_ context.Context, owner, repo, branch, path, content, message string) error {
			if owner != "acme" || repo != "ops" {
				t.Errorf("unexpected repo: %s/%s", owner, repo)
			}
			if branch != "feat/issue-42" {
				t.Errorf("unexpected branch: %s", branch)
			}
			if path != "src/solution.go" {
				t.Errorf("unexpected path: %s", path)
			}
			if content != "package main" {
				t.Errorf("unexpected content: %s", content)
			}
			if message != "feat: solve issue 42" {
				t.Errorf("unexpected message: %s", message)
			}
			return nil
		},
	}

	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w5",
		Name: "github_commit_file",
		Arguments: map[string]any{
			"owner":   "acme",
			"repo":    "ops",
			"branch":  "feat/issue-42",
			"path":    "src/solution.go",
			"content": "package main",
			"message": "feat: solve issue 42",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected ToolResult.Error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if parsed["status"] != "committed" {
		t.Errorf("expected status 'committed', got %v", parsed["status"])
	}
}

func TestGitHubWriteExecutor_CommitFile_MissingPath(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w6",
		Name: "github_commit_file",
		Arguments: map[string]any{
			"owner":   "acme",
			"repo":    "ops",
			"branch":  "feat/x",
			"content": "data",
			"message": "msg",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing path")
	}
}

func TestGitHubWriteExecutor_CommitFile_MissingMessage(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w7",
		Name: "github_commit_file",
		Arguments: map[string]any{
			"owner":   "acme",
			"repo":    "ops",
			"branch":  "feat/x",
			"path":    "file.go",
			"content": "data",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing message")
	}
}

func TestGitHubWriteExecutor_CommitFile_ClientError(t *testing.T) {
	mock := &mockGitHubClient{
		commitFileFunc: func(_ context.Context, _, _, _, _, _, _ string) error {
			return errors.New("push rejected")
		},
	}
	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w8",
		Name: "github_commit_file",
		Arguments: map[string]any{
			"owner":   "acme",
			"repo":    "ops",
			"branch":  "feat/x",
			"path":    "file.go",
			"content": "data",
			"message": "msg",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for client failure")
	}
}

// --- github_create_pr ---

func TestGitHubWriteExecutor_CreatePR_Success(t *testing.T) {
	pr := samplePR()
	mock := &mockGitHubClient{
		createPRFunc: func(_ context.Context, _, _, title, _, head, base string) (*GitHubPR, error) {
			if title != "feat: implement X" {
				t.Errorf("unexpected title: %s", title)
			}
			if head != "feat/branch" || base != "main" {
				t.Errorf("unexpected head/base: %s/%s", head, base)
			}
			return pr, nil
		},
	}

	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w9",
		Name: "github_create_pr",
		Arguments: map[string]any{
			"owner": "acme",
			"repo":  "ops",
			"title": "feat: implement X",
			"body":  "Closes #42",
			"head":  "feat/branch",
			"base":  "main",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected ToolResult.Error: %s", result.Error)
	}

	var parsed GitHubPR
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if parsed.Number != 10 {
		t.Errorf("expected number 10, got %d", parsed.Number)
	}
}

func TestGitHubWriteExecutor_CreatePR_MissingTitle(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w10",
		Name: "github_create_pr",
		Arguments: map[string]any{
			"owner": "acme",
			"repo":  "ops",
			"body":  "body",
			"head":  "feat/branch",
			"base":  "main",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing title")
	}
}

func TestGitHubWriteExecutor_CreatePR_MissingHead(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w11",
		Name: "github_create_pr",
		Arguments: map[string]any{
			"owner": "acme",
			"repo":  "ops",
			"title": "title",
			"body":  "body",
			"base":  "main",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing head")
	}
}

func TestGitHubWriteExecutor_CreatePR_ClientError(t *testing.T) {
	mock := &mockGitHubClient{
		createPRFunc: func(_ context.Context, _, _, _, _, _, _ string) (*GitHubPR, error) {
			return nil, errors.New("PR already exists")
		},
	}
	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w12",
		Name: "github_create_pr",
		Arguments: map[string]any{
			"owner": "acme",
			"repo":  "ops",
			"title": "title",
			"body":  "body",
			"head":  "feat/branch",
			"base":  "main",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for client failure")
	}
}

// --- github_add_comment ---

func TestGitHubWriteExecutor_AddComment_Success(t *testing.T) {
	mock := &mockGitHubClient{
		addCommentFunc: func(_ context.Context, owner, repo string, number int, body string) error {
			if owner != "acme" || repo != "ops" {
				t.Errorf("unexpected repo: %s/%s", owner, repo)
			}
			if number != 42 {
				t.Errorf("expected number 42, got %d", number)
			}
			if body != "Work in progress" {
				t.Errorf("unexpected body: %s", body)
			}
			return nil
		},
	}

	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w13",
		Name: "github_add_comment",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(42),
			"body":   "Work in progress",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected ToolResult.Error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if parsed["status"] != "commented" {
		t.Errorf("expected status 'commented', got %v", parsed["status"])
	}
}

func TestGitHubWriteExecutor_AddComment_MissingNumber(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w14",
		Name: "github_add_comment",
		Arguments: map[string]any{
			"owner": "acme",
			"repo":  "ops",
			"body":  "comment",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing number")
	}
}

func TestGitHubWriteExecutor_AddComment_MissingBody(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w15",
		Name: "github_add_comment",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(1),
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing body")
	}
}

func TestGitHubWriteExecutor_AddComment_ClientError(t *testing.T) {
	mock := &mockGitHubClient{
		addCommentFunc: func(_ context.Context, _, _ string, _ int, _ string) error {
			return errors.New("forbidden")
		},
	}
	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w16",
		Name: "github_add_comment",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(1),
			"body":   "comment",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for client failure")
	}
}

// --- github_add_label ---

func TestGitHubWriteExecutor_AddLabel_Success(t *testing.T) {
	mock := &mockGitHubClient{
		addLabelsFunc: func(_ context.Context, owner, repo string, number int, labels []string) error {
			if owner != "acme" || repo != "ops" {
				t.Errorf("unexpected repo: %s/%s", owner, repo)
			}
			if number != 7 {
				t.Errorf("expected number 7, got %d", number)
			}
			if len(labels) != 2 || labels[0] != "bug" || labels[1] != "triage" {
				t.Errorf("unexpected labels: %v", labels)
			}
			return nil
		},
	}

	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w17",
		Name: "github_add_label",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(7),
			"labels": []interface{}{"bug", "triage"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected ToolResult.Error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if parsed["status"] != "labeled" {
		t.Errorf("expected status 'labeled', got %v", parsed["status"])
	}
}

func TestGitHubWriteExecutor_AddLabel_MissingLabels(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w18",
		Name: "github_add_label",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(1),
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing labels")
	}
}

func TestGitHubWriteExecutor_AddLabel_EmptyLabels(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w19",
		Name: "github_add_label",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(1),
			"labels": []interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for empty labels")
	}
}

func TestGitHubWriteExecutor_AddLabel_MissingNumber(t *testing.T) {
	e := NewGitHubWriteExecutor(&mockGitHubClient{})
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w20",
		Name: "github_add_label",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"labels": []interface{}{"bug"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for missing number")
	}
}

func TestGitHubWriteExecutor_AddLabel_ClientError(t *testing.T) {
	mock := &mockGitHubClient{
		addLabelsFunc: func(_ context.Context, _, _ string, _ int, _ []string) error {
			return errors.New("label not found")
		},
	}
	e := NewGitHubWriteExecutor(mock)
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "w21",
		Name: "github_add_label",
		Arguments: map[string]any{
			"owner":  "acme",
			"repo":   "ops",
			"number": float64(1),
			"labels": []interface{}{"nonexistent"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected ToolResult.Error for client failure")
	}
}

package executors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// mockGitHubClient implements GitHubClient for use in executor tests.
// Each method has a corresponding stub field so tests can inject both
// successful responses and errors without standing up an HTTP server.
type mockGitHubClient struct {
	getIssueFunc     func(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error)
	listIssuesFunc   func(ctx context.Context, owner, repo string, opts ListIssuesOpts) ([]GitHubIssue, error)
	searchIssuesFunc func(ctx context.Context, query string) ([]GitHubIssue, error)
	getPRFunc        func(ctx context.Context, owner, repo string, number int) (*GitHubPR, error)
	getFileFunc      func(ctx context.Context, owner, repo, path, ref string) (string, error)
	createBranchFunc func(ctx context.Context, owner, repo, branch, baseSHA string) error
	commitFileFunc   func(ctx context.Context, owner, repo, branch, path, content, message string) error
	createPRFunc     func(ctx context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error)
	addCommentFunc   func(ctx context.Context, owner, repo string, number int, body string) error
	addLabelsFunc    func(ctx context.Context, owner, repo string, number int, labels []string) error
}

func (m *mockGitHubClient) GetIssue(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error) {
	return m.getIssueFunc(ctx, owner, repo, number)
}
func (m *mockGitHubClient) ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOpts) ([]GitHubIssue, error) {
	return m.listIssuesFunc(ctx, owner, repo, opts)
}
func (m *mockGitHubClient) SearchIssues(ctx context.Context, query string) ([]GitHubIssue, error) {
	return m.searchIssuesFunc(ctx, query)
}
func (m *mockGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*GitHubPR, error) {
	return m.getPRFunc(ctx, owner, repo, number)
}
func (m *mockGitHubClient) GetFileContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	return m.getFileFunc(ctx, owner, repo, path, ref)
}
func (m *mockGitHubClient) CreateBranch(ctx context.Context, owner, repo, branch, baseSHA string) error {
	return m.createBranchFunc(ctx, owner, repo, branch, baseSHA)
}
func (m *mockGitHubClient) CommitFile(ctx context.Context, owner, repo, branch, path, content, message string) error {
	return m.commitFileFunc(ctx, owner, repo, branch, path, content, message)
}
func (m *mockGitHubClient) CreatePullRequest(ctx context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error) {
	return m.createPRFunc(ctx, owner, repo, title, body, head, base)
}
func (m *mockGitHubClient) AddComment(ctx context.Context, owner, repo string, number int, body string) error {
	return m.addCommentFunc(ctx, owner, repo, number, body)
}
func (m *mockGitHubClient) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	return m.addLabelsFunc(ctx, owner, repo, number, labels)
}

// sampleIssue returns a GitHubIssue suitable for assertion in tests.
func sampleIssue() *GitHubIssue {
	return &GitHubIssue{
		Number:    42,
		Title:     "Fix the thing",
		Body:      "Description of the bug",
		State:     "open",
		Labels:    []string{"bug", "priority:high"},
		Author:    "alice",
		HTMLURL:   "https://github.com/owner/repo/issues/42",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Comments:  3,
	}
}

// samplePR returns a GitHubPR suitable for assertion in tests.
func samplePR() *GitHubPR {
	mergeable := true
	return &GitHubPR{
		Number:    10,
		Title:     "feat: add feature",
		Body:      "Implements the feature",
		State:     "open",
		HTMLURL:   "https://github.com/owner/repo/pull/10",
		Head:      "feat/feature-branch",
		Base:      "main",
		Mergeable: &mergeable,
		Additions: 50,
		Deletions: 5,
		DiffURL:   "https://github.com/owner/repo/pull/10.diff",
		CreatedAt: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC),
	}
}

// githubIssueWireResponse builds the JSON structure the GitHub REST API returns for an issue.
// Only the fields the client cares about are populated; the rest match real API behaviour.
func githubIssueWireResponse(number int, title, state string, labels []string) map[string]any {
	rawLabels := make([]map[string]string, 0, len(labels))
	for _, l := range labels {
		rawLabels = append(rawLabels, map[string]string{"name": l})
	}
	return map[string]any{
		"number":     number,
		"title":      title,
		"body":       "issue body",
		"state":      state,
		"html_url":   "https://github.com/owner/repo/issues/1",
		"comments":   0,
		"created_at": "2024-01-01T00:00:00Z",
		"updated_at": "2024-01-01T00:00:00Z",
		"user":       map[string]string{"login": "alice"},
		"assignee":   nil,
		"labels":     rawLabels,
	}
}

// --- GitHubHTTPClient HTTP layer tests ---

func TestGitHubHTTPClient_GetIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/issues/1") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing accept header: %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(githubIssueWireResponse(1, "Test Issue", "open", []string{"bug"})); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("test-token", server.URL)
	issue, err := client.GetIssue(context.Background(), "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if issue.Number != 1 {
		t.Errorf("expected number 1, got %d", issue.Number)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %q", issue.Title)
	}
	if issue.State != "open" {
		t.Errorf("expected state 'open', got %q", issue.State)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Errorf("expected labels [bug], got %v", issue.Labels)
	}
	if issue.Author != "alice" {
		t.Errorf("expected author 'alice', got %q", issue.Author)
	}
}

func TestGitHubHTTPClient_GetIssue_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	_, err := client.GetIssue(context.Background(), "owner", "repo", 999)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGitHubHTTPClient_ListIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("expected state=open, got %s", r.URL.Query().Get("state"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		items := []map[string]any{
			githubIssueWireResponse(1, "Issue One", "open", []string{}),
			githubIssueWireResponse(2, "Issue Two", "open", []string{"enhancement"}),
		}
		if err := json.NewEncoder(w).Encode(items); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	issues, err := client.ListIssues(context.Background(), "owner", "repo", ListIssuesOpts{State: "open", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if len(issues[1].Labels) != 1 || issues[1].Labels[0] != "enhancement" {
		t.Errorf("expected label 'enhancement', got %v", issues[1].Labels)
	}
}

func TestGitHubHTTPClient_SearchIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q != "repo:owner/repo is:open" {
			t.Errorf("unexpected query: %q", q)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				githubIssueWireResponse(5, "Search Result", "open", []string{"bug"}),
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	issues, err := client.SearchIssues(context.Background(), "repo:owner/repo is:open")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Number != 5 {
		t.Errorf("expected number 5, got %d", issues[0].Number)
	}
}

func TestGitHubHTTPClient_GetFileContent(t *testing.T) {
	expectedContent := "package main\n\nfunc main() {}\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(expectedContent))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "main" {
			t.Errorf("expected ref=main, got %s", r.URL.Query().Get("ref"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"content":  encoded,
			"encoding": "base64",
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	content, err := client.GetFileContent(context.Background(), "owner", "repo", "main.go", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != expectedContent {
		t.Errorf("expected %q, got %q", expectedContent, content)
	}
}

func TestGitHubHTTPClient_CreateBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["ref"] != "refs/heads/feat/new-feature" {
			t.Errorf("unexpected ref: %s", body["ref"])
		}
		if body["sha"] != "abc123" {
			t.Errorf("unexpected sha: %s", body["sha"])
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	err := client.CreateBranch(context.Background(), "owner", "repo", "feat/new-feature", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGitHubHTTPClient_CommitFile_NewFile(t *testing.T) {
	// Track which HTTP methods have been called to verify the GET → PUT sequence.
	var methods []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)

		switch r.Method {
		case http.MethodGet:
			// First call: check existing file SHA — return 404 (file doesn't exist yet).
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body["branch"] != "feat/branch" {
				t.Errorf("unexpected branch: %v", body["branch"])
			}
			if body["message"] != "add file" {
				t.Errorf("unexpected message: %v", body["message"])
			}
			// Verify the content was base64 encoded.
			encoded, _ := body["content"].(string)
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Errorf("content is not valid base64: %v", err)
			}
			if string(decoded) != "hello world" {
				t.Errorf("unexpected decoded content: %s", decoded)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	err := client.CommitFile(context.Background(), "owner", "repo", "feat/branch", "hello.txt", "hello world", "add file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPut {
		t.Errorf("expected [GET, PUT], got %v", methods)
	}
}

func TestGitHubHTTPClient_CreatePullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["title"] != "My PR" {
			t.Errorf("unexpected title: %s", body["title"])
		}
		if body["head"] != "feat/branch" {
			t.Errorf("unexpected head: %s", body["head"])
		}
		if body["base"] != "main" {
			t.Errorf("unexpected base: %s", body["base"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"number":     7,
			"title":      "My PR",
			"body":       "PR body",
			"state":      "open",
			"html_url":   "https://github.com/owner/repo/pull/7",
			"diff_url":   "https://github.com/owner/repo/pull/7.diff",
			"additions":  10,
			"deletions":  2,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
			"head":       map[string]string{"ref": "feat/branch"},
			"base":       map[string]string{"ref": "main"},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	pr, err := client.CreatePullRequest(context.Background(), "owner", "repo", "My PR", "PR body", "feat/branch", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 7 {
		t.Errorf("expected number 7, got %d", pr.Number)
	}
	if pr.Head != "feat/branch" {
		t.Errorf("expected head 'feat/branch', got %q", pr.Head)
	}
}

func TestGitHubHTTPClient_AddComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/issues/42/comments") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["body"] != "LGTM!" {
			t.Errorf("unexpected comment body: %s", body["body"])
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	err := client.AddComment(context.Background(), "owner", "repo", 42, "LGTM!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGitHubHTTPClient_AddLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/issues/5/labels") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string][]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if len(body["labels"]) != 2 {
			t.Errorf("expected 2 labels, got %v", body["labels"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	err := client.AddLabels(context.Background(), "owner", "repo", 5, []string{"bug", "triage"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGitHubHTTPClient_ContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled (the client gives up).
		select {
		case <-r.Context().Done():
		case <-blocked:
		}
	}))
	defer func() {
		close(blocked)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the request never fires

	client := newGitHubHTTPClientWithBase("token", server.URL)
	_, err := client.GetIssue(ctx, "owner", "repo", 1)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestGitHubHTTPClient_IssueAssigneeAndLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"number":     3,
			"title":      "Assigned issue",
			"body":       "",
			"state":      "open",
			"html_url":   "https://github.com/owner/repo/issues/3",
			"comments":   0,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
			"user":       map[string]string{"login": "bob"},
			"assignee":   map[string]string{"login": "carol"},
			"labels": []map[string]string{
				{"name": "feature"},
				{"name": "P1"},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := newGitHubHTTPClientWithBase("token", server.URL)
	issue, err := client.GetIssue(context.Background(), "owner", "repo", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Assignee != "carol" {
		t.Errorf("expected assignee 'carol', got %q", issue.Assignee)
	}
	if issue.Author != "bob" {
		t.Errorf("expected author 'bob', got %q", issue.Author)
	}
	if len(issue.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %v", issue.Labels)
	}
	if issue.Labels[0] != "feature" || issue.Labels[1] != "P1" {
		t.Errorf("unexpected labels: %v", issue.Labels)
	}
}

// --- classifyHTTPStatus / classifyGitHubError ---

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		code int
		want agentic.ToolErrorKind
	}{
		{http.StatusUnauthorized, agentic.ToolErrorPermission},
		{http.StatusForbidden, agentic.ToolErrorPermission},
		{http.StatusNotFound, agentic.ToolErrorNotFound},
		{http.StatusGone, agentic.ToolErrorNotFound},
		{http.StatusRequestTimeout, agentic.ToolErrorTimeout},
		{http.StatusGatewayTimeout, agentic.ToolErrorTimeout},
		{http.StatusTooManyRequests, agentic.ToolErrorExternal},
		{http.StatusBadRequest, agentic.ToolErrorInvalidArgs},
		{http.StatusConflict, agentic.ToolErrorInvalidArgs},
		{http.StatusUnprocessableEntity, agentic.ToolErrorInvalidArgs},
		{http.StatusInternalServerError, agentic.ToolErrorExternal},
		{http.StatusBadGateway, agentic.ToolErrorExternal},
		{http.StatusServiceUnavailable, agentic.ToolErrorExternal},
		{http.StatusOK, agentic.ToolErrorUnknown}, // Shouldn't happen, but covered.
		{0, agentic.ToolErrorUnknown},
	}
	for _, c := range cases {
		if got := classifyHTTPStatus(c.code); got != c.want {
			t.Errorf("classifyHTTPStatus(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestClassifyGitHubError(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		if got := classifyGitHubError(nil); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("context deadline is timeout", func(t *testing.T) {
		if got := classifyGitHubError(context.DeadlineExceeded); got != agentic.ToolErrorTimeout {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorTimeout)
		}
	})

	t.Run("context canceled is timeout", func(t *testing.T) {
		if got := classifyGitHubError(context.Canceled); got != agentic.ToolErrorTimeout {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorTimeout)
		}
	})

	t.Run("wrapped context deadline is timeout", func(t *testing.T) {
		wrapped := fmt.Errorf("doing the thing: %w", context.DeadlineExceeded)
		if got := classifyGitHubError(wrapped); got != agentic.ToolErrorTimeout {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorTimeout)
		}
	})

	t.Run("httpStatusError 403 is permission", func(t *testing.T) {
		err := &httpStatusError{StatusCode: http.StatusForbidden, Body: "denied"}
		if got := classifyGitHubError(err); got != agentic.ToolErrorPermission {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorPermission)
		}
	})

	t.Run("wrapped httpStatusError 422 is invalid_args", func(t *testing.T) {
		inner := &httpStatusError{StatusCode: http.StatusUnprocessableEntity, Body: "bad"}
		wrapped := fmt.Errorf("github_create_branch: %w", inner)
		if got := classifyGitHubError(wrapped); got != agentic.ToolErrorInvalidArgs {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorInvalidArgs)
		}
	})

	t.Run("url.Error dial failure is network", func(t *testing.T) {
		// *url.Error wraps transport errors from http.Client.Do. It
		// implements net.Error, which the classifier catches.
		err := &url.Error{
			Op:  "Get",
			URL: "https://api.github.com/repos/x/y",
			Err: errors.New("dial tcp: lookup api.github.com: no such host"),
		}
		if got := classifyGitHubError(err); got != agentic.ToolErrorNetwork {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorNetwork)
		}
	})

	t.Run("net.Error with Timeout() is timeout", func(t *testing.T) {
		err := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &timeoutNetErr{},
		}
		if got := classifyGitHubError(err); got != agentic.ToolErrorTimeout {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorTimeout)
		}
	})

	t.Run("unclassified error falls back to external", func(t *testing.T) {
		err := errors.New("something unexpected")
		if got := classifyGitHubError(err); got != agentic.ToolErrorExternal {
			t.Errorf("got %q, want %q", got, agentic.ToolErrorExternal)
		}
	})
}

// timeoutNetErr is a minimal net.Error whose Timeout() returns true, used
// to exercise the timeout branch of classifyGitHubError without depending
// on actual network I/O.
type timeoutNetErr struct{}

func (*timeoutNetErr) Error() string   { return "i/o timeout" }
func (*timeoutNetErr) Timeout() bool   { return true }
func (*timeoutNetErr) Temporary() bool { return true }

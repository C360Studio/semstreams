// Package sandbox provides an HTTP client for the sandbox server.
// The sandbox server runs file, git, and command operations inside an isolated
// container so that agent-generated code never touches the host process.
//
// Ported from semspec/tools/sandbox with task_id mapped to agent loop_id.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxResponseBytes caps sandbox response bodies at 2 MB to prevent
	// runaway output from filling agent memory.
	maxResponseBytes = 2 * 1024 * 1024
)

// Client communicates with the sandbox server via HTTP.
// All operations are scoped to a task ID that maps to a git worktree on the
// server side. In semteams this is typically the agent loop_id.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a sandbox client pointing at baseURL.
// Returns nil if baseURL is empty, which callers treat as "sandbox disabled".
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			// Slightly above the maximum command timeout (5 min) so the HTTP
			// layer doesn't race with the server-side exec deadline.
			Timeout: 6 * time.Minute,
		},
	}
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// ExecResult holds the outcome of a command executed inside the sandbox.
type ExecResult struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exit_code"`
	TimedOut       bool   `json:"timed_out"`
	Classification string `json:"classification,omitempty"`
}

// FileEntry represents a single filesystem entry.
type FileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// WorktreeInfo describes a newly created isolated workspace.
type WorktreeInfo struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// CommitResult holds the outcome of a git commit inside the sandbox.
type CommitResult struct {
	Status       string `json:"status"`
	Hash         string `json:"hash,omitempty"`
	FilesChanged int    `json:"files_changed,omitempty"`
}

// SearchMatch represents a single search result.
type SearchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Command execution
// ---------------------------------------------------------------------------

// Exec runs command inside the taskID worktree with a server-side timeout of
// timeoutMs milliseconds.
func (c *Client) Exec(ctx context.Context, taskID, command string, timeoutMs int) (*ExecResult, error) {
	body := struct {
		TaskID    string `json:"task_id"`
		Command   string `json:"command"`
		TimeoutMs int    `json:"timeout_ms"`
	}{TaskID: taskID, Command: command, TimeoutMs: timeoutMs}
	var result ExecResult
	if err := c.doJSON(ctx, http.MethodPost, "/exec", body, &result); err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Worktree lifecycle
// ---------------------------------------------------------------------------

// CreateWorktree asks the sandbox server to create an isolated git worktree.
func (c *Client) CreateWorktree(ctx context.Context, taskID string) (*WorktreeInfo, error) {
	body := struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID}
	var info WorktreeInfo
	if err := c.doJSON(ctx, http.MethodPost, "/worktree", body, &info); err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	return &info, nil
}

// DeleteWorktree removes the worktree associated with taskID.
func (c *Client) DeleteWorktree(ctx context.Context, taskID string) error {
	if err := c.doJSON(ctx, http.MethodDelete, "/worktree/"+taskID, nil, nil); err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// File operations
// ---------------------------------------------------------------------------

// ReadFile returns the contents of path inside the taskID worktree.
func (c *Client) ReadFile(ctx context.Context, taskID, path string) (string, error) {
	params := url.Values{"task_id": {taskID}, "path": {path}}
	var result struct {
		Content string `json:"content"`
	}
	if err := c.doGet(ctx, "/file", params, &result); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return result.Content, nil
}

// WriteFile writes content to path inside the taskID worktree.
func (c *Client) WriteFile(ctx context.Context, taskID, path, content string) error {
	body := struct {
		TaskID  string `json:"task_id"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}{TaskID: taskID, Path: path, Content: content}
	if err := c.doJSON(ctx, http.MethodPut, "/file", body, nil); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// ListDir lists the entries in directory path inside the taskID worktree.
func (c *Client) ListDir(ctx context.Context, taskID, path string) ([]FileEntry, error) {
	body := struct {
		TaskID string `json:"task_id"`
		Path   string `json:"path"`
	}{TaskID: taskID, Path: path}
	var result struct {
		Entries []FileEntry `json:"entries"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/list", body, &result); err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}
	return result.Entries, nil
}

// Search runs a regex search for pattern inside the taskID worktree.
func (c *Client) Search(ctx context.Context, taskID, pattern, fileGlob string) ([]SearchMatch, error) {
	body := struct {
		TaskID   string `json:"task_id"`
		Pattern  string `json:"pattern"`
		FileGlob string `json:"file_glob,omitempty"`
	}{TaskID: taskID, Pattern: pattern, FileGlob: fileGlob}
	var result struct {
		Matches []SearchMatch `json:"matches"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/search", body, &result); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return result.Matches, nil
}

// ---------------------------------------------------------------------------
// Git operations
// ---------------------------------------------------------------------------

// GitStatus returns the output of `git status --porcelain` inside the worktree.
func (c *Client) GitStatus(ctx context.Context, taskID string) (string, error) {
	body := struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID}
	var result struct {
		Output string `json:"output"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/git/status", body, &result); err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return result.Output, nil
}

// GitCommit stages all changes and creates a commit.
func (c *Client) GitCommit(ctx context.Context, taskID, message string) (*CommitResult, error) {
	body := struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}{TaskID: taskID, Message: message}
	var result CommitResult
	if err := c.doJSON(ctx, http.MethodPost, "/git/commit", body, &result); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}
	return &result, nil
}

// GitDiff returns the output of `git diff` (staged + unstaged).
func (c *Client) GitDiff(ctx context.Context, taskID string) (string, error) {
	body := struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID}
	var result struct {
		Output string `json:"output"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/git/diff", body, &result); err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return result.Output, nil
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// Health pings the sandbox server.
func (c *Client) Health(ctx context.Context) error {
	if err := c.doGet(ctx, "/health", nil, nil); err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal HTTP helpers
// ---------------------------------------------------------------------------

func (c *Client) doJSON(ctx context.Context, method, path string, body, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.do(req, result)
}

func (c *Client) doGet(ctx context.Context, path string, params url.Values, result any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	return c.do(req, result)
}

func (c *Client) do(req *http.Request, result any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var errBody struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(data, &errBody); jsonErr == nil && errBody.Error != "" {
			return fmt.Errorf("server error %d: %s", resp.StatusCode, errBody.Error)
		}
		return fmt.Errorf("server error %d", resp.StatusCode)
	}

	if result != nil && len(data) > 0 {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

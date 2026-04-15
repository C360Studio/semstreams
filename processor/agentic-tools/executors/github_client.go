// Package executors provides tool executor implementations for the agentic-tools component.
package executors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// GitHubClient defines the interface for GitHub API operations.
// All methods accept context for cancellation and timeout propagation.
type GitHubClient interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error)
	ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOpts) ([]GitHubIssue, error)
	SearchIssues(ctx context.Context, query string) ([]GitHubIssue, error)
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*GitHubPR, error)
	GetFileContent(ctx context.Context, owner, repo, path, ref string) (string, error)
	CreateBranch(ctx context.Context, owner, repo, branch, baseSHA string) error
	CommitFile(ctx context.Context, owner, repo, branch, path, content, message string) error
	CreatePullRequest(ctx context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error)
	AddComment(ctx context.Context, owner, repo string, number int, body string) error
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
}

// GitHubIssue represents a GitHub issue with the fields relevant to the agentic system.
type GitHubIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Labels    []string  `json:"labels"`
	Assignee  string    `json:"assignee,omitempty"`
	Author    string    `json:"author"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Comments  int       `json:"comments"`
}

// GitHubPR represents a GitHub pull request with the fields relevant to the agentic system.
type GitHubPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	Head      string    `json:"head"`
	Base      string    `json:"base"`
	Mergeable *bool     `json:"mergeable,omitempty"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	DiffURL   string    `json:"diff_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListIssuesOpts configures the issue listing query.
type ListIssuesOpts struct {
	State  string // open, closed, all
	Labels []string
	Sort   string // created, updated, comments
	Limit  int
}

// defaultAPIBase is the base URL for the GitHub REST API v3.
const defaultAPIBase = "https://api.github.com"

// GitHubHTTPClient implements GitHubClient using the GitHub REST API v3.
// It authenticates via a Bearer token and parses only the fields the system needs.
type GitHubHTTPClient struct {
	token      string
	httpClient *http.Client
	apiBase    string // configurable for testing; defaults to defaultAPIBase
}

// NewGitHubHTTPClient creates a new GitHubHTTPClient with the given personal access token.
func NewGitHubHTTPClient(token string) *GitHubHTTPClient {
	return &GitHubHTTPClient{
		token:   token,
		apiBase: defaultAPIBase,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// newGitHubHTTPClientWithBase creates a client that sends all requests to the given
// base URL. Intended for unit tests that spin up httptest.Server.
func newGitHubHTTPClientWithBase(token, baseURL string) *GitHubHTTPClient {
	return &GitHubHTTPClient{
		token:   token,
		apiBase: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// do executes an HTTP request against the GitHub API, setting required headers.
// The caller is responsible for closing the response body.
func (c *GitHubHTTPClient) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	return c.doURL(ctx, method, c.apiBase+path, body)
}

// doURL executes an HTTP request against an absolute URL.
// Exposed so tests can target httptest.Server instances directly.
func (c *GitHubHTTPClient) doURL(ctx context.Context, method, apiURL string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "semstreams-agent/1.0")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// classifyGitHubError maps an error returned by the GitHub client into a
// ToolErrorKind suitable for the agent.step.error_category graph predicate.
// HTTP status codes are extracted from httpStatusError; transport failures
// are detected by type assertion; context failures take precedence.
func classifyGitHubError(err error) agentic.ToolErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agentic.ToolErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		return agentic.ToolErrorTimeout
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return classifyHTTPStatus(statusErr.StatusCode)
	}
	// net.Error catches both raw transport errors and *url.Error (which
	// implements net.Error via Timeout/Temporary), covering dial, DNS,
	// connection reset, and client-side timeouts.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return agentic.ToolErrorTimeout
		}
		return agentic.ToolErrorNetwork
	}
	return agentic.ToolErrorExternal
}

// classifyHTTPStatus maps an HTTP status code to a ToolErrorKind.
func classifyHTTPStatus(code int) agentic.ToolErrorKind {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return agentic.ToolErrorPermission
	case code == http.StatusNotFound || code == http.StatusGone:
		return agentic.ToolErrorNotFound
	case code == http.StatusTooManyRequests:
		return agentic.ToolErrorExternal
	case code == http.StatusRequestTimeout || code == http.StatusGatewayTimeout:
		return agentic.ToolErrorTimeout
	case code >= 400 && code < 500:
		// Validation, conflict, unprocessable entity, etc.
		return agentic.ToolErrorInvalidArgs
	case code >= 500:
		return agentic.ToolErrorExternal
	}
	return agentic.ToolErrorUnknown
}

// httpStatusError is returned by checkStatus when a response's status code
// is not in the accepted set. It preserves the status code for classification.
type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.StatusCode, e.Body)
}

// checkStatus returns an error if the response status is not one of the accepted codes.
func checkStatus(resp *http.Response, accepted ...int) error {
	for _, code := range accepted {
		if resp.StatusCode == code {
			return nil
		}
	}
	body, _ := io.ReadAll(resp.Body)
	return &httpStatusError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
	}
}

// GetIssue fetches a single issue by number.
func (c *GitHubHTTPClient) GetIssue(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	return decodeIssue(resp.Body)
}

// ListIssues returns a list of issues for a repository filtered by opts.
func (c *GitHubHTTPClient) ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOpts) ([]GitHubIssue, error) {
	params := url.Values{}
	if opts.State != "" {
		params.Set("state", opts.State)
	} else {
		params.Set("state", "open")
	}
	if len(opts.Labels) > 0 {
		params.Set("labels", strings.Join(opts.Labels, ","))
	}
	if opts.Sort != "" {
		params.Set("sort", opts.Sort)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 30
	}
	params.Set("per_page", fmt.Sprintf("%d", limit))

	path := fmt.Sprintf("/repos/%s/%s/issues?%s", owner, repo, params.Encode())
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	return decodeIssueList(resp.Body)
}

// SearchIssues executes a GitHub issue search query.
func (c *GitHubHTTPClient) SearchIssues(ctx context.Context, query string) ([]GitHubIssue, error) {
	params := url.Values{}
	params.Set("q", query)
	path := "/search/issues?" + params.Encode()

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}

	// Search returns { "items": [...] }
	var result struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	issues := make([]GitHubIssue, 0, len(result.Items))
	for _, raw := range result.Items {
		issue, err := decodeIssueFromRaw(raw)
		if err != nil {
			return nil, err
		}
		issues = append(issues, *issue)
	}
	return issues, nil
}

// GetPullRequest fetches a single pull request by number.
func (c *GitHubHTTPClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*GitHubPR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}

	return decodePR(resp.Body)
}

// GetFileContent retrieves the decoded text content of a file at a specific ref.
func (c *GitHubHTTPClient) GetFileContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	params := url.Values{}
	if ref != "" {
		params.Set("ref", ref)
	}
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, strings.TrimPrefix(path, "/"))
	if len(params) > 0 {
		apiPath += "?" + params.Encode()
	}

	resp, err := c.do(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return "", fmt.Errorf("get file content: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return "", fmt.Errorf("get file content: %w", err)
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode file content response: %w", err)
	}

	if result.Encoding != "base64" {
		return "", fmt.Errorf("unsupported encoding: %s", result.Encoding)
	}

	// GitHub base64-encodes with newlines; strip them before decoding.
	cleaned := strings.ReplaceAll(result.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("decode base64 content: %w", err)
	}

	return string(decoded), nil
}

// CreateBranch creates a new branch from the given base SHA.
// It uses the Git refs API rather than the higher-level branch API so the
// caller controls exactly which commit the branch points at.
func (c *GitHubHTTPClient) CreateBranch(ctx context.Context, owner, repo, branch, baseSHA string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo)
	body := map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": baseSHA,
	}

	resp, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("create branch: %w", err)
	}
	defer resp.Body.Close()

	// 201 = created, 422 = already exists (treat as success so callers can be idempotent)
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusUnprocessableEntity {
		return nil
	}
	return checkStatus(resp, http.StatusCreated)
}

// CommitFile creates or updates a single file on a branch using the Contents API.
// content is the raw file text; this method handles base64 encoding.
func (c *GitHubHTTPClient) CommitFile(ctx context.Context, owner, repo, branch, path, content, message string) error {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, strings.TrimPrefix(path, "/"))

	// Fetch the current file SHA if the file already exists, so GitHub accepts the update.
	existingSHA, err := c.getFileSHA(ctx, owner, repo, path, branch)
	if err != nil {
		return fmt.Errorf("commit file: check existing: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	body := map[string]any{
		"message": message,
		"content": encoded,
		"branch":  branch,
	}
	if existingSHA != "" {
		body["sha"] = existingSHA
	}

	resp, err := c.do(ctx, http.MethodPut, apiPath, body)
	if err != nil {
		return fmt.Errorf("commit file: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp, http.StatusCreated, http.StatusOK)
}

// getFileSHA returns the blob SHA for a file if it exists, or an empty string if it does not.
func (c *GitHubHTTPClient) getFileSHA(ctx context.Context, owner, repo, path, ref string) (string, error) {
	params := url.Values{}
	if ref != "" {
		params.Set("ref", ref)
	}
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, strings.TrimPrefix(path, "/"))
	if len(params) > 0 {
		apiPath += "?" + params.Encode()
	}

	resp, err := c.do(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return "", fmt.Errorf("get file SHA: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil // File does not yet exist — create rather than update.
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return "", err
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode file SHA: %w", err)
	}
	return result.SHA, nil
}

// CreatePullRequest opens a new pull request from head into base.
func (c *GitHubHTTPClient) CreatePullRequest(ctx context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	reqBody := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	resp, err := c.do(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}

	return decodePR(resp.Body)
}

// AddComment posts a comment on an issue or pull request.
func (c *GitHubHTTPClient) AddComment(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	reqBody := map[string]string{"body": body}

	resp, err := c.do(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp, http.StatusCreated)
}

// AddLabels applies labels to an issue or pull request.
func (c *GitHubHTTPClient) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	reqBody := map[string]any{"labels": labels}

	resp, err := c.do(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return fmt.Errorf("add labels: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp, http.StatusOK)
}

// --- JSON decoding helpers ---

// githubIssueRaw is the wire format from the GitHub API.
// We parse only what we need and flatten nested objects.
type githubIssueRaw struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Assignee *struct {
		Login string `json:"login"`
	} `json:"assignee"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func decodeIssue(r io.Reader) (*GitHubIssue, error) {
	var raw githubIssueRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return issueFromRaw(raw), nil
}

func decodeIssueList(r io.Reader) ([]GitHubIssue, error) {
	var raws []githubIssueRaw
	if err := json.NewDecoder(r).Decode(&raws); err != nil {
		return nil, fmt.Errorf("decode issue list: %w", err)
	}
	issues := make([]GitHubIssue, 0, len(raws))
	for _, raw := range raws {
		issues = append(issues, *issueFromRaw(raw))
	}
	return issues, nil
}

func decodeIssueFromRaw(data json.RawMessage) (*GitHubIssue, error) {
	var raw githubIssueRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode issue from raw: %w", err)
	}
	return issueFromRaw(raw), nil
}

func issueFromRaw(raw githubIssueRaw) *GitHubIssue {
	labels := make([]string, 0, len(raw.Labels))
	for _, l := range raw.Labels {
		labels = append(labels, l.Name)
	}
	assignee := ""
	if raw.Assignee != nil {
		assignee = raw.Assignee.Login
	}
	return &GitHubIssue{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		Labels:    labels,
		Assignee:  assignee,
		Author:    raw.User.Login,
		HTMLURL:   raw.HTMLURL,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
		Comments:  raw.Comments,
	}
}

// githubPRRaw is the wire format for a pull request from the GitHub API.
type githubPRRaw struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	DiffURL   string    `json:"diff_url"`
	Mergeable *bool     `json:"mergeable"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func decodePR(r io.Reader) (*GitHubPR, error) {
	var raw githubPRRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode pull request: %w", err)
	}
	return &GitHubPR{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		HTMLURL:   raw.HTMLURL,
		DiffURL:   raw.DiffURL,
		Head:      raw.Head.Ref,
		Base:      raw.Base.Ref,
		Mergeable: raw.Mergeable,
		Additions: raw.Additions,
		Deletions: raw.Deletions,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}

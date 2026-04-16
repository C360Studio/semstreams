package executors

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
)

// GitHubReadExecutor provides read-only GitHub tools to agentic agents.
// It fetches issues, pull requests, and file content without mutating any state.
type GitHubReadExecutor struct {
	client GitHubClient
}

// NewGitHubReadExecutor creates a new GitHubReadExecutor backed by the given client.
func NewGitHubReadExecutor(client GitHubClient) *GitHubReadExecutor {
	return &GitHubReadExecutor{client: client}
}

// ListTools returns the tool definitions provided by this executor.
func (e *GitHubReadExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "github_get_issue",
			Description: "Fetch a single GitHub issue by repository and issue number. Returns the issue title, body, labels, state, and metadata.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{
						"type":        "string",
						"description": "Repository owner (user or organization name)",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Repository name",
					},
					"number": map[string]any{
						"type":        "integer",
						"description": "Issue number",
					},
				},
				"required": []string{"owner", "repo", "number"},
			},
		},
		{
			Name:        "github_list_issues",
			Description: "List issues for a GitHub repository with optional filtering by state and labels.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{
						"type":        "string",
						"description": "Repository owner (user or organization name)",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Repository name",
					},
					"state": map[string]any{
						"type":        "string",
						"enum":        []string{"open", "closed", "all"},
						"description": "Filter issues by state (default: open)",
					},
					"labels": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by label names",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of issues to return (default: 10, max: 100)",
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{"owner", "repo"},
			},
		},
		{
			Name:        "github_search_issues",
			Description: "Search GitHub issues and pull requests using GitHub's search syntax (e.g., 'repo:owner/name is:open label:bug').",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GitHub search query string",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "github_get_pr",
			Description: "Fetch a single GitHub pull request by repository and PR number. Returns the PR title, body, head/base branches, diff stats, and merge status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{
						"type":        "string",
						"description": "Repository owner (user or organization name)",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Repository name",
					},
					"number": map[string]any{
						"type":        "integer",
						"description": "Pull request number",
					},
				},
				"required": []string{"owner", "repo", "number"},
			},
		},
		{
			Name:        "github_get_file",
			Description: "Retrieve the text content of a file from a GitHub repository at a specific ref (branch, tag, or commit SHA).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{
						"type":        "string",
						"description": "Repository owner (user or organization name)",
					},
					"repo": map[string]any{
						"type":        "string",
						"description": "Repository name",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file within the repository (e.g., src/main.go)",
					},
					"ref": map[string]any{
						"type":        "string",
						"description": "Branch, tag, or commit SHA to read from (default: main)",
					},
				},
				"required": []string{"owner", "repo", "path"},
			},
		},
	}
}

// Execute dispatches the tool call to the appropriate handler.
func (e *GitHubReadExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "github_get_issue":
		return e.getIssue(ctx, call)
	case "github_list_issues":
		return e.listIssues(ctx, call)
	case "github_search_issues":
		return e.searchIssues(ctx, call)
	case "github_get_pr":
		return e.getPR(ctx, call)
	case "github_get_file":
		return e.getFile(ctx, call)
	default:
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "GitHubReadExecutor", "Execute", "find tool")
	}
}

func (e *GitHubReadExecutor) getIssue(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	number, ok := extractInt(call.Arguments, "number")
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "number is required and must be an integer", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	issue, err := e.client.GetIssue(ctx, owner, repo, number)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_get_issue failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(issue)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"number":   issue.Number,
			"state":    issue.State,
			"html_url": issue.HTMLURL,
		},
	}, nil
}

func (e *GitHubReadExecutor) listIssues(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	opts := ListIssuesOpts{
		State: "open",
		Limit: 10,
	}
	if state, ok := call.Arguments["state"].(string); ok && state != "" {
		opts.State = state
	}
	if labelsRaw, ok := call.Arguments["labels"]; ok {
		opts.Labels = toStringSlice(labelsRaw)
	}
	if limit, ok := extractInt(call.Arguments, "limit"); ok && limit > 0 {
		opts.Limit = limit
	}

	issues, err := e.client.ListIssues(ctx, owner, repo, opts)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_list_issues failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(issues)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"count": len(issues),
			"state": opts.State,
		},
	}, nil
}

func (e *GitHubReadExecutor) searchIssues(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "query is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	issues, err := e.client.SearchIssues(ctx, query)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_search_issues failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(issues)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"count": len(issues),
			"query": query,
		},
	}, nil
}

func (e *GitHubReadExecutor) getPR(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	number, ok := extractInt(call.Arguments, "number")
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "number is required and must be an integer", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	pr, err := e.client.GetPullRequest(ctx, owner, repo, number)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_get_pr failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(pr)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"number":   pr.Number,
			"state":    pr.State,
			"html_url": pr.HTMLURL,
		},
	}, nil
}

func (e *GitHubReadExecutor) getFile(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	filePath, ok := call.Arguments["path"].(string)
	if !ok || filePath == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "path is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	ref := "main"
	if r, ok := call.Arguments["ref"].(string); ok && r != "" {
		ref = r
	}

	content, err := e.client.GetFileContent(ctx, owner, repo, filePath, ref)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_get_file failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"path": filePath,
			"ref":  ref,
		},
	}, nil
}

// --- shared argument helpers ---

// extractOwnerRepo pulls owner and repo strings from a tool call's arguments.
func extractOwnerRepo(call agentic.ToolCall) (owner, repo string, ok bool) {
	owner, ownerOK := call.Arguments["owner"].(string)
	repo, repoOK := call.Arguments["repo"].(string)
	return owner, repo, ownerOK && owner != "" && repoOK && repo != ""
}

// extractInt extracts an integer argument from a map, handling the float64 that JSON decoding produces.
func extractInt(args map[string]any, key string) (int, bool) {
	v, exists := args[key]
	if !exists {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// toStringSlice converts an interface{} that may be []interface{} or []string to []string.
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []interface{}:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return nil
}

// marshalPretty serializes v to indented JSON.
func marshalPretty(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

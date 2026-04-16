package executors

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
)

// GitHubWriteExecutor provides write tools that let agentic agents mutate GitHub state.
// Operations include branch creation, file commits, PR creation, comments, and labels.
type GitHubWriteExecutor struct {
	client GitHubClient
}

// NewGitHubWriteExecutor creates a new GitHubWriteExecutor backed by the given client.
func NewGitHubWriteExecutor(client GitHubClient) *GitHubWriteExecutor {
	return &GitHubWriteExecutor{client: client}
}

// ListTools returns the tool definitions provided by this executor.
func (e *GitHubWriteExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "github_create_branch",
			Description: "Create a new Git branch in a GitHub repository from a specific commit SHA.",
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
					"branch": map[string]any{
						"type":        "string",
						"description": "Name for the new branch (e.g., feat/issue-42)",
					},
					"base_sha": map[string]any{
						"type":        "string",
						"description": "The commit SHA that the new branch should point to",
					},
				},
				"required": []string{"owner", "repo", "branch", "base_sha"},
			},
		},
		{
			Name:        "github_commit_file",
			Description: "Create or update a single file in a GitHub repository on a specific branch.",
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
					"branch": map[string]any{
						"type":        "string",
						"description": "Branch to commit to",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File path within the repository (e.g., src/feature.go)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Full text content to write to the file",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Commit message",
					},
				},
				"required": []string{"owner", "repo", "branch", "path", "content", "message"},
			},
		},
		{
			Name:        "github_create_pr",
			Description: "Open a new pull request on GitHub from a head branch into a base branch.",
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
					"title": map[string]any{
						"type":        "string",
						"description": "Pull request title",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Pull request description (supports Markdown)",
					},
					"head": map[string]any{
						"type":        "string",
						"description": "Name of the branch that contains the changes",
					},
					"base": map[string]any{
						"type":        "string",
						"description": "Name of the branch to merge into (e.g., main)",
					},
				},
				"required": []string{"owner", "repo", "title", "body", "head", "base"},
			},
		},
		{
			Name:        "github_add_comment",
			Description: "Post a comment on a GitHub issue or pull request.",
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
						"description": "Issue or pull request number",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Comment text (supports Markdown)",
					},
				},
				"required": []string{"owner", "repo", "number", "body"},
			},
		},
		{
			Name:        "github_add_label",
			Description: "Apply one or more labels to a GitHub issue or pull request.",
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
						"description": "Issue or pull request number",
					},
					"labels": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Label names to apply",
					},
				},
				"required": []string{"owner", "repo", "number", "labels"},
			},
		},
	}
}

// Execute dispatches the tool call to the appropriate handler.
func (e *GitHubWriteExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "github_create_branch":
		return e.createBranch(ctx, call)
	case "github_commit_file":
		return e.commitFile(ctx, call)
	case "github_create_pr":
		return e.createPR(ctx, call)
	case "github_add_comment":
		return e.addComment(ctx, call)
	case "github_add_label":
		return e.addLabel(ctx, call)
	default:
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "GitHubWriteExecutor", "Execute", "find tool")
	}
}

func (e *GitHubWriteExecutor) createBranch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	branch, ok := call.Arguments["branch"].(string)
	if !ok || branch == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "branch is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	baseSHA, ok := call.Arguments["base_sha"].(string)
	if !ok || baseSHA == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "base_sha is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	if err := e.client.CreateBranch(ctx, owner, repo, branch, baseSHA); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_create_branch failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(map[string]any{
		"branch": branch,
		"sha":    baseSHA,
		"status": "created",
	})
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"branch":   branch,
			"base_sha": baseSHA,
		},
	}, nil
}

func (e *GitHubWriteExecutor) commitFile(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	branch, ok := call.Arguments["branch"].(string)
	if !ok || branch == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "branch is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	filePath, ok := call.Arguments["path"].(string)
	if !ok || filePath == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "path is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	fileContent, ok := call.Arguments["content"].(string)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "content is required and must be a string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	message, ok := call.Arguments["message"].(string)
	if !ok || message == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "message is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	if err := e.client.CommitFile(ctx, owner, repo, branch, filePath, fileContent, message); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_commit_file failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(map[string]any{
		"path":    filePath,
		"branch":  branch,
		"message": message,
		"status":  "committed",
	})
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"path":   filePath,
			"branch": branch,
		},
	}, nil
}

func (e *GitHubWriteExecutor) createPR(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	title, ok := call.Arguments["title"].(string)
	if !ok || title == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "title is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	// body is required per the spec; an empty body is allowed (PR with no description)
	body, ok := call.Arguments["body"].(string)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "body is required and must be a string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	head, ok := call.Arguments["head"].(string)
	if !ok || head == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "head is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	base, ok := call.Arguments["base"].(string)
	if !ok || base == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "base is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	pr, err := e.client.CreatePullRequest(ctx, owner, repo, title, body, head, base)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_create_pr failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
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
			"html_url": pr.HTMLURL,
		},
	}, nil
}

func (e *GitHubWriteExecutor) addComment(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	number, ok := extractInt(call.Arguments, "number")
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "number is required and must be an integer", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	body, ok := call.Arguments["body"].(string)
	if !ok || body == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "body is required and must be a non-empty string", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	if err := e.client.AddComment(ctx, owner, repo, number, body); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_add_comment failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(map[string]any{
		"number": number,
		"status": "commented",
	})
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"number": number,
		},
	}, nil
}

func (e *GitHubWriteExecutor) addLabel(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	owner, repo, ok := extractOwnerRepo(call)
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "owner and repo are required string parameters", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	number, ok := extractInt(call.Arguments, "number")
	if !ok {
		return agentic.ToolResult{CallID: call.ID, Error: "number is required and must be an integer", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	labelsRaw, exists := call.Arguments["labels"]
	if !exists {
		return agentic.ToolResult{CallID: call.ID, Error: "labels is required", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}
	labels := toStringSlice(labelsRaw)
	if len(labels) == 0 {
		return agentic.ToolResult{CallID: call.ID, Error: "labels must be a non-empty array of strings", ErrorKind: agentic.ToolErrorInvalidArgs}, nil
	}

	if err := e.client.AddLabels(ctx, owner, repo, number, labels); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("github_add_label failed: %v", err), ErrorKind: classifyGitHubError(err)}, nil
	}

	content, err := marshalPretty(map[string]any{
		"number": number,
		"labels": labels,
		"status": "labeled",
	})
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("marshal result: %v", err), ErrorKind: agentic.ToolErrorInternal}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"number": number,
			"labels": labels,
		},
	}, nil
}

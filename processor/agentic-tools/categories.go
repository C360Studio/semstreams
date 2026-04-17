package agentictools

import (
	"sync"
)

// ToolCategory groups tools by their primary function for token optimization
// and role-based filtering. Inspired by semdragon's category system.
type ToolCategory string

const (
	// CategoryCore contains essential tools available to all agents (submit_work, etc.)
	CategoryCore ToolCategory = "core"

	// CategoryKnowledge contains graph query and search tools.
	CategoryKnowledge ToolCategory = "knowledge"

	// CategoryNetwork contains web search, HTTP request, and external API tools.
	CategoryNetwork ToolCategory = "network"

	// CategoryInspect contains code execution tools (bash, sandbox).
	CategoryInspect ToolCategory = "inspect"

	// CategoryOrchestration contains tools for spawning sub-agents and decomposing tasks.
	CategoryOrchestration ToolCategory = "orchestration"

	// CategoryMeta contains tools for self-programming (create_rule, manage_flow).
	CategoryMeta ToolCategory = "meta"
)

// toolCategoryMu protects concurrent access to toolCategories.
var toolCategoryMu sync.RWMutex

// toolCategories maps tool names to their categories.
var toolCategories = map[string]ToolCategory{
	// Core
	"submit_work": CategoryCore,

	// Knowledge
	"graph_query":   CategoryKnowledge,
	"graph_search":  CategoryKnowledge,
	"graph_summary": CategoryKnowledge,

	// Network
	"web_search":   CategoryNetwork,
	"http_request": CategoryNetwork,

	// Inspect
	"bash": CategoryInspect,

	// Orchestration
	"spawn_agent":     CategoryOrchestration,
	"decompose_task":  CategoryOrchestration,
	"ask_question":    CategoryOrchestration,
	"answer_question": CategoryOrchestration,

	// Meta
	"create_rule": CategoryMeta,
	"update_rule": CategoryMeta,
	"delete_rule": CategoryMeta,
	"create_flow": CategoryMeta,
	"deploy_flow": CategoryMeta,

	// GitHub tools
	"github_read":   CategoryKnowledge,
	"github_write":  CategoryInspect,
	"github_init":   CategoryCore,
	"github_client": CategoryCore,
}

// GetToolCategory returns the category for a tool name.
// Returns CategoryCore as default for unregistered tools.
func GetToolCategory(toolName string) ToolCategory {
	toolCategoryMu.RLock()
	defer toolCategoryMu.RUnlock()
	if cat, ok := toolCategories[toolName]; ok {
		return cat
	}
	return CategoryCore
}

// RegisterToolCategory registers a category for a tool name.
// This allows executor implementations to declare their category at registration time.
func RegisterToolCategory(toolName string, category ToolCategory) {
	toolCategoryMu.Lock()
	defer toolCategoryMu.Unlock()
	toolCategories[toolName] = category
}

// ReadOnlyCategories returns the set of categories safe for explore sub-agents
// (no write operations, no orchestration, no meta-programming).
func ReadOnlyCategories() map[ToolCategory]bool {
	return map[ToolCategory]bool{
		CategoryCore:      true,
		CategoryKnowledge: true,
		CategoryNetwork:   true,
	}
}

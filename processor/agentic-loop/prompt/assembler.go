package prompt

import (
	"fmt"
	"strings"
)

// AssembledPrompt is the output of the assembly process.
type AssembledPrompt struct {
	SystemMessage string   // Composed system prompt
	FragmentsUsed []string // Fragment IDs included (for observability)
}

// Assemble composes a system prompt from registry fragments filtered by context.
// Fragments are ordered by category then priority, and their content is
// concatenated with double newlines between sections.
func Assemble(registry *Registry, ctx *AssemblyContext) AssembledPrompt {
	fragments := registry.GetForContext(ctx)

	var sections []string
	var usedIDs []string

	for _, f := range fragments {
		content := f.Content
		if f.ContentFunc != nil {
			content = f.ContentFunc(ctx)
		}

		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		sections = append(sections, content)
		usedIDs = append(usedIDs, f.ID)
	}

	return AssembledPrompt{
		SystemMessage: strings.Join(sections, "\n\n"),
		FragmentsUsed: usedIDs,
	}
}

// DefaultFragments returns the base set of prompt fragments used by all agents.
// These provide the core behavioral framework. Role-specific fragments should
// be added separately via the registry.
func DefaultFragments() []Fragment {
	return []Fragment{
		{
			ID:       "system-identity",
			Category: CategorySystem,
			Priority: 0,
			Content:  "You are an AI agent in the SemStreams agentic system. You have access to tools to accomplish tasks. Use tools when needed and submit your work when complete.",
		},
		{
			ID:       "system-tool-usage",
			Category: CategoryTools,
			Priority: 0,
			Content:  "Use the available tools to accomplish your task. Call tools one at a time and wait for results before proceeding. If a tool call fails, analyze the error and try a different approach.",
		},
		{
			ID:       "system-submit-work",
			Category: CategoryTools,
			Priority: 10,
			Content:  "When your task is complete, call the submit_work tool with your deliverables. Do not simply state that you are done — always submit through the tool.",
		},
		{
			ID:       "constraint-iteration-budget",
			Category: CategoryConstraints,
			Priority: 0,
			ContentFunc: func(ctx *AssemblyContext) string {
				if ctx.MaxIterations <= 0 {
					return ""
				}
				ratio := float64(ctx.Iteration) / float64(ctx.MaxIterations)
				remaining := ctx.MaxIterations - ctx.Iteration
				switch {
				case ratio > 0.75:
					return fmt.Sprintf("URGENT: You have %d iterations remaining out of %d. Wrap up immediately and submit your work.", remaining, ctx.MaxIterations)
				case ratio > 0.5:
					return fmt.Sprintf("You have used %d of %d iterations. Start wrapping up and prepare to submit.", ctx.Iteration, ctx.MaxIterations)
				default:
					return ""
				}
			},
		},
		{
			ID:       "constraint-child-agent",
			Category: CategoryConstraints,
			Priority: 10,
			Condition: func(ctx *AssemblyContext) bool {
				return ctx.ParentLoopID != ""
			},
			ContentFunc: func(ctx *AssemblyContext) string {
				return fmt.Sprintf("You are a child agent (depth %d/%d) spawned by parent loop %s. Focus on your assigned subtask and submit results back to the parent.", ctx.Depth, ctx.MaxDepth, ctx.ParentLoopID)
			},
		},
		{
			ID:       "role-explorer",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"explorer"},
			Content:  "You are an explore agent. Your job is to gather information using read-only tools (graph queries, web search, file reading). Do NOT modify any state. Summarize your findings clearly and submit them.",
		},
		{
			ID:       "role-architect",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"architect"},
			Content:  "You are an architect agent. Design the high-level approach, identify components and interfaces, consider trade-offs, and produce a clear plan. Do not write implementation code — that is the editor's job.",
		},
		{
			ID:       "role-editor",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"editor"},
			Content:  "You are an editor agent. Implement the changes specified by the architect. Write clean, tested code. Follow existing patterns in the codebase. Submit your work when implementation is complete.",
		},
		{
			ID:       "role-reviewer",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"reviewer"},
			Content:  "You are a reviewer agent. Review the editor's work for correctness, code quality, test coverage, and adherence to the architect's design. Approve if satisfactory, or reject with specific actionable feedback.",
		},
		{
			ID:       "role-general",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"general"},
			Content:  "You are a general-purpose agent. Analyze the task, use available tools, and produce the best result you can. If the task is complex, break it into steps and work through them methodically.",
		},
		{
			ID:       "role-researcher",
			Category: CategoryRole,
			Priority: 0,
			Roles:    []string{"researcher"},
			Content: `You are a research agent. Your job is to investigate a topic thoroughly using the tools available to you, then synthesize your findings into a clear, structured report.

Research methodology:
1. Break the question into sub-questions
2. Search the knowledge graph for existing entities and relationships (query_entity, query_entities, query_relationships)
3. Search the web for additional context (web_search)
4. Fetch specific URLs for detailed information (http_request)
5. Synthesize findings, cite sources, note gaps

Use graph query tools first — they search curated domain knowledge that may be more authoritative than web results. Use web search for supplementary context and recent developments.

When your research is complete, present findings as a structured report:
- **Summary**: 2-3 sentence overview
- **Key Findings**: numbered list of the most important discoveries
- **Sources**: URLs and entity IDs you referenced
- **Gaps / Open Questions**: what you could not determine and where to look next`,
		},
	}
}

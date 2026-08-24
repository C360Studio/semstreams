package agentprofiles

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfileContract(t *testing.T) {
	root := repositoryRoot(t)

	canonical := map[string][]string{
		".agents/contracts/semstreams-developer.md": {
			"## Required workflow",
			"## Semantic identity and graph contracts",
			"## Storage and retention contracts",
			"## Test and operational fidelity",
		},
		".agents/contracts/semstreams-reviewer.md": {
			"## Required review workflow",
			"## Semantic identity and graph review",
			"## Storage, retention, and cutover review",
			"### Test fidelity",
			"## Finding and verdict format",
			"APPROVE",
			"CHANGES REQUESTED",
		},
	}
	for name, snippets := range canonical {
		t.Run(name, func(t *testing.T) {
			body := readProfileFile(t, root, name)
			for _, snippet := range snippets {
				if !strings.Contains(body, snippet) {
					t.Errorf("%s is missing required contract text %q", name, snippet)
				}
			}
		})
	}

	adapters := map[string]string{
		".claude/agents/semstreams-developer.md":  ".agents/contracts/semstreams-developer.md",
		".claude/agents/semstreams-reviewer.md":   ".agents/contracts/semstreams-reviewer.md",
		".codex/agents/semstreams-developer.toml": ".agents/contracts/semstreams-developer.md",
		".codex/agents/semstreams-reviewer.toml":  ".agents/contracts/semstreams-reviewer.md",
	}
	for name, contract := range adapters {
		t.Run(name, func(t *testing.T) {
			body := readProfileFile(t, root, name)
			if got := strings.Count(body, contract); got != 1 {
				t.Errorf("%s must reference %s exactly once; got %d", name, contract, got)
			}
			if lineCount(body) >= 40 {
				t.Errorf("%s must remain a thin adapter of fewer than 40 lines; got %d", name, lineCount(body))
			}
		})
	}

	t.Run("reviewers exclude direct write tools and Codex is sandboxed read-only", func(t *testing.T) {
		claude := readProfileFile(t, root, ".claude/agents/semstreams-reviewer.md")
		toolsLine := ""
		for _, line := range strings.Split(claude, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "tools:") {
				toolsLine = line
				break
			}
		}
		if toolsLine == "" {
			t.Fatal("Claude reviewer is missing a tools frontmatter entry")
		}
		for _, forbidden := range []string{"Edit", "Write", "Task"} {
			for _, tool := range strings.Split(strings.TrimPrefix(strings.TrimSpace(toolsLine), "tools:"), ",") {
				if strings.TrimSpace(tool) == forbidden {
					t.Errorf("Claude reviewer must not include %s", forbidden)
				}
			}
		}

		codex := readProfileFile(t, root, ".codex/agents/semstreams-reviewer.toml")
		for _, snippet := range []string{
			`name = "semstreams-reviewer"`,
			`sandbox_mode = "read-only"`,
		} {
			if !strings.Contains(codex, snippet) {
				t.Errorf("Codex reviewer is missing %q", snippet)
			}
		}
	})

	t.Run("Codex developer identity", func(t *testing.T) {
		body := readProfileFile(t, root, ".codex/agents/semstreams-developer.toml")
		if !strings.Contains(body, `name = "semstreams-developer"`) {
			t.Error("Codex developer has the wrong or missing name")
		}
	})

	t.Run("routing is symmetric and ordered", func(t *testing.T) {
		agents := markdownSection(t, readProfileFile(t, root, "AGENTS.md"), "## Semantic Agent Routing")
		claude := markdownSection(t, readProfileFile(t, root, "CLAUDE.md"), "## Semantic Agent Routing")
		if agents != claude {
			t.Error("AGENTS.md and CLAUDE.md must have identical Semantic Agent Routing sections")
		}

		developer := strings.Index(agents, "`semstreams-developer`")
		reviewer := strings.Index(agents, "`semstreams-reviewer`")
		if developer < 0 || reviewer < 0 {
			t.Error("Semantic Agent Routing is missing the SemStreams developer or reviewer")
		} else if developer >= reviewer {
			t.Error("Semantic Agent Routing must route implementation before review")
		}
	})

	t.Run("shared work protocol is symmetric", func(t *testing.T) {
		agents := markdownSection(t, readProfileFile(t, root, "AGENTS.md"), "## Shared work protocol (Claude and Codex)")
		claude := markdownSection(t, readProfileFile(t, root, "CLAUDE.md"), "## Shared work protocol (Claude and Codex)")
		if agents != claude {
			t.Error("AGENTS.md and CLAUDE.md must have identical Shared work protocol sections")
		}
	})

	t.Run("shared contracts are tracked", func(t *testing.T) {
		body := readProfileFile(t, root, ".gitignore")
		if strings.Contains(body, ".agents/") {
			t.Error(".gitignore must not ignore the shared .agents/ contracts")
		}
	})
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate profile contract test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repository root: %v", err)
	}
	return root
}

func readProfileFile(t *testing.T, root, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func markdownSection(t *testing.T, body, heading string) string {
	t.Helper()
	start := strings.Index(body, heading)
	if start < 0 {
		t.Fatalf("missing Markdown section %q", heading)
	}
	section := body[start:]
	if next := strings.Index(section[len(heading):], "\n## "); next >= 0 {
		section = section[:len(heading)+next]
	}
	return strings.TrimSpace(section)
}

func lineCount(body string) int {
	body = strings.TrimSuffix(body, "\n")
	if body == "" {
		return 0
	}
	return strings.Count(body, "\n") + 1
}

package agentprofiles

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// guidanceMapWordCeiling bounds the always-loaded agent guidance map (gh#1323:
// the previous CLAUDE.md was 3,020 words, 78% of it rule bodies duplicated
// verbatim from files every role loads anyway).
const guidanceMapWordCeiling = 1100

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

	t.Run("guidance map is one file under two names", func(t *testing.T) {
		// CLAUDE.md (Claude Code) and AGENTS.md (Codex) are the always-loaded
		// guidance map. Before gh#1323 they were hand-maintained copies whose
		// agreement was checked section by section; AGENTS.md had silently
		// never carried the purpose or OpenSpec sections. Byte identity
		// subsumes every per-section symmetry check. The word ceiling is a
		// ratchet: the map is paid on every turn of every session, so a rule
		// that needs more than one line belongs in its canonical home.
		agents := readProfileFile(t, root, "AGENTS.md")
		claude := readProfileFile(t, root, "CLAUDE.md")
		if agents != claude {
			t.Fatalf("AGENTS.md and CLAUDE.md differ (%d vs %d bytes); the guidance map is one file under two names: edit one and cp it over the other", len(agents), len(claude))
		}
		if words := len(strings.Fields(claude)); words > guidanceMapWordCeiling {
			t.Errorf("guidance map is %d words, ceiling is %d: move the rule body to its canonical home and leave one line here", words, guidanceMapWordCeiling)
		}

		developer := strings.Index(claude, "`semstreams-developer`")
		reviewer := strings.Index(claude, "`semstreams-reviewer`")
		if developer < 0 || reviewer < 0 {
			t.Error("guidance map is missing the SemStreams developer or reviewer")
		} else if developer >= reviewer {
			t.Error("guidance map must route implementation before review")
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

func lineCount(body string) int {
	body = strings.TrimSuffix(body, "\n")
	if body == "" {
		return 0
	}
	return strings.Count(body, "\n") + 1
}

package contract

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guidanceMapWordCeiling bounds the always-loaded agent guidance map.
//
// The map is paid on every turn of every agent session, so its cost is size ×
// turns, not size. gh#1323 measured the previous CLAUDE.md at 3,020 words, 78%
// of it rule bodies duplicated verbatim from files every role loads anyway, and
// a 100-turn session re-reading roughly a million tokens of guidance before any
// work. The ceiling is a ratchet: a rule that needs more than one line here
// belongs in its canonical home with a pointer, not in the map.
const guidanceMapWordCeiling = 1100

// TestGuidanceMapIsOneFileUnderTwoNames enforces that CLAUDE.md (read by
// Claude Code) and AGENTS.md (read by Codex) are the same bytes.
//
// Before gh#1323 they were hand-maintained copies with no check, and they had
// drifted by omission: AGENTS.md had never carried the framework-not-product
// boundary or the OpenSpec homes, so one of the two agent vendors was working
// without the guidance the other treated as a hard rule. Platform-specific
// command names are mapped in .agents/README.md, so nothing platform-specific
// belongs in either file.
func TestGuidanceMapIsOneFileUnderTwoNames(t *testing.T) {
	root := filepath.Join("..", "..")
	claude := readGuidanceFile(t, filepath.Join(root, "CLAUDE.md"))
	agents := readGuidanceFile(t, filepath.Join(root, "AGENTS.md"))

	if !bytes.Equal(claude, agents) {
		t.Fatalf("CLAUDE.md and AGENTS.md differ (%d vs %d bytes); the guidance map is one file under two names — edit one and `cp` it over the other",
			len(claude), len(agents))
	}

	words := len(strings.Fields(string(claude)))
	if words > guidanceMapWordCeiling {
		t.Fatalf("guidance map is %d words, ceiling is %d: move the rule body to its canonical home and leave one line here",
			words, guidanceMapWordCeiling)
	}
}

func readGuidanceFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

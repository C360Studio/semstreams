package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stripJSONLineComments removes `// …` trailing comments from the README's
// annotated JSON while leaving `//` inside string literals (`nats://…`) alone.
func stripJSONLineComments(body string) string {
	var out strings.Builder
	for _, line := range strings.Split(body, "\n") {
		inString, escaped, cut := false, false, -1
		for i := 0; i < len(line); i++ {
			switch {
			case escaped:
				escaped = false
			case line[i] == '\\':
				escaped = true
			case line[i] == '"':
				inString = !inString
			case !inString && line[i] == '/' && i+1 < len(line) && line[i+1] == '/':
				cut = i
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			line = line[:cut]
		}
		out.WriteString(strings.TrimRight(line, " \t"))
		out.WriteString("\n")
	}
	return out.String()
}

// readmeExample returns the first fenced ```json block of config/README.md with
// its annotations stripped.
func readmeExample(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("README.md")
	require.NoError(t, err)
	_, after, found := strings.Cut(string(body), "```json\n")
	require.True(t, found, "config/README.md no longer opens a ```json example block")
	block, _, found := strings.Cut(after, "\n```")
	require.True(t, found, "the README's first ```json block is unterminated")
	return stripJSONLineComments(block)
}

// TestREADMEPlatformExampleLoads pins the adopter-facing example against the
// production loader. The documented platform block is what an adopter copies
// first, and a field this repo has since removed turns that copy into a
// boot failure they cannot diagnose — `instance_id` was documented here for
// exactly as long as ADR-102 made it a load-time error. Driving the real
// loader means the example cannot drift from the removed-field guard again.
func TestREADMEPlatformExampleLoads(t *testing.T) {
	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(readmeExample(t)), &document),
		"the README example must be valid JSON once its annotations are stripped")

	platform, ok := document["platform"]
	require.True(t, ok, "the README example must document a platform block")

	minimal, err := json.Marshal(map[string]json.RawMessage{"platform": platform})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, minimal, 0o600))

	loader := NewLoader()
	loader.EnableValidation(true)
	cfg, err := loader.LoadFile(path)
	require.NoError(t, err, "the documented platform block must load; an adopter copies it verbatim")
	require.NotEmpty(t, cfg.GetOrg())
	require.NotEmpty(t, cfg.GetPlatform())
}

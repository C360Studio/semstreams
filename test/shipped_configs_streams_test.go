// This file verifies every configuration file this repository
// SHIPS still satisfies the stream-provisioning contract.
//
// The bounds requirement (ordinary streams declare a finite MaxAge, a finite
// MaxBytes and a discard policy, unless declared archival or bridged by an
// expiring override) is a FLAG DAY: it fails readiness at boot, and it applies to
// operator-map streams and to streams derived from a component's JetStream output
// ports alike. A shipped config that misses a field is a deployment that will not
// start, discovered by whoever copies the file rather than by us.
//
// Nothing gated this before. The reference-config test next door covers rule
// predicates only, and the config package's own tests build declarations in Go, so
// no test read the JSON an operator actually starts from. That gap was found in
// review of the change that introduced the requirement — the shipped configs were
// clean, but only because someone audited them by hand once.
//
// This test walks configs/ and puts each file through the SAME entry point boot
// uses (config.ValidateStreamDeclarations), so it cannot pass by re-implementing a
// weaker check. It also reports the exceptions each config declares, because an
// archival stream or a migration override in a shipped file is a decision worth
// seeing in test output rather than a quiet pass.
//
// The files are read and decoded through LoadFromBytes rather than LoadFile: the
// loader's path guard refuses any path resolving outside the working directory, and
// configs/ is a sibling of test/. LoadFromBytes runs the same decode, defaulting and
// merge path — only the file read is ours.
//
// Every config MUST validate. A skip here would make the whole test vacuous, which
// is what its first version did: the path guard rejected all 41 files, every subtest
// skipped, and the run was green.
package referenceconfigs_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
)

// configsDir is repository-relative: this test lives in test/, configs/ is a
// sibling of it.
const configsDir = "../configs"

// skipDirs are trees that are not whole deployment configs. Rule packs and
// prompt/vocabulary fragments are merged INTO a config rather than loaded as one,
// so validating them standalone would test a shape nothing boots.
var skipDirs = map[string]bool{
	"rules":   true,
	"prompts": true,
}

func TestShippedConfigs_SatisfyTheStreamBoundsContract(t *testing.T) {
	paths := shippedConfigPaths(t)
	require.NotEmpty(t, paths, "found no shipped configs to validate — has configs/ moved?")

	for _, path := range paths {
		t.Run(mustRel(t, path), func(t *testing.T) {
			cfg := loadShippedConfig(t, path)

			report, err := config.ValidateStreamDeclarations(cfg)

			// The SAME entry point boot calls. A test that re-derived the rule would
			// pass while production failed.
			require.NoError(t, err,
				"this config would fail readiness at boot: every ordinary stream it declares — in "+
					"config.streams, and derived from any component's jetstream output port — needs an "+
					"explicit max_age, max_bytes and discard, or an archival_streams / "+
					"stream_migration_overrides entry")

			// Exceptions are legitimate and are surfaced rather than swallowed: a
			// shipped file carrying a permanent archival exception or a countdown to a
			// boot failure is a decision, and decisions belong in test output.
			for _, archival := range report.Archival {
				t.Logf("declares ARCHIVAL stream %q (owner %q): %s",
					archival.Stream, archival.Owner, archival.Reason)
			}
			for _, override := range report.MigrationOverrides {
				t.Logf("declares MIGRATION OVERRIDE for %q (owner %q), expires %s: %s",
					override.Stream, override.Owner, override.Expires.Format("2006-01-02"), override.Reason)
			}
		})
	}
}

// TestShippedConfigs_DeclareNoExpiringOverrides keeps a countdown out of the files
// operators copy.
//
// A migration override is a time-limited bridge for a stream that predates the
// bounds requirement. Shipping one means shipping a config that boots today and
// fails on a date — and the operator who copied it has no idea a clock is running.
// An archival declaration is fine (it is permanent by contract and names why); an
// override is not.
func TestShippedConfigs_DeclareNoExpiringOverrides(t *testing.T) {
	for _, path := range shippedConfigPaths(t) {
		rel := mustRel(t, path)
		report, err := config.ValidateStreamDeclarations(loadShippedConfig(t, path))
		if err != nil {
			continue // the bounds failure itself is the test above's finding
		}
		assert.Empty(t, report.MigrationOverrides,
			"%s ships a stream_migration_overrides entry: an operator who copies this file inherits a "+
				"boot failure on the expiry date without knowing a clock is running. Bound the stream, or "+
				"declare it archival if permanence is genuinely its contract", rel)
	}
}

func TestShippedConfigs_ToolStreamCapturesOnlyQueuedToolWork(t *testing.T) {
	wantPaths := []string{
		"agentic.json",
		"examples/research-graph-pipeline.json",
		"flows/crud-tools-test.json",
		"flows/deep-research-test.json",
		"flows/deep-research.json",
		"flows/lesson-example.json",
		"flows/ops-agent-test.json",
		"flows/ops-agent.json",
		"research-graph-e2e.json",
	}

	var gotPaths []string
	for _, path := range shippedConfigPaths(t) {
		tool, ok := loadShippedConfig(t, path).Streams["TOOL"]
		if !ok {
			continue
		}
		rel := mustRel(t, path)
		gotPaths = append(gotPaths, rel)
		t.Run(rel, func(t *testing.T) {
			assert.Equal(t, []string{"tool.execute.>", "tool.result.>"}, tool.Subjects)
			assert.Equal(t, "24h", tool.MaxAge)
			assert.EqualValues(t, 268435456, tool.MaxBytes)
			assert.Equal(t, config.StreamDiscardOld, tool.Discard)
		})
	}
	assert.Equal(t, wantPaths, gotPaths, "the shipped TOOL declaration census changed")
}

func TestShippedResearchConfigs_DeclareModelCapabilitiesOnlyInRegistry(t *testing.T) {
	paths := []string{
		filepath.Join(configsDir, "examples", "research-graph-pipeline.json"),
		filepath.Join(configsDir, "research-graph-e2e.json"),
	}
	components := []string{
		"research-graph-route",
		"research-graph-assess",
		"research-graph-synthesize",
	}

	for _, path := range paths {
		t.Run(mustRel(t, path), func(t *testing.T) {
			cfg := loadShippedConfig(t, path)
			require.NotNil(t, cfg.ModelRegistry, "%s is missing model_registry", path)
			for _, capability := range []string{
				model.CapabilityResearchRouting,
				model.CapabilityResearchAssessment,
				model.CapabilityResearchSynthesis,
			} {
				assert.Contains(t, cfg.ModelRegistry.Capabilities, capability,
					"%s must declare the graph-research capability in model_registry", path)
			}

			data, err := os.ReadFile(path)
			require.NoError(t, err)

			var raw struct {
				Components map[string]struct {
					Config map[string]json.RawMessage `json:"config"`
				} `json:"components"`
			}
			require.NoError(t, json.Unmarshal(data, &raw))
			for _, name := range components {
				componentConfig, ok := raw.Components[name]
				require.True(t, ok, "%s is missing component %s", path, name)
				assert.NotContains(t, componentConfig.Config, "model_capability",
					"%s declares unsupported component-local model_capability; graph research selects its fixed capability from model_registry", name)
			}
		})
	}
}

// loadShippedConfig decodes one shipped config. A failure is a FAILURE, never a
// skip: every file under configs/ that this walk selects is one an operator can
// start from, so "it would not even load" is the strongest possible finding.
func loadShippedConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)

	cfg, err := config.NewLoader().LoadFromBytes(data)
	require.NoError(t, err, "%s does not decode as a configuration", path)
	require.NotNil(t, cfg)
	return cfg
}

func shippedConfigPaths(t *testing.T) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(configsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err, "walking %s", configsDir)
	return paths
}

func mustRel(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(configsDir, path)
	require.NoError(t, err)
	return rel
}

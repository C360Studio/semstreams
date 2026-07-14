package referenceconfigs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/c360studio/semstreams/agentic/research"
	_ "github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	_ "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/processor/rule"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	_ "github.com/c360studio/semstreams/vocabulary/rulepacks"
)

func TestReferenceRuleConfigsUseDeclaredPredicates(t *testing.T) {
	agvocab.Register()
	root := mustFindRepoRoot(t)
	for _, rel := range []string{"configs/rules", "config/rules"} {
		err := filepath.WalkDir(filepath.Join(root, rel), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			definitions := []rule.Definition{}
			if err := json.Unmarshal(data, &definitions); err != nil {
				var definition rule.Definition
				if err := json.Unmarshal(data, &definition); err != nil {
					t.Fatalf("decode %s: %v", path, err)
				}
				definitions = append(definitions, definition)
			}
			for _, definition := range definitions {
				if err := rule.ValidateDefinition(definition); err != nil {
					t.Errorf("%s rule %q: %v", path, definition.ID, err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", rel, err)
		}
	}
}

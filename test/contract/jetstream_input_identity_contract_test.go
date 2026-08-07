package contract_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestShippedJetStreamInputsDeclareBackingStreamAndSubjects(t *testing.T) {
	root := filepath.Join("..", "..", "configs")
	violations := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
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
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		collectJetStreamInputIdentityViolations(document, filepath.ToSlash(path), "$", &violations)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(violations)
	if len(violations) != 0 {
		t.Fatalf("canonical JetStream inputs missing explicit identity:\n%s", strings.Join(violations, "\n"))
	}
}

func TestShippedAgenticModelConfigDoesNotExposeLegacyStreamName(t *testing.T) {
	root := filepath.Join("..", "..", "configs")
	violations := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
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
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		collectAgenticModelLegacyStreamNameViolations(document, filepath.ToSlash(path), "$", &violations)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(violations)
	if len(violations) != 0 {
		t.Fatalf("agentic-model configurations retain legacy top-level stream_name:\n%s", strings.Join(violations, "\n"))
	}
}

func collectJetStreamInputIdentityViolations(value any, path, pointer string, violations *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		if inputs, ok := typed["inputs"].([]any); ok {
			for index, input := range inputs {
				row, ok := input.(map[string]any)
				if !ok {
					continue
				}
				config, ok := row["config"].(map[string]any)
				if !ok || config["kind"] != "jetstream" {
					continue
				}
				rowPointer := fmt.Sprintf("%s/inputs/%d", pointer, index)
				if strings.TrimSpace(stringField(config, "stream_name")) == "" {
					*violations = append(*violations, path+":"+rowPointer+" missing stream_name")
				}
				subjects, ok := config["subjects"].([]any)
				if !ok || len(subjects) == 0 {
					*violations = append(*violations, path+":"+rowPointer+" missing subjects")
					continue
				}
				for subjectIndex, subject := range subjects {
					text, ok := subject.(string)
					if !ok || strings.TrimSpace(text) == "" {
						*violations = append(*violations, fmt.Sprintf("%s:%s/config/subjects/%d empty subject", path, rowPointer, subjectIndex))
					}
				}
			}
		}
		for key, child := range typed {
			collectJetStreamInputIdentityViolations(child, path, pointer+"/"+escapeJSONPointer(key), violations)
		}
	case []any:
		for index, child := range typed {
			collectJetStreamInputIdentityViolations(child, path, fmt.Sprintf("%s/%d", pointer, index), violations)
		}
	}
}

func collectAgenticModelLegacyStreamNameViolations(value any, path, pointer string, violations *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		if typed["name"] == "agentic-model" {
			if config, ok := typed["config"].(map[string]any); ok {
				if _, exists := config["stream_name"]; exists {
					*violations = append(*violations, path+":"+pointer+"/config/stream_name")
				}
			}
		}
		for key, child := range typed {
			collectAgenticModelLegacyStreamNameViolations(child, path, pointer+"/"+escapeJSONPointer(key), violations)
		}
	case []any:
		for index, child := range typed {
			collectAgenticModelLegacyStreamNameViolations(child, path, fmt.Sprintf("%s/%d", pointer, index), violations)
		}
	}
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func escapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

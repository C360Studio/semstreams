package rule

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

func TestRuleTriggerEntityIDContract(t *testing.T) {
	const ruleID = "Rule ID with exact spaces/bytes"
	const packID = "contract-pack"
	const golden = "semstreams.framework.graph.rules.trigger.35a80784ae877b3ed4dd1f35aaa5fdd4bf4905e495af8810ce871be20571b685"

	first, err := ruleTriggerEntityID(packID, ruleID)
	if err != nil {
		t.Fatalf("ruleTriggerEntityID: %v", err)
	}
	second, err := ruleTriggerEntityID(packID, ruleID)
	if err != nil {
		t.Fatalf("repeat ruleTriggerEntityID: %v", err)
	}
	if first != golden {
		t.Fatalf("rule trigger ID = %q, want %q", first, golden)
	}
	if second != first {
		t.Fatalf("repeat ID = %q, want %q", second, first)
	}
	if err := types.ValidateEntityID(first); err != nil {
		t.Fatalf("derived trigger ID is not canonical: %v", err)
	}
	if len(first) != len(ruleTriggerEntityPrefix)+64 || len(first) > types.MaxEntityIDBytes {
		t.Fatalf("derived trigger ID length = %d", len(first))
	}
	if len(first) != 105 {
		t.Fatalf("derived trigger ID length = %d, want 105", len(first))
	}
	if strings.HasPrefix(first, "semstreams.framework.graph.rules.alert.") {
		t.Fatalf("rule trigger identity overlaps the alert namespace: %q", first)
	}

	different, err := ruleTriggerEntityID(packID, ruleID+"!")
	if err != nil {
		t.Fatalf("different ruleTriggerEntityID: %v", err)
	}
	if different == first {
		t.Fatal("distinct exact rule IDs produced the same trigger ID")
	}
	differentPack, err := ruleTriggerEntityID(packID+"-other", ruleID)
	if err != nil {
		t.Fatalf("different pack ruleTriggerEntityID: %v", err)
	}
	if differentPack == first {
		t.Fatal("distinct exact pack IDs produced the same trigger ID")
	}
	unnormalized, err := ruleTriggerEntityID("Contract-Pack", ruleID)
	if err != nil {
		t.Fatalf("unnormalized ruleTriggerEntityID: %v", err)
	}
	if unnormalized == first {
		t.Fatal("pack ID was normalized before hashing")
	}

	veryLong, err := ruleTriggerEntityID(packID, strings.Repeat("x", 4096))
	if err != nil {
		t.Fatalf("long ruleTriggerEntityID: %v", err)
	}
	if len(veryLong) != len(first) {
		t.Fatalf("long rule ID changed bounded output length: %d != %d", len(veryLong), len(first))
	}
	if empty, err := ruleTriggerEntityID(packID, ""); err == nil || empty != "" {
		t.Fatalf("empty rule ID = (%q, %v), want empty plus error", empty, err)
	}
	if empty, err := ruleTriggerEntityID("", ruleID); err == nil || empty != "" {
		t.Fatalf("empty pack ID = (%q, %v), want empty plus error", empty, err)
	}
}

func TestRuleTriggerEntityIDRejectsNonTokenPackID(t *testing.T) {
	t.Parallel()
	if entityID, err := ruleTriggerEntityID("pack.with.separator", "rule"); err == nil || entityID != "" {
		t.Fatalf("dotted pack trigger identity = (%q, %v), want empty ID and error", entityID, err)
	}
}

func TestExecutableRuleConstructionRequiresPackID(t *testing.T) {
	t.Parallel()
	definition := Definition{
		ID: "pack-required", Type: "expression", Name: "Pack required", Enabled: true,
		Conditions: []expression.ConditionExpression{{Field: "test.fixture.value", Operator: "eq", Value: "ok"}},
	}
	if _, err := NewExpressionRule("", definition); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
		t.Fatalf("NewExpressionRule empty pack error = %v, want required error", err)
	}
	if _, err := NewTestRule("", definition.ID, definition.Name, nil, definition.Conditions); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
		t.Fatalf("NewTestRule empty pack error = %v, want required error", err)
	}
	if _, err := NewExpressionRuleFactory().Create(definition.ID, definition, Dependencies{}); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
		t.Fatalf("expression factory empty pack error = %v, want required error", err)
	}
	testDefinition := definition
	testDefinition.Type = "test_rule"
	if _, err := NewTestRuleFactory().Create(testDefinition.ID, testDefinition, Dependencies{}); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
		t.Fatalf("test factory empty pack error = %v, want required error", err)
	}
}

func TestDirectRuleEventProducersUseCanonicalTriggerIdentity(t *testing.T) {
	msg := createTestMessage(map[string]any{"value": "trigger"})
	expressionRule := &ExpressionRule{
		id:            "expression trigger",
		packID:        "expression-contract-pack",
		name:          "Expression Trigger",
		shouldTrigger: true,
	}
	expressionEvents, err := expressionRule.ExecuteEvents([]message.Message{msg})
	if err != nil {
		t.Fatalf("ExpressionRule.ExecuteEvents: %v", err)
	}
	assertCanonicalTriggerEvent(t, expressionEvents, expressionRule.packID, expressionRule.id)

	testRule := &TestRule{
		id:            "test trigger",
		packID:        "test-contract-pack",
		name:          "Test Trigger",
		shouldTrigger: true,
	}
	testEvents, err := testRule.ExecuteEvents([]message.Message{msg})
	if err != nil {
		t.Fatalf("TestRule.ExecuteEvents: %v", err)
	}
	assertCanonicalTriggerEvent(t, testEvents, testRule.packID, testRule.id)
}

func TestDirectRuleEventProducerPropagatesConstructionError(t *testing.T) {
	msg := createTestMessage(map[string]any{"value": "trigger"})
	rule := &ExpressionRule{
		id:            "reserved-metadata",
		packID:        "construction-error-pack",
		name:          "Reserved Metadata",
		shouldTrigger: true,
		metadata:      map[string]any{"entity_id": "must not shadow"},
		// entity-id-audit:classify intentional-malformed "must not shadow" line=154 column=46 surface=go-field:.entity_id entity_id_invalid:arity reserved-envelope collision rejection fixture
	}
	events, err := rule.ExecuteEvents([]message.Message{msg})
	if err == nil || events != nil {
		t.Fatalf("ExecuteEvents() = (%#v, %v), want (nil, error)", events, err)
	}
	if !rule.shouldTrigger {
		t.Fatal("failed construction cleared trigger state")
	}
}

func TestDirectRuleEventProducersRequirePackIdentity(t *testing.T) {
	msg := createTestMessage(map[string]any{"value": "trigger"})
	tests := []struct {
		name string
		rule Rule
	}{
		{"expression", &ExpressionRule{id: "local-rule", name: "Local Rule", shouldTrigger: true}},
		{"test", &TestRule{id: "local-rule", name: "Local Rule", shouldTrigger: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := test.rule.ExecuteEvents([]message.Message{msg})
			if err == nil || events != nil {
				t.Fatalf("ExecuteEvents() = (%#v, %v), want (nil, error)", events, err)
			}
		})
	}
}

func TestRuleFactoriesPlumbPackIdentity(t *testing.T) {
	const packID = "factory-contract-pack"
	def := Definition{
		ID:      "factory-rule",
		Name:    "Factory Rule",
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "value", Operator: "eq", Value: "trigger"},
		},
	}
	expressionRule, err := NewExpressionRuleFactory().Create(def.ID, def, Dependencies{PackID: packID})
	if err != nil {
		t.Fatalf("expression factory Create: %v", err)
	}
	if got := expressionRule.(*ExpressionRule).packID; got != packID {
		t.Fatalf("expression pack ID = %q, want %q", got, packID)
	}
	testRule, err := NewTestRuleFactory().Create(def.ID, def, Dependencies{PackID: packID})
	if err != nil {
		t.Fatalf("test factory Create: %v", err)
	}
	if got := testRule.(*TestRule).packID; got != packID {
		t.Fatalf("test pack ID = %q, want %q", got, packID)
	}
}

func TestReferenceRuleProcessorsDeclareStableUniquePackIDs(t *testing.T) {
	expected := map[string]string{
		"agentic.json":                          "agentic-rules",
		"e2e-structural.json":                   "e2e-structural-rules",
		"lifecycle-flow.json":                   "lifecycle-flow-rules",
		"research-graph-e2e.json":               "research-graph-e2e-rules",
		"semantic.json":                         "semantic-rules",
		"semantic-8b.json":                      "semantic-8b-rules",
		"semantic-frontier.json":                "semantic-frontier-rules",
		"statistical.json":                      "statistical-rules",
		"structural.json":                       "structural-rules",
		"flows/crud-tools-test.json":            "crud-tools-test-rules",
		"flows/deep-research-test.json":         "deep-research-test-rules",
		"flows/deep-research.json":              "deep-research-rules",
		"examples/research-graph-pipeline.json": "research-graph-example-rules",
	}
	found := make(map[string]string)
	used := make(map[string]string)
	configRoot := filepath.Clean(filepath.Join("..", "..", "configs"))
	err := filepath.WalkDir(configRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var document any
		if unmarshalErr := json.Unmarshal(data, &document); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", path, unmarshalErr)
		}
		relative, relativeErr := filepath.Rel(configRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		inspectRuleProcessorConfigs(t, relative, document, found, used)
		return nil
	})
	if err != nil {
		t.Fatalf("walk reference configs: %v", err)
	}
	if len(found) != len(expected) {
		t.Fatalf("rule processor config count = %d, want %d; found=%v", len(found), len(expected), found)
	}
	for path, want := range expected {
		if got := found[path]; got != want {
			t.Errorf("%s pack_id = %q, want %q", path, got, want)
		}
	}
}

func TestGraphEventProducerSourceAudit(t *testing.T) {
	expectedCalls := map[string]int{
		"processor/rule/expression_factory.go": 1,
		"processor/rule/test_rule_factory.go":  1,
	}
	foundCalls := make(map[string]int)
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == ".worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relativeErr := filepath.Rel(repoRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		for _, legacy := range [][]byte{
			[]byte(`fmt.Sprintf("rule.%s.triggered"`),
			[]byte(`EntityID: "test.entity."`),
			[]byte(`EntityID: "alert_`),
		} {
			if bytes.Contains(data, legacy) {
				t.Errorf("%s retains legacy graph-event identity source %q", relative, legacy)
			}
		}

		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, path, data, 0)
		if parseErr != nil {
			return parseErr
		}
		graphAliases := make(map[string]struct{})
		for _, spec := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if importPath != "github.com/c360studio/semstreams/graph" {
				continue
			}
			alias := "graph"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			graphAliases[alias] = struct{}{}
		}
		if len(graphAliases) == 0 {
			return nil
		}

		calls := 0
		safelyAssigned := 0
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CompositeLit:
				if selector, ok := typed.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "Event" {
					if identifier, ok := selector.X.(*ast.Ident); ok {
						if _, graphImport := graphAliases[identifier.Name]; graphImport {
							t.Errorf("%s directly constructs graph.Event; use a fail-closed constructor", relative)
						}
					}
				}
			case *ast.CallExpr:
				if isExportedGraphEventConstructorCall(typed, graphAliases) {
					calls++
				}
			case *ast.AssignStmt:
				if len(typed.Rhs) != 1 || len(typed.Lhs) < 2 {
					break
				}
				call, ok := typed.Rhs[0].(*ast.CallExpr)
				if !ok || !isExportedGraphEventConstructorCall(call, graphAliases) {
					break
				}
				errorTarget, ok := typed.Lhs[1].(*ast.Ident)
				if !ok || errorTarget.Name == "_" {
					t.Errorf("%s drops a graph-event constructor error", relative)
					break
				}
				safelyAssigned++
			}
			return true
		})
		if calls != safelyAssigned {
			t.Errorf("%s graph-event constructor calls=%d safely-assigned=%d", relative, calls, safelyAssigned)
		}
		if calls > 0 {
			foundCalls[relative] = calls
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk graph-event producers: %v", err)
	}
	if !reflect.DeepEqual(foundCalls, expectedCalls) {
		t.Fatalf("graph-event constructor call sites = %v, want %v", foundCalls, expectedCalls)
	}
}

func isExportedGraphEventConstructorCall(call *ast.CallExpr, graphAliases map[string]struct{}) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(selector.Sel.Name, "New") || !strings.HasSuffix(selector.Sel.Name, "Event") {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, graphImport := graphAliases[identifier.Name]
	return graphImport
}

func inspectRuleProcessorConfigs(
	t testing.TB,
	path string,
	value any,
	found, used map[string]string,
) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if typed["name"] == "rule-processor" {
			config, ok := typed["config"].(map[string]any)
			if !ok {
				t.Fatalf("%s rule processor has no config object", path)
			}
			packID, ok := config["pack_id"].(string)
			if !ok || packID == "" {
				t.Fatalf("%s rule processor has no explicit pack_id", path)
			}
			if previous, duplicate := used[packID]; duplicate {
				t.Fatalf("pack_id %q reused by %s and %s", packID, previous, path)
			}
			integration, _ := config["enable_graph_integration"].(bool)
			if err := (Config{PackID: packID, EnableGraphIntegration: integration}).Validate(); err != nil {
				t.Fatalf("%s rule processor pack contract: %v", path, err)
			}
			used[packID] = path
			found[path] = packID
		}
		for _, child := range typed {
			inspectRuleProcessorConfigs(t, path, child, found, used)
		}
	case []any:
		for _, child := range typed {
			inspectRuleProcessorConfigs(t, path, child, found, used)
		}
	}
}

func assertCanonicalTriggerEvent(t testing.TB, events []Event, packID, ruleID string) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event, ok := events[0].(*gtypes.Event)
	if !ok {
		t.Fatalf("event type = %T, want *graph.Event", events[0])
	}
	wantID, err := ruleTriggerEntityID(packID, ruleID)
	if err != nil {
		t.Fatalf("ruleTriggerEntityID: %v", err)
	}
	if event.EntityID != wantID {
		t.Fatalf("event ID = %q, want %q", event.EntityID, wantID)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("direct producer returned invalid event: %v", err)
	}
}

package entityidaudit

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Location identifies one exact occurrence within a grouped audited surface.
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// AuditedSurface groups exact occurrences of one implementation surface.
type AuditedSurface struct {
	File           string     `json:"file"`
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	Classification string     `json:"classification"`
	Locations      []Location `json:"locations"`
}

// SurfaceRule documents one explicit structural inventory rule.
type SurfaceRule struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// SurfaceRules returns the stable manifest governing the surface inventory.
func SurfaceRules() []SurfaceRule {
	return []SurfaceRule{
		{"go-contract-field", "All Go struct fields from the entity ID, subject, reference, pattern, prefix, scope, and gated-DAG fan-out set; every exact group requires a checked disposition"},
		{"parser-validator-api", "All Go declarations and calls whose function name is one of the five public entity-ID parser or validator APIs; every exact group requires a checked disposition"},
		{"schema-contract-field", "All JSON or YAML schema keys from the explicit contract field set, even without an example value; every exact group requires a checked disposition"},
		{"string-builder-candidate", "Every Go function or method returning string whose body structurally concatenates or formats strings; every exact group requires a checked disposition"},
		{"kv-call", "Every Go call to Get, Put, Delete, Watch, Keys, or ListKeysFiltered; every exact group requires a checked disposition"},
		{"direct-split", "Every Go call to strings.Split; every exact group requires a checked disposition"},
		{"match-family-call", "Every Go call whose called function or method name contains match; every exact group requires a checked disposition"},
		{"schema-regex", "Entity-named Go string declarations containing regex metacharacters; inventoried but excluded from declaration-pattern values and every exact group requires a checked disposition"},
	}
}

type surfaceOccurrence struct {
	file   string
	kind   string
	name   string
	line   int
	column int
}

var entityIDAPIs = map[string]bool{
	"ValidateEntityID": true, "ParseEntityID": true, "IsValidEntityID": true,
	"ValidateEntityIDPattern": true, "ValidateEntityIDPrefix": true,
}

func auditSurfaces(files []string) ([]AuditedSurface, error) {
	var occurrences []surfaceOccurrence
	for _, path := range files {
		var found []surfaceOccurrence
		var err error
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			found, err = auditGoSurfaces(path)
		case ".json", ".json5", ".yaml", ".yml":
			found, err = auditConfigSurfaces(path)
		}
		if err != nil {
			return nil, err
		}
		occurrences = append(occurrences, found...)
	}
	return groupSurfaces(occurrences), nil
}

func auditGoSurfaces(path string) ([]surfaceOccurrence, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse Go surface inventory %s: %w", path, err)
	}
	var out []surfaceOccurrence
	add := func(node ast.Node, kind, name string) {
		position := fset.Position(node.Pos())
		out = append(out, surfaceOccurrence{file: path, kind: kind, name: name, line: position.Line, column: position.Column})
	}

	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					structure, ok := value.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							if isContractSurfaceField(name.Name, false) {
								add(name, "go-contract-field", value.Name.Name+"."+name.Name)
							}
						}
					}
				case *ast.ValueSpec:
					for i, name := range value.Names {
						if i >= len(value.Values) {
							continue
						}
						literal, ok := resolveStaticString(value.Values[i], nil, nil, nil)
						if ok && looksLikeRegex(literal) && strings.Contains(strings.ToLower(name.Name), "entity") {
							add(name, "schema-regex", name.Name)
						}
					}
				}
			}
		case *ast.FuncDecl:
			functionName := typed.Name.Name
			if entityIDAPIs[functionName] {
				add(typed.Name, "parser-validator-api-declaration", functionName)
			}
			if returnsString(typed.Type) && buildsString(typed.Body) {
				add(typed.Name, "string-builder-candidate", functionName)
			}
			if typed.Body != nil {
				ast.Inspect(typed.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					callName := expressionName(call.Fun)
					if entityIDAPIs[callName] {
						add(call.Fun, "parser-validator-api-call", callName+" in "+functionName)
					}
					if isStringsSplit(call.Fun) {
						add(call.Fun, "direct-split", "strings.Split in "+functionName)
					}
					if strings.Contains(strings.ToLower(callName), "match") {
						add(call.Fun, "match-family-call", callName+" in "+functionName)
					}
					if isKVMethod(callName) {
						add(call.Fun, "kv-call", callName+" in "+functionName)
					}
					return true
				})
			}
		}
	}
	return out, nil
}

func auditConfigSurfaces(path string) ([]surfaceOccurrence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if ext := strings.ToLower(filepath.Ext(path)); (ext == ".json" || ext == ".json5") && !json.Valid(data) {
		return nil, fmt.Errorf("parse JSON surface inventory %s: invalid JSON", path)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse config surface inventory %s: %w", path, err)
	}
	var out []surfaceOccurrence
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i]
				schema := strings.Contains(filepath.ToSlash(path), "schemas/")
				if isContractSurfaceField(key.Value, schema) {
					kind := "config-contract-field"
					if schema {
						kind = "schema-contract-field"
					}
					out = append(out, surfaceOccurrence{file: path, kind: kind, name: key.Value, line: key.Line, column: key.Column})
				}
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(&root)
	return out, nil
}

func isStringsSplit(function ast.Expr) bool {
	selector, ok := function.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Split" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "strings"
}

func returnsString(functionType *ast.FuncType) bool {
	if functionType.Results == nil {
		return false
	}
	for _, field := range functionType.Results.List {
		if identifier, ok := field.Type.(*ast.Ident); ok && identifier.Name == "string" {
			return true
		}
	}
	return false
}

func buildsString(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BinaryExpr:
			if typed.Op == token.ADD {
				found = true
				return false
			}
		case *ast.CallExpr:
			name := expressionName(typed.Fun)
			if name == "Sprintf" || name == "Join" || name == "String" {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func isContractSurfaceField(name string, schema bool) bool {
	if _, ok := languageForName(name, ""); ok {
		return true
	}
	normalized := normalizeName(name)
	switch normalized {
	case "subject", "reference", "references", "fanoutinstanceid", "scope":
		return true
	case "id", "pattern", "prefix":
		return schema
	default:
		return false
	}
}

func isKVMethod(name string) bool {
	switch name {
	case "Get", "Put", "Delete", "Watch", "Keys", "ListKeysFiltered":
		return true
	default:
		return false
	}
}

func looksLikeRegex(value string) bool {
	return strings.HasPrefix(value, "^") || strings.HasSuffix(value, "$") || strings.ContainsAny(value, "[]()\\")
}

func surfaceKey(file, kind, name string) string {
	return filepath.ToSlash(file) + "|" + kind + "|" + name
}

func groupSurfaces(occurrences []surfaceOccurrence) []AuditedSurface {
	groups := make(map[string]*AuditedSurface)
	for _, occurrence := range occurrences {
		key := surfaceKey(occurrence.file, occurrence.kind, occurrence.name)
		group := groups[key]
		if group == nil {
			group = &AuditedSurface{File: occurrence.file, Kind: occurrence.kind, Name: occurrence.name, Classification: "unreviewed:missing-disposition"}
			groups[key] = group
		}
		group.Locations = append(group.Locations, Location{Line: occurrence.line, Column: occurrence.column})
	}
	out := make([]AuditedSurface, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.Locations, func(i, j int) bool {
			if group.Locations[i].Line != group.Locations[j].Line {
				return group.Locations[i].Line < group.Locations[j].Line
			}
			return group.Locations[i].Column < group.Locations[j].Column
		})
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

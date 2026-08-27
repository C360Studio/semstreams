// Package entityidaudit implements a bounded fixture-hygiene lint over statically identifiable
// entity-ID-shaped source candidates. It does not prove implementation-surface coverage or enforcement.
package entityidaudit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"gopkg.in/yaml.v3"
)

// Language is one of the three entity-ID contract languages or one of the
// explicit source-corpus classifications.
type Language string

const (
	// LanguageLiteral is one complete six-part stored identity.
	LanguageLiteral Language = "literal"
	// LanguageDeclarationPattern is one exact six-position wildcard declaration.
	LanguageDeclarationPattern Language = "declaration-pattern"
	// LanguageQueryPrefix is one non-empty one-to-six-part literal query prefix.
	LanguageQueryPrefix Language = "query-prefix"
	// LanguageUnrelatedGlob marks a glob from another language, such as a NATS subject.
	LanguageUnrelatedGlob Language = "unrelated-glob"
	// LanguageIntentionalMalformed marks a deliberate negative contract fixture.
	LanguageIntentionalMalformed Language = "intentional-malformed"
	// LanguageIntentionalSentinel marks an exact empty value whose surrounding
	// contract explicitly gives it non-entity semantics, such as match-all.
	LanguageIntentionalSentinel Language = "intentional-sentinel"
	// LanguageIntentionalTemplate marks an exact pre-substitution expression
	// whose resolved runtime value must satisfy the entity-ID contract.
	LanguageIntentionalTemplate Language = "intentional-template"
	// LanguageFormatBuilder is a fmt.Sprintf format whose dot-separated tokens
	// are entity-ID positions, with a format verb as a template position
	// (surface go-format-prefix).
	LanguageFormatBuilder Language = "format-builder"
	// LanguagePrefixConstant is a trailing-dot dotted string constant an
	// entity-ID builder concatenates onto (surface go-dotted-constant).
	LanguagePrefixConstant Language = "prefix-constant"

	maxAnnotationsPerFile    = 100
	maxAnnotationLineBytes   = 512
	maxAnnotationReasonBytes = 160
)

// Candidate records one structurally extracted entity-ID value.
type Candidate struct {
	File                 string   `json:"file"`
	Line                 int      `json:"line"`
	Column               int      `json:"column"`
	Language             Language `json:"language"`
	Value                string   `json:"value"`
	Surface              string   `json:"surface"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason,omitempty"`
	Classification       Language `json:"classification,omitempty"`
	ClassificationReason string   `json:"classification_reason,omitempty"`
}

// Finding is an invalid candidate without an explicit source classification.
type Finding struct {
	Candidate
	Reason string `json:"reason"`
}

var (
	annotationRE          = regexp.MustCompile(`entity-id-audit:classify[[:space:]]+(intentional-malformed|intentional-sentinel|intentional-template|unrelated-glob)[[:space:]]+("(?:[^"\\]|\\.)*")[[:space:]]+line=([0-9]+)[[:space:]]+column=([0-9]+)[[:space:]]+surface=([^[:space:]]+)[[:space:]]+(.+)$`)
	intentionalTemplateRE = regexp.MustCompile(`^\$(?:entity|related|state|schedule|caller|message)\.[a-z0-9][a-z0-9_.-]*$`)
	structuredPatterns    = []struct {
		language Language
		re       *regexp.Regexp
	}{
		{LanguageDeclarationPattern, regexp.MustCompile(`(?i)(?:entity[_-]?id[_-]?pattern|target[_-]?pattern)[[:space:]]*(?::|=)[[:space:]]*["'\x60]([^"'\x60]+)["'\x60]`)},
		{LanguageQueryPrefix, regexp.MustCompile(`(?i)(?:entity[_-]?id[_-]?prefix|entity[_-]?prefix|id[_-]?prefix)[[:space:]]*(?::|=)[[:space:]]*["'\x60]([^"'\x60]+)["'\x60]`)},
		{LanguageLiteral, regexp.MustCompile(`(?i)(?:entity[_-]?id|owner[_-]?entity[_-]?id|target[_-]?entity[_-]?id)[[:space:]]*(?::|=)[[:space:]]*["'\x60]([^"'\x60]+)["'\x60]`)},
	}
)

type annotation struct {
	language        Language
	value           string
	reason          string
	declarationLine int
	targetLine      int
	targetColumn    int
	targetSurface   string
}

// Audit walks roots, returns a stable source corpus, and reports every
// invalid value that is not deliberately classified in its source file.
func Audit(roots ...string) ([]Candidate, []Finding, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	roots = append([]string(nil), roots...)
	sort.Strings(roots)

	files, err := walkSourceFiles(roots)
	if err != nil {
		return nil, nil, err
	}
	return auditFiles(files)
}

func walkSourceFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && ignoredPath(path, entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if supportedExtension(strings.ToLower(filepath.Ext(path))) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func auditFiles(files []string) ([]Candidate, []Finding, error) {
	symbols, err := collectGoSymbols(files)
	if err != nil {
		return nil, nil, err
	}
	var candidates []Candidate
	for _, path := range files {
		ext := strings.ToLower(filepath.Ext(path))
		annotations, err := loadAnnotations(path)
		if err != nil {
			return nil, nil, err
		}
		var extracted []Candidate
		switch ext {
		case ".go":
			extracted, err = auditGo(path, symbols)
		case ".json", ".json5":
			extracted, err = auditJSON(path)
		case ".jsonl", ".ndjson":
			extracted, err = auditJSONLines(path)
		case ".yaml", ".yml":
			extracted, err = auditYAML(path)
		case ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".svelte", ".toml", ".graphql", ".gql", ".proto", ".cue":
			extracted, err = auditStructuredText(path)
		}
		if err != nil {
			return nil, nil, err
		}
		classified, err := applyAnnotations(path, extracted, annotations)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, classified...)
	}

	registered, err := collectRegisteredDomains(files, symbols)
	if err != nil {
		return nil, nil, err
	}
	candidates = deduplicate(candidates)
	findings := make([]Finding, 0)
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Classification != "" {
			candidate.Status = "classified"
			continue
		}
		if err := validate(candidate.Language, candidate.Value); err != nil {
			candidate.Status = "finding"
			candidate.Reason = validationReason(err)
			findings = append(findings, Finding{Candidate: *candidate, Reason: candidate.Reason})
			continue
		}
		if reason := segmentFinding(*candidate, registered); reason != "" {
			candidate.Status = "finding"
			candidate.Reason = reason
			findings = append(findings, Finding{Candidate: *candidate, Reason: reason})
			continue
		}
		candidate.Status = "valid"
	}
	return candidates, findings, nil
}

func validationReason(err error) string {
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		return err.Error()
	}
	reason, _ := classified.Detail[semtypes.EntityIDDetailReason].(string)
	if reason == "" {
		return classified.Code
	}
	return classified.Code + ":" + reason
}

func supportedExtension(ext string) bool {
	switch ext {
	case ".go", ".json", ".json5", ".jsonl", ".ndjson", ".yaml", ".yml", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".svelte", ".toml", ".graphql", ".gql", ".proto", ".cue":
		return true
	default:
		return false
	}
}

func validate(language Language, value string) error {
	switch language {
	case LanguageLiteral:
		return semtypes.ValidateEntityID(value)
	case LanguageDeclarationPattern:
		return semtypes.ValidateEntityIDPattern(value)
	case LanguageQueryPrefix:
		return semtypes.ValidateEntityIDPrefix(value)
	case LanguageFormatBuilder:
		return validateFormatBuilder(value)
	case LanguagePrefixConstant:
		return validatePrefixConstant(value)
	default:
		return nil
	}
}

type goSymbols struct {
	byPackage map[string]map[string]string
	byFile    map[string]map[string]string
}

func collectGoSymbols(files []string) (*goSymbols, error) {
	table := &goSymbols{byPackage: make(map[string]map[string]string), byFile: make(map[string]map[string]string)}
	conflicts := make(map[string]map[string]bool)
	for _, path := range files {
		if filepath.Ext(path) != ".go" {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse Go %s: %w", path, err)
		}
		pkg := file.Name.Name
		table.byFile[path] = make(map[string]string)
		if table.byPackage[pkg] == nil {
			table.byPackage[pkg] = make(map[string]string)
			conflicts[pkg] = make(map[string]bool)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, item := range gen.Specs {
				values := item.(*ast.ValueSpec)
				for i, name := range values.Names {
					if i >= len(values.Values) {
						continue
					}
					value, ok := resolveStaticString(values.Values[i], table.byFile[path], table.byPackage[pkg], nil)
					if !ok {
						continue
					}
					if old, exists := table.byPackage[pkg][name.Name]; exists && old != value {
						conflicts[pkg][name.Name] = true
						delete(table.byPackage[pkg], name.Name)
					} else if !conflicts[pkg][name.Name] {
						table.byPackage[pkg][name.Name] = value
					}
					table.byFile[path][name.Name] = value
				}
			}
		}
	}
	return table, nil
}

func ignoredPath(path, name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build":
		return true
	}
	clean := filepath.ToSlash(path)
	return strings.Contains(clean, "openspec/changes/archive/") || strings.HasSuffix(clean, "openspec/changes/archive")
}

func auditGo(path string, symbols *goSymbols) ([]Candidate, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go %s: %w", path, err)
	}
	semanticTestAliases, err := semanticTestImportAliases(file, path)
	if err != nil {
		return nil, err
	}
	imports := make(map[string]string)
	for _, spec := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		alias := filepath.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "_" && alias != "." {
			imports[alias] = filepath.Base(importPath)
		}
	}
	resolve := func(expr ast.Expr) (string, bool) {
		return resolveStaticString(expr, symbols.byFile[path], symbols.byPackage[file.Name.Name], func(pkg, name string) (string, bool) {
			if imported, ok := imports[pkg]; ok {
				pkg = imported
			}
			value, ok := symbols.byPackage[pkg][name]
			return value, ok
		})
	}
	var out []Candidate
	add := func(expr ast.Expr, language Language, surface string) {
		value, ok := resolve(expr)
		if !ok {
			return
		}
		position := fset.Position(expr.Pos())
		out = append(out, Candidate{File: path, Line: position.Line, Column: position.Column, Language: language, Value: value, Surface: surface})
	}

	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.Name == "EntityID" && function.Body != nil {
			ast.Inspect(function.Body, func(node ast.Node) bool {
				returned, ok := node.(*ast.ReturnStmt)
				if !ok || len(returned.Results) != 1 {
					return true
				}
				add(returned.Results[0], LanguageLiteral, "go-return:EntityID")
				return false
			})
		}
		if gen, ok := decl.(*ast.GenDecl); ok && (gen.Tok == token.CONST || gen.Tok == token.VAR) {
			for _, item := range gen.Specs {
				values := item.(*ast.ValueSpec)
				for i, name := range values.Names {
					if i < len(values.Values) && !strings.Contains(filepath.ToSlash(path), "vocabulary/") &&
						!excludedDeclarationName(name.Name) && !isEntityIDSchemaRegexDeclaration(path, name.Name) {
						if language, ok := languageForName(name.Name, ""); ok {
							add(values.Values[i], language, "go-declaration:"+name.Name)
						} else if value, ok := resolve(values.Values[i]); ok && isDottedPrefixConstant(name.Name, value) {
							position := fset.Position(values.Values[i].Pos())
							out = append(out, Candidate{
								File: path, Line: position.Line, Column: position.Column, Language: LanguagePrefixConstant,
								Value: value, Surface: "go-dotted-constant:" + name.Name,
							})
						}
					}
				}
			}
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CompositeLit:
			typeName := expressionName(n.Type)
			fields := make(map[string]ast.Expr)
			for _, element := range n.Elts {
				if keyed, ok := element.(*ast.KeyValueExpr); ok {
					switch key := keyed.Key.(type) {
					case *ast.Ident:
						fields[key.Name] = keyed.Value
					case *ast.BasicLit:
						if key.Kind == token.STRING {
							if name, err := strconv.Unquote(key.Value); err == nil {
								fields[name] = keyed.Value
							}
						}
					}
				}
			}
			for field, expr := range fields {
				if language, ok := languageForName(field, typeName); ok {
					add(expr, language, "go-field:"+typeName+"."+field)
				}
			}
			if typeName == "EntityID" {
				if value, ok := entityIDConstructorValue(fields, resolve); ok {
					out = append(out, Candidate{
						File: path, Line: fset.Position(n.Pos()).Line, Column: fset.Position(n.Pos()).Column, Language: LanguageLiteral,
						Value: value, Surface: "go-constructor:EntityID",
					})
				}
			}
			if isTripleType(typeName) {
				if expr, ok := fields["Subject"]; ok {
					add(expr, LanguageLiteral, "go-triple-subject")
				}
				if object, ok := fields["Object"]; ok && typedEntityReference(fields, resolve) {
					add(object, LanguageLiteral, "go-triple-reference")
				}
			}
		case *ast.CallExpr:
			name := expressionName(n.Fun)
			if isSemanticTestCall(n.Fun, file.Name.Name, path, semanticTestAliases, "EntityID") && len(n.Args) == 7 {
				parts := make([]string, 0, 6)
				resolved := true
				for _, argument := range n.Args[1:] {
					part, ok := resolve(argument)
					if !ok {
						resolved = false
						break
					}
					parts = append(parts, part)
				}
				if resolved {
					position := fset.Position(n.Pos())
					out = append(out, Candidate{
						File: path, Line: position.Line, Column: position.Column, Language: LanguageLiteral,
						Value: strings.Join(parts, "."), Surface: "go-call:semantictest.EntityID",
					})
				}
			}
			if name == "Sprintf" && len(n.Args) > 0 {
				if format, ok := n.Args[0].(*ast.BasicLit); ok && format.Kind == token.STRING {
					if value, err := strconv.Unquote(format.Value); err == nil && formatBuilderTokens(value) != nil {
						position := fset.Position(format.Pos())
						out = append(out, Candidate{
							File: path, Line: position.Line, Column: position.Column, Language: LanguageFormatBuilder,
							Value: value, Surface: "go-format-prefix:" + enclosingFunctionName(file, n.Pos()),
						})
					}
				}
			}
			if len(n.Args) > 0 {
				switch name {
				case "ValidateEntityID", "ParseEntityID", "IsValidEntityID":
					add(n.Args[0], LanguageLiteral, "go-call:"+name)
				case "ValidateEntityIDPattern":
					add(n.Args[0], LanguageDeclarationPattern, "go-call:"+name)
				case "ValidateEntityIDPrefix":
					add(n.Args[0], LanguageQueryPrefix, "go-call:"+name)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if i >= len(n.Rhs) {
					continue
				}
				if ident, ok := lhs.(*ast.Ident); ok {
					if language, classified := languageForName(ident.Name, ""); classified {
						add(n.Rhs[i], language, "go-assignment:"+ident.Name)
					}
				}
			}
		}
		return true
	})
	return out, nil
}

func isSemanticTestCall(
	function ast.Expr,
	packageName string,
	path string,
	aliases map[string]bool,
	helper string,
) bool {
	switch typed := function.(type) {
	case *ast.SelectorExpr:
		identifier, ok := typed.X.(*ast.Ident)
		return ok && aliases[identifier.Name] && typed.Sel.Name == helper
	case *ast.Ident:
		return typed.Name == helper && packageName == "semantictest" &&
			isSemanticTestPackagePath(path)
	default:
		return false
	}
}

func isSemanticTestPackagePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "internal/semantictest/") ||
		strings.Contains(clean, "/internal/semantictest/")
}

func semanticTestImportAliases(file *ast.File, path string) (map[string]bool, error) {
	aliases := make(map[string]bool)
	if file.Name.Name == "semantictest" && isSemanticTestPackagePath(path) &&
		(goFileDeclaresIdentifier(file, "EntityID", true) || goFileImportDeclaresIdentifier(file, "EntityID")) {
		return nil, fmt.Errorf(
			"%s: declaration shadows the package semantictest.EntityID helper",
			path,
		)
	}
	importsSemanticTest := false
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != "github.com/c360studio/semstreams/internal/semantictest" {
			continue
		}
		importsSemanticTest = true
		if spec.Name != nil {
			return nil, fmt.Errorf(
				"%s: internal/semantictest must use its canonical unaliased import name",
				path,
			)
		}
		aliases["semantictest"] = true
	}
	if importsSemanticTest && goFileDeclaresIdentifier(file, "semantictest", false) {
		return nil, fmt.Errorf(
			"%s: declaration shadows the canonical internal/semantictest import name",
			path,
		)
	}
	return aliases, nil
}

func goFileDeclaresIdentifier(file *ast.File, wanted string, allowPackageFunction bool) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		switch typed := node.(type) {
		case *ast.ValueSpec:
			found = identifiersContain(typed.Names, wanted)
		case *ast.TypeSpec:
			found = typed.Name.Name == wanted
		case *ast.FuncDecl:
			declaresFunctionName := typed.Name.Name == wanted
			if allowPackageFunction && typed.Recv == nil {
				declaresFunctionName = false
			}
			found = declaresFunctionName ||
				fieldListDeclares(typed.Recv, wanted) ||
				fieldListDeclares(typed.Type.Params, wanted) ||
				fieldListDeclares(typed.Type.Results, wanted)
		case *ast.FuncLit:
			found = fieldListDeclares(typed.Type.Params, wanted) ||
				fieldListDeclares(typed.Type.Results, wanted)
		case *ast.AssignStmt:
			if typed.Tok == token.DEFINE {
				found = expressionsDeclare(typed.Lhs, wanted)
			}
		case *ast.RangeStmt:
			if typed.Tok == token.DEFINE {
				found = expressionDeclares(typed.Key, wanted) || expressionDeclares(typed.Value, wanted)
			}
		}
		return !found
	})
	return found
}

func goFileImportDeclaresIdentifier(file *ast.File, wanted string) bool {
	for _, spec := range file.Imports {
		if spec.Name != nil {
			if spec.Name.Name == wanted {
				return true
			}
			continue
		}
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err == nil && filepath.Base(importPath) == wanted {
			return true
		}
	}
	return false
}

func fieldListDeclares(fields *ast.FieldList, wanted string) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if identifiersContain(field.Names, wanted) {
			return true
		}
	}
	return false
}

func identifiersContain(identifiers []*ast.Ident, wanted string) bool {
	for _, identifier := range identifiers {
		if identifier.Name == wanted {
			return true
		}
	}
	return false
}

func expressionsDeclare(expressions []ast.Expr, wanted string) bool {
	for _, expression := range expressions {
		if expressionDeclares(expression, wanted) {
			return true
		}
	}
	return false
}

func expressionDeclares(expression ast.Expr, wanted string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == wanted
}

func excludedDeclarationName(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range []string{"predicate", "metadata", "error", "reason", "detail"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isEntityIDSchemaRegexDeclaration(path, name string) bool {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	if cleanPath != "pkg/types/entity_id.go" && !strings.HasSuffix(cleanPath, "/pkg/types/entity_id.go") {
		return false
	}
	switch name {
	case "entityIDLiteralSegmentPattern",
		"entityIDLiteralBodyPattern",
		"entityIDPatternSegmentPattern",
		"entityIDPatternBodyPattern",
		"EntityIDLiteralPattern",
		"EntityIDDeclarationPattern",
		"EntityIDLiteralPrefixPattern",
		"OptionalEntityIDLiteralPattern":
		return true
	default:
		return false
	}
}

func resolveStaticString(expr ast.Expr, fileSymbols, packageSymbols map[string]string, selector func(string, string) (string, bool)) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := fileSymbols[typed.Name]
		if !ok {
			value, ok = packageSymbols[typed.Name]
		}
		return value, ok
	case *ast.SelectorExpr:
		pkg, ok := typed.X.(*ast.Ident)
		if !ok || selector == nil {
			return "", false
		}
		return selector(pkg.Name, typed.Sel.Name)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return "", false
		}
		left, leftOK := resolveStaticString(typed.X, fileSymbols, packageSymbols, selector)
		right, rightOK := resolveStaticString(typed.Y, fileSymbols, packageSymbols, selector)
		return left + right, leftOK && rightOK
	default:
		return "", false
	}
}

func expressionName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.IndexExpr:
		return expressionName(typed.X)
	case *ast.IndexListExpr:
		return expressionName(typed.X)
	default:
		return ""
	}
}

func languageForName(name, container string) (Language, bool) {
	normalized := normalizeName(name)
	container = strings.ToLower(container)
	switch {
	case normalized == "entityidpattern" || strings.HasSuffix(normalized, "entityidpattern") || normalized == "targetpattern" || normalized == "ownerpattern":
		return LanguageDeclarationPattern, true
	case normalized == "entityidprefix" || strings.HasSuffix(normalized, "entityidprefix") || normalized == "entityprefix" || normalized == "idprefix":
		return LanguageQueryPrefix, true
	case normalized == "prefix" && (strings.Contains(container, "query") || strings.Contains(container, "hierarchy") || strings.Contains(container, "entity")):
		return LanguageQueryPrefix, true
	case normalized == "entityid" || normalized == "entityidf" || strings.HasSuffix(normalized, "entityid") || strings.HasSuffix(normalized, "entityids"):
		return LanguageLiteral, true
	case normalized == "id" && (strings.Contains(container, "entitystate") || strings.Contains(container, "entitymutation")):
		return LanguageLiteral, true
	default:
		return "", false
	}
}

func normalizeName(value string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(value))
}

func isTripleType(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), "triple")
}

func entityIDConstructorValue(fields map[string]ast.Expr, resolve func(ast.Expr) (string, bool)) (string, bool) {
	parts := make([]string, 0, 6)
	// Canonical order org.platform.system.domain.type.instance (ADR-102).
	for _, name := range []string{"Org", "Platform", "System", "Domain", "Type", "Instance"} {
		expr, ok := fields[name]
		if !ok {
			return "", false
		}
		value, ok := resolve(expr)
		if !ok {
			return "", false
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "."), true
}

func typedEntityReference(fields map[string]ast.Expr, resolve func(ast.Expr) (string, bool)) bool {
	for _, name := range []string{"ObjectType", "Datatype"} {
		if expr, ok := fields[name]; ok {
			value, resolved := resolve(expr)
			if resolved && value == "@id" {
				return true
			}
		}
	}
	return false
}

func auditJSON(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("parse JSON %s: invalid JSON; JSON5 and malformed JSON do not use text fallback", path)
	}
	return auditConfigDocument(path, data, 0)
}

func auditJSONLines(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	for scanner.Scan() {
		line++
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !json.Valid(scanner.Bytes()) {
			return nil, fmt.Errorf("parse JSONL %s:%d: invalid JSON", path, line)
		}
		candidates, err := auditConfigDocument(path, scanner.Bytes(), line-1)
		if err != nil {
			return nil, err
		}
		out = append(out, candidates...)
	}
	return out, scanner.Err()
}

func auditYAML(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse YAML %s: %w", path, err)
	}
	return walkConfigNode(path, &root, 0), nil
}

func auditConfigDocument(path string, data []byte, lineOffset int) ([]Candidate, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse config AST %s:%d: %w", path, lineOffset+1, err)
	}
	return walkConfigNode(path, &root, lineOffset), nil
}

func walkConfigNode(path string, root *yaml.Node, lineOffset int) []Candidate {
	var out []Candidate
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.DocumentNode {
			for _, child := range node.Content {
				walk(child)
			}
			return
		}
		if node.Kind == yaml.MappingNode {
			fields := make(map[string]*yaml.Node, len(node.Content)/2)
			for i := 0; i+1 < len(node.Content); i += 2 {
				fields[node.Content[i].Value] = node.Content[i+1]
			}
			addValue := func(key string, value *yaml.Node, language Language, surface string) {
				for _, scalar := range scalarStringNodes(value) {
					out = append(out, Candidate{
						File: path, Line: scalar.Line + lineOffset, Column: scalar.Column,
						Language: language, Value: scalar.Value, Surface: surface + key,
					})
				}
			}
			addOccurrences := func(key string, language Language, surface string) {
				for i := 0; i+1 < len(node.Content); i += 2 {
					if node.Content[i].Value == key {
						addValue(key, node.Content[i+1], language, surface)
					}
				}
			}
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i].Value
				normalized := normalizeName(key)
				switch {
				case normalized == "entityidpattern" || normalized == "targetpattern" || normalized == "ownerpattern":
					addValue(key, node.Content[i+1], LanguageDeclarationPattern, "config:")
				case normalized == "entityidprefix" || normalized == "entityprefix" || normalized == "idprefix":
					addValue(key, node.Content[i+1], LanguageQueryPrefix, "config:")
				case normalized == "entityid" || strings.HasSuffix(normalized, "entityid"):
					addValue(key, node.Content[i+1], LanguageLiteral, "config:")
				}
			}
			if _, ok := fields["triples"]; ok {
				addOccurrences("id", LanguageLiteral, "config:entity-state-")
			}
			if entity, ok := fields["entity"]; ok && entity.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(entity.Content); i += 2 {
					if entity.Content[i].Value == "pattern" {
						addValue("entity.pattern", entity.Content[i+1], LanguageDeclarationPattern, "config:")
					}
				}
			}
			if buckets, ok := fields["entity_watch_buckets"]; ok && buckets.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(buckets.Content); i += 2 {
					if buckets.Content[i].Value == graph.BucketEntityStates {
						addValue("entity_watch_buckets."+graph.BucketEntityStates, buckets.Content[i+1], LanguageDeclarationPattern, "config:")
					}
				}
			}
			if _, predicate := fields["predicate"]; predicate {
				if _, object := fields["object"]; object {
					addOccurrences("subject", LanguageLiteral, "config:triple-")
					if configNodeEntityReference(fields) {
						addOccurrences("object", LanguageLiteral, "config:triple-reference-")
					}
				}
			}
			if queryType := strings.ToLower(nodeScalar(fields["type"])); strings.Contains(queryType, "prefix") || strings.Contains(queryType, "hierarchy") {
				addOccurrences("prefix", LanguageQueryPrefix, "config:query-")
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(root)
	return out
}

func configNodeEntityReference(object map[string]*yaml.Node) bool {
	for _, key := range []string{"object_type", "objectType", "datatype"} {
		if nodeScalar(object[key]) == "@id" {
			return true
		}
	}
	return false
}

func scalarStringNodes(node *yaml.Node) []*yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && node.Value != "" && !strings.HasPrefix(node.Value, "$") {
		return []*yaml.Node{node}
	}
	if node.Kind == yaml.SequenceNode {
		var out []*yaml.Node
		for _, child := range node.Content {
			out = append(out, scalarStringNodes(child)...)
		}
		return out
	}
	return nil
}

func nodeScalar(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func auditStructuredText(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		for _, pattern := range structuredPatterns {
			for _, location := range pattern.re.FindAllStringSubmatchIndex(text, -1) {
				out = append(out, Candidate{
					File: path, Line: line, Column: location[2] + 1, Language: pattern.language,
					Value: text[location[2]:location[3]], Surface: "structured-text",
				})
			}
		}
	}
	return out, scanner.Err()
}

func loadAnnotations(path string) ([]annotation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if filepath.Ext(path) == ".go" {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse Go annotations %s: %w", path, err)
		}
		var annotations []annotation
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !strings.Contains(comment.Text, "entity-id-audit:classify") {
					continue
				}
				item, err := parseAnnotation(path, fset.Position(comment.Pos()).Line, comment.Text)
				if err != nil {
					return nil, err
				}
				annotations = append(annotations, item)
				if len(annotations) > maxAnnotationsPerFile {
					return nil, fmt.Errorf("%s: more than %d entity ID audit annotations", path, maxAnnotationsPerFile)
				}
			}
		}
		return annotations, validateAnnotationSet(path, annotations)
	}
	var annotations []annotation
	for index, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "entity-id-audit:classify") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") &&
			!strings.HasPrefix(trimmed, "/*") && !strings.HasPrefix(trimmed, "*") &&
			!strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		item, err := parseAnnotation(path, index+1, line)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, item)
		if len(annotations) > maxAnnotationsPerFile {
			return nil, fmt.Errorf("%s: more than %d entity ID audit annotations", path, maxAnnotationsPerFile)
		}
	}
	return annotations, validateAnnotationSet(path, annotations)
}

func validateAnnotationSet(path string, annotations []annotation) error {
	seen := make(map[string]int, len(annotations))
	for _, item := range annotations {
		key := fmt.Sprintf("%d:%d:%s:%s", item.targetLine, item.targetColumn, item.targetSurface, item.value)
		if line, exists := seen[key]; exists {
			return fmt.Errorf("%s:%d: duplicate entity ID audit annotation for %q at %d:%d/%s (first declared at line %d)", path, item.declarationLine, item.value, item.targetLine, item.targetColumn, item.targetSurface, line)
		}
		seen[key] = item.declarationLine
	}
	return nil
}

func parseAnnotation(path string, line int, text string) (annotation, error) {
	if len(text) > maxAnnotationLineBytes {
		return annotation{}, fmt.Errorf("%s:%d: entity ID audit annotation exceeds %d bytes", path, line, maxAnnotationLineBytes)
	}
	match := annotationRE.FindStringSubmatch(text)
	if match == nil {
		return annotation{}, fmt.Errorf("%s:%d: malformed entity ID audit annotation", path, line)
	}
	value, err := strconv.Unquote(match[2])
	if err != nil {
		return annotation{}, fmt.Errorf("%s:%d: unquote entity ID audit annotation: %w", path, line, err)
	}
	targetLine, err := strconv.Atoi(match[3])
	if err != nil || targetLine < 1 {
		return annotation{}, fmt.Errorf("%s:%d: invalid entity ID audit target line", path, line)
	}
	targetColumn, err := strconv.Atoi(match[4])
	if err != nil || targetColumn < 1 {
		return annotation{}, fmt.Errorf("%s:%d: invalid entity ID audit target column", path, line)
	}
	targetSurface := match[5]
	reason := strings.TrimSpace(match[6])
	reason = strings.TrimSuffix(reason, "*/")
	reason = strings.TrimSuffix(reason, "-->")
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > maxAnnotationReasonBytes {
		return annotation{}, fmt.Errorf("%s:%d: entity ID audit annotation reason must be 1-%d bytes", path, line, maxAnnotationReasonBytes)
	}
	return annotation{
		language: Language(match[1]), value: value, reason: reason, declarationLine: line,
		targetLine: targetLine, targetColumn: targetColumn, targetSurface: targetSurface,
	}, nil
}

func applyAnnotations(path string, extracted []Candidate, annotations []annotation) ([]Candidate, error) {
	for _, item := range annotations {
		matched := -1
		for i := range extracted {
			if extracted[i].Line == item.targetLine && extracted[i].Column == item.targetColumn && extracted[i].Surface == item.targetSurface && extracted[i].Value == item.value {
				if matched != -1 {
					return nil, fmt.Errorf("%s:%d: entity ID audit annotation matches multiple candidates at line=%d column=%d surface=%s value=%q", path, item.declarationLine, item.targetLine, item.targetColumn, item.targetSurface, item.value)
				}
				matched = i
			}
		}
		if matched == -1 {
			return nil, fmt.Errorf("%s:%d: entity ID audit annotation does not match target line=%d column=%d surface=%s value=%q", path, item.declarationLine, item.targetLine, item.targetColumn, item.targetSurface, item.value)
		}

		candidate := &extracted[matched]
		validationErr := validate(candidate.Language, candidate.Value)
		if validationErr == nil {
			return nil, fmt.Errorf("%s:%d: entity ID audit classification %s targets valid %s value %q", path, item.declarationLine, item.language, candidate.Language, candidate.Value)
		}
		wantReason := validationReason(validationErr)
		declaredReason := strings.Fields(item.reason)[0]
		if declaredReason != wantReason {
			return nil, fmt.Errorf("%s:%d: entity ID audit classification reason %q does not match authoritative reason %q", path, item.declarationLine, declaredReason, wantReason)
		}
		if item.language == LanguageIntentionalSentinel && candidate.Value != "" {
			return nil, fmt.Errorf("%s:%d: intentional-sentinel classification requires an empty value", path, item.declarationLine)
		}
		if item.language == LanguageIntentionalTemplate && !intentionalTemplateRE.MatchString(candidate.Value) {
			return nil, fmt.Errorf("%s:%d: intentional-template classification requires one recognized full substitution token", path, item.declarationLine)
		}
		candidate.Classification = item.language
		candidate.ClassificationReason = item.reason
	}
	return extracted, nil
}

func deduplicate(in []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(in))
	out := make([]Candidate, 0, len(in))
	for _, candidate := range in {
		key := fmt.Sprintf("%s:%d:%d:%s:%s:%s", candidate.File, candidate.Line, candidate.Column, candidate.Language, candidate.Value, candidate.Surface)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Column != out[j].Column {
			return out[i].Column < out[j].Column
		}
		if out[i].Language != out[j].Language {
			return out[i].Language < out[j].Language
		}
		if out[i].Value != out[j].Value {
			return out[i].Value < out[j].Value
		}
		return out[i].Surface < out[j].Surface
	})
	return out
}

// enclosingFunctionName names the function or method whose body contains pos,
// so a format-builder surface reads like the builder it belongs to.
func enclosingFunctionName(file *ast.File, pos token.Pos) string {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil || pos < function.Pos() || pos > function.End() {
			continue
		}
		return function.Name.Name
	}
	return "fmt.Sprintf"
}

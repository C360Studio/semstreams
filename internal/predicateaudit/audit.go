// Package predicateaudit extracts predicate identities from owned source and
// configuration artifacts and validates them against the canonical grammar.
package predicateaudit

import (
	"bufio"
	"encoding/json"
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

	"github.com/c360studio/semstreams/vocabulary"
	"gopkg.in/yaml.v3"
)

// Candidate records one structurally extracted predicate use.
type Candidate struct {
	File      string
	Line      int
	Column    int
	Predicate string
	Surface   string
}

// Finding is an invalid, unclassified candidate.
type Finding struct {
	Candidate
	Reason string
}

var (
	substitutionRE = regexp.MustCompile(`\$(?:entity|related)\.triple\.([A-Za-z0-9_.-]+)`)
	structuredRE   = regexp.MustCompile(
		`(?i)(?:predicate|predicates|phasePredicate|linkPredicate|referencePredicates|triplePredicate)` +
			`["']?[[:space:]]*(?::|=)[[:space:]]*["'\x60]([^"'\x60]*)["'\x60]`)
	allowRE        = regexp.MustCompile(`predicate-audit:allow-invalid[[:space:]]+([^[:space:]]+)[[:space:]]+(.+)$`)
	lifecycleTagRE = regexp.MustCompile(`(?:^|[,[:space:]])predicate=([^,"[:space:]]+)`)
)

// Audit walks roots and returns every extracted candidate plus invalid
// candidates that lack an explicit predicate-audit:allow-invalid annotation.
func Audit(roots ...string) ([]Candidate, []Finding, error) {
	symbols, err := collectGoSymbols(roots)
	if err != nil {
		return nil, nil, err
	}
	var candidates []Candidate
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && ignoredDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), "_test.go") {
				// Negative grammar and legacy compatibility fixtures are classified
				// by their owning Go tests. The production corpus gate deliberately
				// audits executable source plus checked-in reference/e2e config.
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			var extracted []Candidate
			var err error
			switch ext {
			case ".go":
				extracted, err = auditGo(path, symbols)
			case ".json", ".json5":
				extracted, err = auditJSON(path)
			case ".yaml", ".yml":
				extracted, err = auditYAML(path)
			case ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".svelte", ".toml", ".graphql", ".gql", ".proto", ".cue":
				extracted, err = auditStructuredText(path)
			default:
				return nil
			}
			if err != nil {
				return err
			}
			candidates = append(candidates, extracted...)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	candidates = deduplicate(candidates)
	findings := make([]Finding, 0)
	allowCache := make(map[string]map[string]string)
	for _, candidate := range candidates {
		if _, err := vocabulary.ParsePredicate(candidate.Predicate); err != nil {
			allowed, ok := allowCache[candidate.File]
			if !ok {
				allowed, _ = loadAllowances(candidate.File)
				allowCache[candidate.File] = allowed
			}
			if _, classified := allowed[candidate.Predicate]; classified {
				continue
			}
			findings = append(findings, Finding{Candidate: candidate, Reason: err.Error()})
		}
	}
	return candidates, findings, nil
}

type goSymbols struct {
	byPackage   map[string]map[string]string
	byDirectory map[string]map[string]string
	byFile      map[string]map[string]string
}

// collectGoSymbols builds the bounded const table needed to resolve ordinary
// identifiers used in Triple.Predicate fields and vocabulary.Register calls.
// Conflicting same-name symbols in packages with the same short name are
// dropped so the audit fails by omission rather than guessing a value.
func collectGoSymbols(roots []string) (*goSymbols, error) {
	return collectGoSymbolsMode(roots, false)
}

func collectGoSymbolsIncludingTests(roots []string) (*goSymbols, error) {
	return collectGoSymbolsMode(roots, true)
}

func collectGoSymbolsMode(roots []string, includeTests bool) (*goSymbols, error) {
	table := &goSymbols{
		byPackage:   make(map[string]map[string]string),
		byDirectory: make(map[string]map[string]string),
		byFile:      make(map[string]map[string]string),
	}
	conflicts := make(map[string]map[string]bool)
	directoryConflicts := make(map[string]map[string]bool)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				ignored := ignoredDir(entry.Name())
				if includeTests {
					ignored = ignoredFixtureDir(entry.Name())
				}
				if path != root && ignored {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || (!includeTests && strings.HasSuffix(path, "_test.go")) {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse Go %s: %w", path, err)
			}
			pkg := file.Name.Name
			directory := filepath.Clean(filepath.Dir(path))
			table.byFile[path] = make(map[string]string)
			if table.byDirectory[directory] == nil {
				table.byDirectory[directory] = make(map[string]string)
				directoryConflicts[directory] = make(map[string]bool)
			}
			if table.byPackage[pkg] == nil {
				table.byPackage[pkg] = make(map[string]string)
				conflicts[pkg] = make(map[string]bool)
			}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					values := spec.(*ast.ValueSpec)
					for i, name := range values.Names {
						if i >= len(values.Values) {
							continue
						}
						value, ok := stringLiteral(values.Values[i])
						if !ok {
							continue
						}
						table.byFile[path][name.Name] = value
						if old, exists := table.byDirectory[directory][name.Name]; exists && old != value {
							directoryConflicts[directory][name.Name] = true
							delete(table.byDirectory[directory], name.Name)
						} else if !directoryConflicts[directory][name.Name] {
							table.byDirectory[directory][name.Name] = value
						}
						if old, exists := table.byPackage[pkg][name.Name]; exists && old != value {
							conflicts[pkg][name.Name] = true
							delete(table.byPackage[pkg], name.Name)
							continue
						}
						if !conflicts[pkg][name.Name] {
							table.byPackage[pkg][name.Name] = value
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return table, nil
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func ignoredDir(name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build", "testdata":
		return true
	default:
		return false
	}
}

func auditGo(path string, symbols *goSymbols) ([]Candidate, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go %s: %w", path, err)
	}
	importPackages := make(map[string]string)
	for _, spec := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		packageName := filepath.Base(importPath)
		alias := packageName
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "_" && alias != "." {
			importPackages[alias] = packageName
		}
	}
	var out []Candidate
	resolve := func(expr ast.Expr) (string, bool) {
		if value, ok := stringLiteral(expr); ok {
			return value, true
		}
		switch typed := expr.(type) {
		case *ast.Ident:
			value, ok := symbols.byFile[path][typed.Name]
			if !ok {
				value, ok = symbols.byPackage[file.Name.Name][typed.Name]
			}
			return value, ok
		case *ast.SelectorExpr:
			pkg, ok := typed.X.(*ast.Ident)
			if !ok {
				return "", false
			}
			packageName := pkg.Name
			if imported, exists := importPackages[pkg.Name]; exists {
				packageName = imported
			}
			value, ok := symbols.byPackage[packageName][typed.Sel.Name]
			return value, ok
		default:
			return "", false
		}
	}
	addExpr := func(expr ast.Expr, surface string) {
		value, ok := resolve(expr)
		if !ok {
			return
		}
		if value == "" || isSymbolicConstant(value) {
			return
		}
		position := fset.Position(expr.Pos())
		for _, predicate := range literalCandidates(value) {
			out = append(out, Candidate{
				File: path, Line: position.Line, Column: position.Column, Predicate: predicate, Surface: surface,
			})
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.KeyValueExpr:
			if ident, ok := n.Key.(*ast.Ident); ok && isPredicateName(ident.Name) {
				addExpr(n.Value, "go-field:"+ident.Name)
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				isVocabularyConst := strings.Contains(filepath.ToSlash(path), "/vocabulary/")
				if (isPredicateDeclarationName(name.Name) || isVocabularyConst) && i < len(n.Values) {
					addExpr(n.Values[i], "go-declaration:"+name.Name)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && strings.EqualFold(ident.Name, "predicate") && i < len(n.Rhs) {
					addExpr(n.Rhs[i], "go-assignment:"+ident.Name)
				}
			}
		case *ast.CallExpr:
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Register" && len(n.Args) > 0 {
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "vocabulary" {
					addExpr(n.Args[0], "go-register")
				}
			}
		case *ast.Field:
			if n.Tag != nil {
				value, err := strconv.Unquote(n.Tag.Value)
				if err == nil {
					position := fset.Position(n.Tag.Pos())
					for _, predicate := range lifecycleTagCandidates(value) {
						out = append(out, Candidate{
							File: path, Line: position.Line, Column: position.Column,
							Predicate: predicate, Surface: "go-lifecycle-tag",
						})
					}
				}
			}
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				break
			}
			value, err := strconv.Unquote(n.Value)
			if err != nil {
				break
			}
			position := fset.Position(n.Pos())
			for _, predicate := range substitutionCandidates(value) {
				out = append(out, Candidate{
					File: path, Line: position.Line, Column: position.Column,
					Predicate: predicate, Surface: "go-substitution",
				})
			}
		}
		return true
	})
	return out, nil
}

func auditJSON(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		// JSON5 and template-bearing files fall back to the structured lexer.
		return auditStructuredText(path)
	}
	return walkConfig(path, root), nil
}

func auditYAML(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse YAML %s: %w", path, err)
	}
	return walkConfig(path, root), nil
}

func walkConfig(path string, root any) []Candidate {
	var out []Candidate
	var walk func(any, string)
	walk = func(value any, key string) {
		switch typed := value.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case map[any]any:
			for childKey, child := range typed {
				walk(child, fmt.Sprint(childKey))
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case string:
			if isPredicateConfigKey(key) && !strings.HasPrefix(typed, "$") && typed != "" && !isSymbolicConstant(typed) {
				out = append(out, Candidate{File: path, Line: findLine(path, typed), Predicate: typed, Surface: "config:" + key})
			}
			for _, predicate := range substitutionCandidates(typed) {
				out = append(out, Candidate{
					File: path, Line: findLine(path, typed), Predicate: predicate, Surface: "config-substitution",
				})
			}
		}
	}
	walk(root, "")
	return out
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
		for _, match := range structuredRE.FindAllStringSubmatch(text, -1) {
			out = append(out, Candidate{File: path, Line: line, Predicate: match[1], Surface: "structured-text"})
		}
		for _, predicate := range substitutionCandidates(text) {
			out = append(out, Candidate{File: path, Line: line, Predicate: predicate, Surface: "structured-substitution"})
		}
	}
	return out, scanner.Err()
}

func isPredicateName(name string) bool {
	name = strings.ToLower(name)
	return name == "predicate" || strings.HasSuffix(name, "predicate") || strings.HasSuffix(name, "predicates")
}

func isPredicateDeclarationName(name string) bool {
	return (name == "Predicate" || strings.HasPrefix(name, "Predicate") || strings.HasSuffix(name, "Predicates")) &&
		!strings.HasPrefix(name, "PredicateReason")
}

func isPredicateConfigKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return key == "predicate" || key == "predicates" || key == "field" ||
		strings.HasSuffix(key, "_predicate") || strings.HasSuffix(key, "_predicates")
}

func literalCandidates(value string) []string {
	if strings.HasPrefix(value, "$") {
		return substitutionCandidates(value)
	}
	return []string{value}
}

// substitutionCandidates extracts the predicate portion of every
// $entity.triple.* / $related.triple.* reference in value, stripping
// the known scalar-graceful suffixes (.length, .triples, .value —
// rule-evaluation-completeness / gh#519) so the audit checks the actual
// predicate identity rather than the suffixed token.
//
// This is a syntactic strip, not the substitution layer's arity
// disambiguation (see applyTripleValueSubstitutions in
// processor/rule/execution_context.go): a predicate genuinely NAMED
// with a literal ".value"/".length"/".triples" trailing segment would
// be mis-stripped here, same known/accepted collision the substitution
// layer itself documents for .length. No production predicate does
// this today (see execution_context.go's tripleLengthRe comment).
func substitutionCandidates(value string) []string {
	var out []string
	for _, match := range substitutionRE.FindAllStringSubmatch(value, -1) {
		predicate := strings.TrimSuffix(match[1], ".length")
		predicate = strings.TrimSuffix(predicate, ".triples")
		predicate = strings.TrimSuffix(predicate, ".value")
		out = append(out, predicate)
	}
	return out
}

func lifecycleTagCandidates(value string) []string {
	var out []string
	for _, match := range lifecycleTagRE.FindAllStringSubmatch(value, -1) {
		out = append(out, match[1])
	}
	return out
}

func isSymbolicConstant(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r != '_' && r != '-' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func findLine(path, needle string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 0
}

func loadAllowances(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if match := allowRE.FindStringSubmatch(line); match != nil {
			allowed[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return allowed, nil
}

func deduplicate(in []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(in))
	out := make([]Candidate, 0, len(in))
	for _, candidate := range in {
		key := fmt.Sprintf(
			"%s:%d:%d:%s:%s",
			candidate.File,
			candidate.Line,
			candidate.Column,
			candidate.Predicate,
			candidate.Surface,
		)
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
		return out[i].Predicate < out[j].Predicate
	})
	return out
}

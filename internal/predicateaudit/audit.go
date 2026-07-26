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
	File                string    `json:"file"`
	Line                int       `json:"line"`
	Column              int       `json:"column"`
	Predicate           string    `json:"value"`
	Surface             string    `json:"surface"`
	Authority           Authority `json:"authority"`
	Status              string    `json:"status"`
	ClassificationBasis string    `json:"classification_basis,omitempty"`
}

// Authority distinguishes graph predicate storage surfaces from heuristic
// name-derived source candidates.
type Authority string

const (
	// AuthorityStoredPredicate marks a source surface that authoritatively
	// stores, registers, configures, or substitutes a graph predicate.
	AuthorityStoredPredicate Authority = "stored-predicate"
	// AuthorityPredicateShaped marks a heuristic Go declaration or assignment
	// whose name caused extraction but whose value may belong to another domain.
	AuthorityPredicateShaped Authority = "predicate-shaped"

	// CandidateStatusValid marks a candidate accepted by the predicate grammar.
	CandidateStatusValid = "valid"
	// CandidateStatusFinding marks a candidate or marker producing a contract finding.
	CandidateStatusFinding = "finding"
	// CandidateStatusClassifiedUnrelated marks one exact accepted heuristic disposition.
	CandidateStatusClassifiedUnrelated = "classified-unrelated"

	maxClassificationsPerFile   = 100
	maxClassificationLineBytes  = 512
	maxClassificationBasisBytes = 160
)

// FindingCode is a stable production-audit contract code.
type FindingCode string

const (
	// FindingInvalidPredicate identifies an unclassified grammar violation.
	FindingInvalidPredicate FindingCode = "invalid-predicate"
	// FindingLegacyBroadAllowance identifies a removed file/value-wide escape hatch.
	FindingLegacyBroadAllowance FindingCode = "legacy-broad-allowance"
	// FindingClassificationMalformed identifies an annotation outside the exact grammar or bounds.
	FindingClassificationMalformed FindingCode = "classification-malformed"
	// FindingClassificationDuplicate identifies repeated annotations for one exact locator.
	FindingClassificationDuplicate FindingCode = "classification-duplicate"
	// FindingClassificationAmbiguous identifies a locator resolving to multiple extracted candidates.
	FindingClassificationAmbiguous FindingCode = "classification-ambiguous"
	// FindingClassificationIneligible identifies an annotation targeting an authoritative occurrence.
	FindingClassificationIneligible FindingCode = "classification-ineligible"
	// FindingClassificationWrongLine identifies an otherwise exact locator with a moved target line.
	FindingClassificationWrongLine FindingCode = "classification-wrong-line"
	// FindingClassificationWrongColumn identifies an otherwise exact locator with a moved target column.
	FindingClassificationWrongColumn FindingCode = "classification-wrong-column"
	// FindingClassificationWrongSurface identifies a locator naming a different extraction surface.
	FindingClassificationWrongSurface FindingCode = "classification-wrong-surface"
	// FindingClassificationWrongValue identifies a locator naming a different extracted value.
	FindingClassificationWrongValue FindingCode = "classification-wrong-value"
	// FindingClassificationStale identifies a locator with no uniquely diagnosable current target.
	FindingClassificationStale FindingCode = "classification-stale"
)

// Finding is an invalid candidate or classification-contract violation.
type Finding struct {
	Candidate
	Code   FindingCode `json:"code"`
	Reason string      `json:"reason"`
}

var (
	substitutionRE = regexp.MustCompile(`\$(?:entity|related)\.triple\.([A-Za-z0-9_.-]+)`)
	structuredRE   = regexp.MustCompile(
		`(?i)(?:predicate|predicates|phasePredicate|linkPredicate|referencePredicates|triplePredicate)` +
			`["']?[[:space:]]*(?::|=)[[:space:]]*["'\x60]([^"'\x60]*)["'\x60]`)
	classificationRE  = regexp.MustCompile(`predicate-audit:classify[[:space:]]+unrelated[[:space:]]+("(?:[^"\\]|\\.)*")[[:space:]]+line=([0-9]+)[[:space:]]+column=([0-9]+)[[:space:]]+surface=([^[:space:]]+)[[:space:]]+(.+)$`)
	lifecycleTagRE    = regexp.MustCompile(`(?:^|[,[:space:]])predicate=([^,"[:space:]]+)`)
	yamlBlockScalarRE = regexp.MustCompile(
		`(?:^|[:=-][[:space:]]*)[|>](?:[1-9][+-]?|[+-][1-9]?)?[[:space:]]*$`,
	)
)

type classification struct {
	File            string
	DeclarationLine int
	TargetLine      int
	TargetColumn    int
	Surface         string
	Value           string
	Basis           string
}

type auditRoot struct {
	physical     string
	evidenceBase string
	label        string
}

// Audit walks roots and returns every extracted candidate plus all stable
// predicate and classification contract findings.
func Audit(roots ...string) ([]Candidate, []Finding, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	auditRoots, err := canonicalAuditRoots(roots)
	if err != nil {
		return nil, nil, err
	}
	physicalRoots := make([]string, 0, len(auditRoots))
	for _, root := range auditRoots {
		physicalRoots = append(physicalRoots, root.physical)
	}
	symbols, err := collectGoSymbols(physicalRoots)
	if err != nil {
		return nil, nil, err
	}
	var candidates []Candidate
	var findings []Finding
	for _, root := range auditRoots {
		err := filepath.WalkDir(root.physical, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root.physical && ignoredDir(entry.Name()) {
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
			classifications, markerFindings, err := loadProductionClassifications(path)
			if err != nil {
				return err
			}
			_, classificationFindings := applyProductionClassifications(path, extracted, classifications)
			normalizeEvidencePaths(root, path, extracted, markerFindings, classificationFindings)
			findings = append(findings, markerFindings...)
			findings = append(findings, classificationFindings...)
			candidates = append(candidates, extracted...)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	candidates = deduplicate(candidates)
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Status == CandidateStatusClassifiedUnrelated {
			continue
		}
		if _, err := vocabulary.ParsePredicate(candidate.Predicate); err != nil {
			candidate.Status = CandidateStatusFinding
			findings = append(findings, Finding{
				Candidate: *candidate, Code: FindingInvalidPredicate, Reason: err.Error(),
			})
			continue
		}
		candidate.Status = CandidateStatusValid
	}
	sortFindings(findings)
	return candidates, findings, nil
}

func canonicalAuditRoots(roots []string) ([]auditRoot, error) {
	physical := make([]string, 0, len(roots))
	bases := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve audit root %q: %w", root, err)
		}
		absolute = filepath.Clean(absolute)
		if _, duplicate := seen[absolute]; duplicate {
			continue
		}
		seen[absolute] = struct{}{}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, fmt.Errorf("inspect audit root %q: %w", root, err)
		}
		physical = append(physical, absolute)
		if repositoryRoot, found := findGitWorktreeRoot(absolute, info.IsDir()); found {
			bases = append(bases, repositoryRoot)
			continue
		}
		localBase := absolute
		if !info.IsDir() {
			localBase = filepath.Dir(absolute)
		}
		bases = append(bases, localBase)
	}
	type rootAndBase struct {
		physical string
		base     string
	}
	pairs := make([]rootAndBase, 0, len(physical))
	for index := range physical {
		pairs = append(pairs, rootAndBase{physical: physical[index], base: bases[index]})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].physical < pairs[j].physical
	})
	bases = bases[:0]
	for _, pair := range pairs {
		bases = append(bases, pair.base)
	}
	base := commonPath(bases)
	out := make([]auditRoot, 0, len(physical))
	for _, pair := range pairs {
		label, err := filepath.Rel(base, pair.physical)
		if err != nil {
			return nil, fmt.Errorf("label audit root %q: %w", pair.physical, err)
		}
		out = append(out, auditRoot{
			physical: pair.physical, evidenceBase: base, label: filepath.ToSlash(label),
		})
	}
	return out, nil
}

func findGitWorktreeRoot(path string, isDirectory bool) (string, bool) {
	current := path
	if !isDirectory {
		current = filepath.Dir(current)
	}
	for {
		gitMetadata, err := os.Stat(filepath.Join(current, ".git"))
		if err == nil && (gitMetadata.IsDir() || gitMetadata.Mode().IsRegular()) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func commonPath(paths []string) string {
	if len(paths) == 0 {
		return "."
	}
	base := paths[0]
	for _, path := range paths[1:] {
		for !pathWithin(base, path) {
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	return base
}

func pathWithin(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func normalizeEvidencePaths(
	root auditRoot,
	physicalPath string,
	candidates []Candidate,
	findingGroups ...[]Finding,
) {
	relative, err := filepath.Rel(root.evidenceBase, physicalPath)
	if err != nil {
		return
	}
	display := filepath.ToSlash(relative)
	for index := range candidates {
		candidates[index].File = display
	}
	for _, findings := range findingGroups {
		for index := range findings {
			findings[index].File = display
		}
	}
}

func canonicalRootLabels(roots []string) []string {
	auditRoots, err := canonicalAuditRoots(roots)
	if err != nil {
		labels := append([]string(nil), roots...)
		sort.Strings(labels)
		return labels
	}
	labels := make([]string, 0, len(auditRoots))
	for _, root := range auditRoots {
		labels = append(labels, root.label)
	}
	return labels
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
	addExpr := func(expr ast.Expr, surface string, authority Authority) {
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
				Authority: authority,
			})
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.KeyValueExpr:
			if ident, ok := n.Key.(*ast.Ident); ok && isPredicateName(ident.Name) {
				addExpr(n.Value, "go-field:"+ident.Name, AuthorityStoredPredicate)
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if isPredicateDeclarationName(name.Name) && i < len(n.Values) {
					addExpr(n.Values[i], "go-declaration:"+name.Name, AuthorityPredicateShaped)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && strings.EqualFold(ident.Name, "predicate") && i < len(n.Rhs) {
					addExpr(n.Rhs[i], "go-assignment:"+ident.Name, AuthorityPredicateShaped)
				}
				if selector, ok := lhs.(*ast.SelectorExpr); ok &&
					strings.EqualFold(selector.Sel.Name, "predicate") &&
					i < len(n.Rhs) {
					addExpr(n.Rhs[i], "go-assignment:"+selector.Sel.Name, AuthorityStoredPredicate)
				}
			}
		case *ast.CallExpr:
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Register" && len(n.Args) > 0 {
				if pkg, ok := selector.X.(*ast.Ident); ok {
					packageName := pkg.Name
					if imported, exists := importPackages[pkg.Name]; exists {
						packageName = imported
					}
					if packageName == "vocabulary" {
						addExpr(n.Args[0], "go-register", AuthorityStoredPredicate)
					}
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
							Predicate: predicate, Surface: "go-lifecycle-tag", Authority: AuthorityStoredPredicate,
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
					Predicate: predicate, Surface: "go-substitution", Authority: AuthorityStoredPredicate,
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
		if strings.EqualFold(filepath.Ext(path), ".json5") {
			return auditStructuredText(path)
		}
		return nil, fmt.Errorf("parse JSON %s: %w", path, err)
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
				out = append(out, Candidate{
					File: path, Line: findLine(path, typed), Predicate: typed, Surface: "config:" + key,
					Authority: AuthorityStoredPredicate,
				})
			}
			for _, predicate := range substitutionCandidates(typed) {
				out = append(out, Candidate{
					File: path, Line: findLine(path, typed), Predicate: predicate, Surface: "config-substitution",
					Authority: AuthorityStoredPredicate,
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
			out = append(out, Candidate{
				File: path, Line: line, Predicate: match[1], Surface: "structured-text",
				Authority: AuthorityStoredPredicate,
			})
		}
		for _, predicate := range substitutionCandidates(text) {
			out = append(out, Candidate{
				File: path, Line: line, Predicate: predicate, Surface: "structured-substitution",
				Authority: AuthorityStoredPredicate,
			})
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

func loadProductionClassifications(path string) ([]classification, []Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	type sourceComment struct {
		line int
		text string
	}
	var comments []sourceComment
	isGo := filepath.Ext(path) == ".go"
	if isGo {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse Go annotations %s: %w", path, err)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				comments = append(comments, sourceComment{
					line: fset.Position(comment.Pos()).Line,
					text: comment.Text,
				})
			}
		}
	} else {
		for _, comment := range nonGoProductionComments(path, data) {
			comments = append(comments, sourceComment{line: comment.line, text: comment.text})
		}
	}
	var classifications []classification
	var findings []Finding
	for _, comment := range comments {
		if strings.Contains(comment.text, "predicate-audit:allow-invalid") {
			findings = append(findings, markerFinding(
				path, comment.line, FindingLegacyBroadAllowance,
				"predicate-audit:allow-invalid is a removed broad allowance and suppresses no candidate",
			))
		}
		if !isGo || !strings.Contains(comment.text, "predicate-audit:classify") {
			continue
		}
		if strings.Count(comment.text, "predicate-audit:classify") != 1 {
			findings = append(findings, markerFinding(
				path, comment.line, FindingClassificationDuplicate,
				"one source comment must contain exactly one production classification",
			))
			continue
		}
		item, parseErr := parseProductionClassification(path, comment.line, comment.text)
		if parseErr != nil {
			findings = append(findings, markerFinding(path, comment.line, FindingClassificationMalformed, parseErr.Error()))
			continue
		}
		classifications = append(classifications, item)
		if len(classifications) > maxClassificationsPerFile {
			findings = append(findings, markerFinding(
				path, comment.line, FindingClassificationMalformed,
				fmt.Sprintf("more than %d production classifications", maxClassificationsPerFile),
			))
			classifications = classifications[:maxClassificationsPerFile]
		}
	}
	return classifications, findings, nil
}

type nonGoComment struct {
	line int
	text string
}

func nonGoProductionComments(path string, data []byte) []nonGoComment {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".yaml", ".yml":
		return yamlProductionComments(data)
	case ".py", ".toml", ".graphql", ".gql":
		return hashProductionComments(data)
	case ".js", ".mjs", ".cjs", ".ts", ".tsx", ".proto", ".cue", ".json5":
		return slashProductionComments(data)
	case ".svelte":
		comments := slashProductionComments(data)
		comments = append(comments, markupProductionComments(data)...)
		sort.Slice(comments, func(i, j int) bool {
			if comments[i].line != comments[j].line {
				return comments[i].line < comments[j].line
			}
			return comments[i].text < comments[j].text
		})
		return comments
	default:
		return nil
	}
}

func yamlProductionComments(data []byte) []nonGoComment {
	var comments []nonGoComment
	blockParentIndent := -1
	for lineIndex, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		indent := leadingSpaceCount(line)
		if blockParentIndent >= 0 {
			if trimmed == "" || indent > blockParentIndent {
				continue
			}
			blockParentIndent = -1
		}
		commentStart := yamlCommentStart(line)
		code := line
		if commentStart >= 0 {
			code = line[:commentStart]
			comments = append(comments, nonGoComment{
				line: lineIndex + 1,
				text: line[commentStart:],
			})
		}
		if yamlBlockScalarRE.MatchString(strings.TrimSpace(code)) {
			blockParentIndent = indent
		}
	}
	return comments
}

func leadingSpaceCount(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func yamlCommentStart(line string) int {
	var quote byte
	for index := 0; index < len(line); {
		char := line[index]
		if quote != 0 {
			if quote == '"' && char == '\\' {
				index += 2
				continue
			}
			if quote == '\'' && char == '\'' &&
				index+1 < len(line) && line[index+1] == '\'' {
				index += 2
				continue
			}
			if char == quote {
				quote = 0
			}
			index++
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			index++
			continue
		}
		if char == '#' {
			return index
		}
		index++
	}
	return -1
}

func hashProductionComments(data []byte) []nonGoComment {
	var comments []nonGoComment
	var tripleQuote string
	for lineIndex, line := range strings.Split(string(data), "\n") {
		var quote byte
		for index := 0; index < len(line); {
			if tripleQuote != "" {
				end := strings.Index(line[index:], tripleQuote)
				if end == -1 {
					break
				}
				index += end + len(tripleQuote)
				tripleQuote = ""
				continue
			}
			if quote != 0 {
				if line[index] == '\\' && quote == '"' {
					index += 2
					continue
				}
				if line[index] == quote {
					quote = 0
				}
				index++
				continue
			}
			if index+3 <= len(line) &&
				(line[index:index+3] == `"""` || line[index:index+3] == `'''`) {
				tripleQuote = line[index : index+3]
				index += 3
				continue
			}
			if line[index] == '"' || line[index] == '\'' {
				quote = line[index]
				index++
				continue
			}
			if line[index] == '#' {
				comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:]})
				break
			}
			index++
		}
	}
	return comments
}

func slashProductionComments(data []byte) []nonGoComment {
	lines := strings.Split(string(data), "\n")
	var comments []nonGoComment
	inBlock := false
	var quote byte
	for lineIndex, line := range lines {
		for index := 0; index < len(line); {
			if inBlock {
				end := strings.Index(line[index:], "*/")
				if end == -1 {
					comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:]})
					break
				}
				end += index
				comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:end]})
				index = end + 2
				inBlock = false
				continue
			}
			char := line[index]
			if quote != 0 {
				if char == '\\' {
					index += 2
					continue
				}
				if char == quote {
					quote = 0
				}
				index++
				continue
			}
			if char == '"' || char == '\'' || char == '`' {
				quote = char
				index++
				continue
			}
			if index+1 < len(line) && line[index:index+2] == "//" {
				comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:]})
				break
			}
			if index+1 < len(line) && line[index:index+2] == "/*" {
				inBlock = true
				index += 2
				continue
			}
			index++
		}
		if !quotedLineContinues(quote, line) {
			quote = 0
		}
	}
	return comments
}

func markupProductionComments(data []byte) []nonGoComment {
	lines := strings.Split(string(data), "\n")
	var comments []nonGoComment
	inComment := false
	var quote byte
	for lineIndex, line := range lines {
		for index := 0; index < len(line); {
			if inComment {
				end := strings.Index(line[index:], "-->")
				if end == -1 {
					comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:]})
					break
				}
				end += index
				comments = append(comments, nonGoComment{line: lineIndex + 1, text: line[index:end]})
				index = end + 3
				inComment = false
				continue
			}
			if quote != 0 {
				if line[index] == '\\' {
					index += 2
					continue
				}
				if line[index] == quote {
					quote = 0
				}
				index++
				continue
			}
			if line[index] == '"' || line[index] == '\'' || line[index] == '`' {
				quote = line[index]
				index++
				continue
			}
			if index+4 <= len(line) && line[index:index+4] == "<!--" {
				inComment = true
				index += 4
				continue
			}
			index++
		}
		if !quotedLineContinues(quote, line) {
			quote = 0
		}
	}
	return comments
}

func quotedLineContinues(quote byte, line string) bool {
	if quote == '`' {
		return true
	}
	if quote != '"' && quote != '\'' {
		return false
	}
	backslashes := 0
	for index := len(line) - 1; index >= 0 && line[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func parseProductionClassification(path string, line int, text string) (classification, error) {
	if len(text) > maxClassificationLineBytes {
		return classification{}, fmt.Errorf("production classification exceeds %d bytes", maxClassificationLineBytes)
	}
	match := classificationRE.FindStringSubmatch(text)
	if match == nil {
		return classification{}, fmt.Errorf("malformed production unrelated classification")
	}
	value, err := strconv.Unquote(match[1])
	if err != nil {
		return classification{}, fmt.Errorf("unquote production classification: %w", err)
	}
	targetLine, err := strconv.Atoi(match[2])
	if err != nil || targetLine < 1 {
		return classification{}, fmt.Errorf("classification target line must be positive")
	}
	targetColumn, err := strconv.Atoi(match[3])
	if err != nil || targetColumn < 1 {
		return classification{}, fmt.Errorf("classification target column must be positive")
	}
	basis := strings.TrimSpace(match[5])
	basis = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(basis, "*/"), "-->"))
	if basis == "" || len(basis) > maxClassificationBasisBytes {
		return classification{}, fmt.Errorf(
			"classification basis must be 1-%d bytes", maxClassificationBasisBytes,
		)
	}
	return classification{
		File: path, DeclarationLine: line, TargetLine: targetLine, TargetColumn: targetColumn,
		Surface: match[4], Value: value, Basis: basis,
	}, nil
}

func applyProductionClassifications(
	path string,
	candidates []Candidate,
	classifications []classification,
) ([]classification, []Finding) {
	var accepted []classification
	var findings []Finding
	duplicates := make(map[string]bool)
	firstDeclaration := make(map[string]int)
	for _, item := range classifications {
		key := classificationKey(item)
		if first, exists := firstDeclaration[key]; exists {
			duplicates[key] = true
			findings = append(findings, classificationFinding(
				item, FindingClassificationDuplicate,
				fmt.Sprintf("duplicate classification; first declared at line %d", first),
			))
			continue
		}
		firstDeclaration[key] = item.DeclarationLine
	}
	for _, item := range classifications {
		if duplicates[classificationKey(item)] {
			continue
		}
		matches := candidateIndexes(candidates, func(candidate Candidate) bool {
			return candidate.Line == item.TargetLine &&
				candidate.Column == item.TargetColumn &&
				candidate.Surface == item.Surface &&
				candidate.Predicate == item.Value
		})
		if len(matches) > 1 {
			findings = append(findings, classificationFinding(
				item, FindingClassificationAmbiguous,
				fmt.Sprintf("classification resolves to %d candidates", len(matches)),
			))
			continue
		}
		if len(matches) == 0 {
			code := diagnoseClassificationMiss(candidates, item)
			findings = append(findings, classificationFinding(
				item, code,
				fmt.Sprintf(
					"classification does not resolve exactly at line=%d column=%d surface=%s value=%q",
					item.TargetLine, item.TargetColumn, item.Surface, item.Value,
				),
			))
			continue
		}
		candidate := &candidates[matches[0]]
		if candidate.Authority != AuthorityPredicateShaped {
			findingCandidate := *candidate
			findingCandidate.Status = CandidateStatusFinding
			findings = append(findings, Finding{
				Candidate: findingCandidate,
				Code:      FindingClassificationIneligible,
				Reason:    "only predicate-shaped heuristic Go occurrences may be classified unrelated",
			})
			continue
		}
		candidate.Status = CandidateStatusClassifiedUnrelated
		candidate.ClassificationBasis = item.Basis
		accepted = append(accepted, item)
	}
	_ = path
	return accepted, findings
}

func diagnoseClassificationMiss(candidates []Candidate, item classification) FindingCode {
	for _, candidate := range candidates {
		if candidate.Line == item.TargetLine && candidate.Column == item.TargetColumn &&
			candidate.Surface == item.Surface && candidate.Predicate != item.Value {
			return FindingClassificationWrongValue
		}
	}
	for _, candidate := range candidates {
		if candidate.Line == item.TargetLine && candidate.Column == item.TargetColumn &&
			candidate.Predicate == item.Value && candidate.Surface != item.Surface {
			return FindingClassificationWrongSurface
		}
	}
	for _, candidate := range candidates {
		if candidate.Line == item.TargetLine && candidate.Surface == item.Surface &&
			candidate.Predicate == item.Value && candidate.Column != item.TargetColumn {
			return FindingClassificationWrongColumn
		}
	}
	for _, candidate := range candidates {
		if candidate.Column == item.TargetColumn && candidate.Surface == item.Surface &&
			candidate.Predicate == item.Value && candidate.Line != item.TargetLine {
			return FindingClassificationWrongLine
		}
	}
	return FindingClassificationStale
}

func classificationKey(item classification) string {
	return fmt.Sprintf(
		"%d:%d:%s:%s", item.TargetLine, item.TargetColumn, item.Surface, item.Value,
	)
}

func candidateIndexes(candidates []Candidate, matches func(Candidate) bool) []int {
	var indexes []int
	for index, candidate := range candidates {
		if matches(candidate) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func markerFinding(path string, line int, code FindingCode, reason string) Finding {
	return Finding{
		Candidate: Candidate{
			File: path, Line: line, Surface: "classification", Authority: AuthorityPredicateShaped,
			Status: CandidateStatusFinding,
		},
		Code: code, Reason: reason,
	}
}

func classificationFinding(item classification, code FindingCode, reason string) Finding {
	return Finding{
		Candidate: Candidate{
			File: item.File, Line: item.TargetLine, Column: item.TargetColumn,
			Predicate: item.Value, Surface: item.Surface, Authority: AuthorityPredicateShaped,
			Status: CandidateStatusFinding,
		},
		Code: code, Reason: reason,
	}
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].Column != findings[j].Column {
			return findings[i].Column < findings[j].Column
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Surface != findings[j].Surface {
			return findings[i].Surface < findings[j].Surface
		}
		return findings[i].Predicate < findings[j].Predicate
	})
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

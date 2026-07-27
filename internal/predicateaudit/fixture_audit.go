package predicateaudit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/c360studio/semstreams/vocabulary"
	"gopkg.in/yaml.v3"
)

const (
	// FixtureInvalidMarker introduces one JSON source classification on the
	// same line as the exact malformed Go predicate occurrence it classifies.
	FixtureInvalidMarker = "predicate-audit:invalid"
	// FixtureUnrelatedMarker introduces one exact disposition for an ambiguous
	// name-derived Go occurrence that was reviewed as unrelated to predicates.
	FixtureUnrelatedMarker = "predicate-audit:unrelated"
	// FixtureStoredPredicateKind is the contract kind audited by this corpus.
	FixtureStoredPredicateKind = "stored-predicate"
	semanticTestImportPath     = "github.com/c360studio/semstreams/internal/semantictest"
	maxFixtureDispositionBasis = 256
)

// FixtureCandidate records one predicate occurrence in a tracked Go test or
// a structured artifact beneath testdata. RuntimeAuthoritative is true when
// internal/semantictest.Predicate validates a dynamic position at test time.
type FixtureCandidate struct {
	File                 string
	Line                 int
	Column               int
	Location             string
	Document             int
	Record               int
	Occurrence           int
	Predicate            string
	Surface              string
	RuntimeAuthoritative bool
	Unresolved           bool
}

// FixtureFinding reports one malformed or incorrectly classified fixture.
type FixtureFinding struct {
	File       string
	Line       int
	Location   string
	Document   int
	Record     int
	Occurrence int
	Predicate  string
	Code       string
	Message    string
}

// FixtureAuditResult is the complete complementary test-fixture corpus.
type FixtureAuditResult struct {
	Candidates      []FixtureCandidate
	Findings        []FixtureFinding
	Classifications int
	Dispositions    int
}

// FixtureClassificationManifest stores exact classifications for commentless
// structured testdata. Location remains an RFC6901-style semantic pointer;
// document, record, and occurrence are separate physical discriminators.
type FixtureClassificationManifest struct {
	Version            int                          `json:"version"`
	Entries            []FixtureClassificationEntry `json:"entries"`
	UnrelatedArtifacts []FixtureArtifactDisposition `json:"unrelated_artifacts,omitempty"`
}

// FixtureArtifactDisposition records one exact unsupported testdata artifact
// that was reviewed as unrelated to predicate syntax.
type FixtureArtifactDisposition struct {
	File           string `json:"file"`
	Classification string `json:"classification"`
	Basis          string `json:"basis"`
}

// FixtureClassificationEntry classifies one exact structured fixture value.
type FixtureClassificationEntry struct {
	File       string  `json:"file"`
	Location   string  `json:"location"`
	Document   *int    `json:"document"`
	Record     *int    `json:"record"`
	Occurrence *int    `json:"occurrence"`
	Kind       string  `json:"kind"`
	Value      *string `json:"value"`
	Reason     string  `json:"reason"`
}

type sourceFixtureClassification struct {
	File     string
	Line     int
	Location string  `json:"location,omitempty"`
	Kind     string  `json:"kind"`
	Value    *string `json:"value"`
	Reason   string  `json:"reason"`
}

type sourceFixtureDisposition struct {
	File    string  `json:"-"`
	Line    int     `json:"-"`
	Column  int     `json:"column"`
	Surface string  `json:"surface"`
	Value   *string `json:"value"`
	Basis   string  `json:"basis"`
}

// AuditTestFixtures audits the corpus deliberately excluded by Audit. It only
// walks *_test.go plus structured artifacts below directories named testdata.
// Go negatives use same-line source annotations; commentless fixtures use the
// exact checked manifest supplied by manifestPath.
func AuditTestFixtures(manifestPath string, roots ...string) (FixtureAuditResult, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	manifest, err := loadFixtureClassificationManifest(manifestPath)
	if err != nil {
		return FixtureAuditResult{}, err
	}
	symbols, err := collectGoSymbolsIncludingTests(roots)
	if err != nil {
		return FixtureAuditResult{}, err
	}

	var candidates []FixtureCandidate
	var sourceClassifications []sourceFixtureClassification
	var sourceDispositions []sourceFixtureDisposition
	var unsupportedArtifacts []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && ignoredFixtureDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			inTestdata := pathHasDirectory(path, "testdata")
			if strings.HasSuffix(entry.Name(), "_test.go") || (inTestdata && filepath.Ext(path) == ".go") {
				extracted, classifications, dispositions, err := auditFixtureGo(path, symbols)
				if err != nil {
					return err
				}
				candidates = append(candidates, normalizeFixtureCandidates(root, extracted)...)
				for _, classification := range classifications {
					classification.File = normalizeFixturePath(root, classification.File)
					sourceClassifications = append(sourceClassifications, classification)
				}
				for _, disposition := range dispositions {
					disposition.File = normalizeFixturePath(root, disposition.File)
					sourceDispositions = append(sourceDispositions, disposition)
				}
				return nil
			}
			if !inTestdata {
				return nil
			}
			if !isStructuredFixtureExtension(path) {
				unsupportedArtifacts = append(unsupportedArtifacts, normalizeFixturePath(root, path))
				return nil
			}
			extracted, err := auditFixtureStructured(path)
			if err != nil {
				return err
			}
			candidates = append(candidates, normalizeFixtureCandidates(root, extracted)...)
			return nil
		})
		if err != nil {
			return FixtureAuditResult{}, err
		}
	}

	candidates = deduplicateFixtureCandidates(candidates)
	result := classifyFixtureCandidates(candidates, sourceClassifications, sourceDispositions, manifest.Entries)
	result.Dispositions = len(sourceDispositions) + len(manifest.UnrelatedArtifacts)
	result.Findings = append(result.Findings, classifyUnsupportedArtifacts(unsupportedArtifacts, manifest.UnrelatedArtifacts)...)
	sortFixtureFindings(result.Findings)
	return result, nil
}

func auditFixtureGo(
	path string,
	symbols *goSymbols,
) ([]FixtureCandidate, []sourceFixtureClassification, []sourceFixtureDisposition, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse Go fixture %s: %w", path, err)
	}
	importsSemanticTest, err := validateSemanticTestPredicateImport(file, path)
	if err != nil {
		return nil, nil, nil, err
	}

	raw, err := auditGo(path, symbols)
	if err != nil {
		return nil, nil, nil, err
	}
	descriptivePositions := fixtureDescriptiveStringPositions(fset, file)
	embedded, embeddedPositions := fixtureEmbeddedStructuredCandidates(path, fset, file)
	out := make([]FixtureCandidate, 0, len(raw)+len(embedded))
	for _, candidate := range raw {
		positionKey := fmt.Sprintf("%d:%d", candidate.Line, candidate.Column)
		if candidate.Surface == "go-substitution" && descriptivePositions[positionKey] {
			continue
		}
		if candidate.Surface == "go-substitution" && embeddedPositions[positionKey] {
			continue
		}
		out = append(out, FixtureCandidate{
			File: candidate.File, Line: candidate.Line, Column: candidate.Column,
			Location:  fmt.Sprintf("line:%d:column:%d", candidate.Line, candidate.Column),
			Predicate: candidate.Predicate, Surface: candidate.Surface,
		})
	}
	out = append(out, embedded...)

	resolve := fixtureGoResolver(path, file, symbols)
	out = append(out, fixtureKnownGoSurfaceCandidates(path, fset, file, resolve, importsSemanticTest)...)
	out = append(out, fixtureFuzzSeedCandidates(path, fset, file, resolve)...)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isSemanticTestPredicateCall(call.Fun, file.Name.Name, path, importsSemanticTest) {
			if candidate, extracted := fixtureAuthorityCallCandidate(path, fset, file, call, resolve); extracted {
				out = append(out, candidate)
			}
			return true
		}
		position := fset.Position(call.Pos())
		candidate := FixtureCandidate{
			File: path, Line: position.Line, Column: position.Column,
			Location: fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
			Surface:  "go-call:semantictest.Predicate", RuntimeAuthoritative: true,
		}
		if len(call.Args) != 4 {
			candidate.RuntimeAuthoritative = false
			candidate.Surface = "go-call:semantictest.Predicate:wrong-arity"
			out = append(out, candidate)
			return true
		}
		parts := make([]string, 0, 3)
		for _, argument := range call.Args[1:] {
			part, resolved := resolve(argument)
			if !resolved {
				out = append(out, candidate)
				return true
			}
			parts = append(parts, part)
		}
		candidate.Predicate = strings.Join(parts, ".")
		out = append(out, candidate)
		return true
	})
	out = append(out, fixtureNegativePartsCandidates(path, fset, file)...)
	out = append(out, fixtureHelperIndirectionCandidates(path, fset, file, importsSemanticTest)...)

	classifications, dispositions, err := loadSourceFixtureAnnotations(path, fset, file.Comments)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, classifications, dispositions, nil
}

func fixtureEmbeddedStructuredCandidates(
	path string,
	fset *token.FileSet,
	file *ast.File,
) ([]FixtureCandidate, map[string]bool) {
	var out []FixtureCandidate
	handled := make(map[string]bool)
	descriptivePositions := fixtureDescriptiveStringPositions(fset, file)
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, rawOffsets, err := decodeGoStringWithOffsets(literal.Value)
		if err != nil {
			return true
		}
		start := fset.Position(literal.Pos())
		if descriptivePositions[fmt.Sprintf("%d:%d", start.Line, start.Column)] {
			return true
		}
		candidateStart := len(out)
		decodedOffset := 0
		for _, line := range strings.Split(value, "\n") {
			for _, indexed := range structuredRE.FindAllStringSubmatchIndex(line, -1) {
				predicate := line[indexed[2]:indexed[3]]
				if strings.HasPrefix(predicate, "$") {
					continue
				}
				innerOffset := decodedOffset + indexed[2]
				lineNumber, column := goLiteralSourcePosition(start, literal.Value, rawOffsets[innerOffset])
				out = append(out, FixtureCandidate{
					File: path, Line: lineNumber, Column: column, Predicate: predicate,
					Surface: "go-embedded-structured",
					Location: fmt.Sprintf(
						"line:%d:column:%d:embedded-structured:inner-offset:%d",
						lineNumber,
						column,
						innerOffset,
					),
				})
			}
			for _, indexed := range substitutionRE.FindAllStringSubmatchIndex(line, -1) {
				predicate := fixtureSubstitutionPredicate(line[indexed[2]:indexed[3]])
				innerOffset := decodedOffset + indexed[2]
				lineNumber, column := goLiteralSourcePosition(start, literal.Value, rawOffsets[innerOffset])
				out = append(out, FixtureCandidate{
					File: path, Line: lineNumber, Column: column, Predicate: predicate,
					Surface: "go-embedded-substitution",
					Location: fmt.Sprintf(
						"line:%d:column:%d:embedded-substitution:inner-offset:%d",
						lineNumber,
						column,
						innerOffset,
					),
				})
			}
			decodedOffset += len(line) + 1
		}
		if len(out) > candidateStart {
			handled[fmt.Sprintf("%d:%d", start.Line, start.Column)] = true
		}
		return true
	})
	return out, handled
}

func decodeGoStringWithOffsets(literal string) (string, []int, error) {
	if len(literal) < 2 {
		return "", nil, errors.New("short Go string literal")
	}
	if literal[0] == '`' {
		value := literal[1 : len(literal)-1]
		offsets := make([]int, len(value))
		for index := range value {
			offsets[index] = index + 1
		}
		return value, offsets, nil
	}
	if literal[0] != '"' {
		return "", nil, errors.New("unsupported Go string quote")
	}
	content := literal[1 : len(literal)-1]
	var decoded []byte
	var offsets []int
	for rawOffset := 0; rawOffset < len(content); {
		character, _, tail, err := strconv.UnquoteChar(content[rawOffset:], '"')
		if err != nil {
			return "", nil, err
		}
		consumed := len(content[rawOffset:]) - len(tail)
		var characterBytes []byte
		if content[rawOffset] == '\\' && rawOffset+1 < len(content) &&
			(content[rawOffset+1] == 'x' || (content[rawOffset+1] >= '0' && content[rawOffset+1] <= '7')) {
			characterBytes = []byte{byte(character)}
		} else {
			characterBytes = []byte(string(character))
		}
		decoded = append(decoded, characterBytes...)
		for range characterBytes {
			offsets = append(offsets, rawOffset+1)
		}
		rawOffset += consumed
	}
	return string(decoded), offsets, nil
}

func goLiteralSourcePosition(start token.Position, literal string, tokenOffset int) (int, int) {
	line := start.Line
	column := start.Column
	for index := 0; index < tokenOffset && index < len(literal); index++ {
		if literal[index] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func fixtureHelperIndirectionCandidates(
	path string,
	fset *token.FileSet,
	file *ast.File,
	importsSemanticTest bool,
) []FixtureCandidate {
	parents := fixtureParentMap(file)
	var out []FixtureCandidate
	ast.Inspect(file, func(node ast.Node) bool {
		isHelper := false
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			identifier, ok := typed.X.(*ast.Ident)
			isHelper = importsSemanticTest && ok && identifier.Name == "semantictest" && typed.Sel.Name == "Predicate"
		case *ast.Ident:
			isSelectorField := false
			if selector, ok := parents[node].(*ast.SelectorExpr); ok {
				isSelectorField = selector.Sel == node
			}
			isHelper = !isSelectorField && typed.Name == "Predicate" && file.Name.Name == "semantictest" &&
				pathHasPackage(path, "internal/semantictest")
		}
		if !isHelper {
			return true
		}
		parent := parents[node]
		if call, ok := parent.(*ast.CallExpr); ok && call.Fun == node {
			if fixtureHelperCallIsForwarded(call, parents, file, path, importsSemanticTest) {
				position := fset.Position(call.Pos())
				out = append(out, FixtureCandidate{
					File: path, Line: position.Line, Column: position.Column,
					Location: fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
					Surface:  "go-call:semantictest.Predicate:wrapper", Unresolved: true,
				})
			}
			return true
		}
		position := fset.Position(node.Pos())
		out = append(out, FixtureCandidate{
			File: path, Line: position.Line, Column: position.Column,
			Location: fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
			Surface:  "go-call:semantictest.Predicate:value", Unresolved: true,
		})
		return true
	})
	return out
}

func fixtureHelperCallIsForwarded(
	call *ast.CallExpr,
	parents map[ast.Node]ast.Node,
	file *ast.File,
	path string,
	importsSemanticTest bool,
) bool {
	if !importsSemanticTest || file.Name.Name == "semantictest" && pathHasPackage(path, "internal/semantictest") {
		return false
	}
	for current := ast.Node(call); current != nil; current = parents[current] {
		switch typed := current.(type) {
		case *ast.FuncLit:
			if !fixtureFuncLitIsExactTestingRunCallback(typed, parents, file) {
				return true
			}
		case *ast.FuncDecl:
			return fixtureGoTestEntrypointKind(typed, file) == ""
		}
	}
	return !fixtureCallIsDirectPredicateValue(call, parents)
}

func fixtureFuncLitIsExactTestingRunCallback(
	function *ast.FuncLit,
	parents map[ast.Node]ast.Node,
	file *ast.File,
) bool {
	parameter := fixtureExactTestingTParameter(function.Type, file)
	if parameter == nil {
		return false
	}
	call, ok := parents[function].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Args[1] != function {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Run" {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok || receiver.Obj == nil {
		return false
	}
	return fixtureIdentifierIsEnclosingTestingReceiver(receiver, call, parents, file)
}

func fixtureIdentifierIsEnclosingTestingReceiver(
	receiver *ast.Ident,
	start ast.Node,
	parents map[ast.Node]ast.Node,
	file *ast.File,
) bool {
	for current := start; current != nil; current = parents[current] {
		switch typed := current.(type) {
		case *ast.FuncDecl:
			if fixtureGoTestEntrypointKind(typed, file) == "" {
				return false
			}
			parameter := fixtureExactTestingTParameter(typed.Type, file)
			return parameter != nil && parameter.Obj == receiver.Obj
		case *ast.FuncLit:
			if !fixtureFuncLitIsExactTestingRunCallback(typed, parents, file) {
				return false
			}
			parameter := fixtureExactTestingTParameter(typed.Type, file)
			if parameter != nil && parameter.Obj == receiver.Obj {
				return true
			}
		}
	}
	return false
}

func fixtureExactTestingTParameter(function *ast.FuncType, file *ast.File) *ast.Ident {
	if function == nil || function.TypeParams != nil && len(function.TypeParams.List) != 0 ||
		function.Results != nil && len(function.Results.List) != 0 ||
		function.Params == nil || len(function.Params.List) != 1 {
		return nil
	}
	parameter := function.Params.List[0]
	if len(parameter.Names) != 1 {
		return nil
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return nil
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return nil
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || fixtureImportPath(file, packageName.Name) != "testing" {
		return nil
	}
	return parameter.Names[0]
}

func fixtureGoTestEntrypointKind(function *ast.FuncDecl, file *ast.File) string {
	if function == nil || function.Recv != nil || function.Body == nil ||
		function.Type.TypeParams != nil && len(function.Type.TypeParams.List) != 0 ||
		function.Type.Results != nil && len(function.Type.Results.List) != 0 ||
		function.Type.Params == nil || len(function.Type.Params.List) != 1 {
		return ""
	}
	parameter := function.Type.Params.List[0]
	if len(parameter.Names) > 1 {
		return ""
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || fixtureImportPath(file, packageName.Name) != "testing" {
		return ""
	}
	for _, entrypoint := range []struct {
		prefix string
		typeID string
		kind   string
	}{
		{prefix: "Test", typeID: "T", kind: "test"},
		{prefix: "Fuzz", typeID: "F", kind: "fuzz"},
		{prefix: "Benchmark", typeID: "B", kind: "benchmark"},
	} {
		if selector.Sel.Name != entrypoint.typeID || !strings.HasPrefix(function.Name.Name, entrypoint.prefix) {
			continue
		}
		suffix := strings.TrimPrefix(function.Name.Name, entrypoint.prefix)
		if suffix == "" {
			return ""
		}
		first, _ := utf8.DecodeRuneInString(suffix)
		if unicode.IsLower(first) {
			return ""
		}
		return entrypoint.kind
	}
	return ""
}

func fixtureCallIsDirectPredicateValue(call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	parent := parents[call]
	switch typed := parent.(type) {
	case *ast.KeyValueExpr:
		_, known := fixturePredicateKeyName(typed.Key)
		return known && typed.Value == call
	case *ast.AssignStmt:
		for index, right := range typed.Rhs {
			if right != call || index >= len(typed.Lhs) {
				continue
			}
			_, known := fixturePredicateAssignmentTarget(typed.Lhs[index])
			return known
		}
	}
	return false
}

func fixtureParentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func fixtureKnownGoSurfaceCandidates(
	path string,
	fset *token.FileSet,
	file *ast.File,
	resolve func(ast.Expr) (string, bool),
	importsSemanticTest bool,
) []FixtureCandidate {
	var out []FixtureCandidate
	addScalar := func(expression ast.Expr, surface string, skipNonStringControl bool) {
		if skipNonStringControl && fixtureExpressionIsDefinitelyNonString(expression) {
			return
		}
		if call, ok := expression.(*ast.CallExpr); ok &&
			isSemanticTestPredicateCall(call.Fun, file.Name.Name, path, importsSemanticTest) {
			// auditFixtureGo emits the exact authoritative helper candidate. Do
			// not also turn the containing predicate field into an unresolved
			// scalar. An outer call or other wrapper does not match this branch
			// and therefore remains fail-closed.
			return
		}
		position := fset.Position(expression.Pos())
		predicate, resolved := resolve(expression)
		out = append(out, FixtureCandidate{
			File: path, Line: position.Line, Column: position.Column,
			Location:  fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
			Predicate: predicate, Surface: surface, Unresolved: !resolved,
		})
	}
	add := func(expression ast.Expr, surface, name string) {
		if !fixturePluralPredicateName(name) {
			addScalar(expression, surface, fixtureNonStringPredicateControl(name))
			return
		}
		elements, exact := fixtureInlinePredicateSliceElements(expression)
		if !exact {
			position := fset.Position(expression.Pos())
			predicate, _ := resolve(expression)
			out = append(out, FixtureCandidate{
				File: path, Line: position.Line, Column: position.Column,
				Location:  fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
				Predicate: predicate, Surface: surface, Unresolved: true,
			})
			return
		}
		for _, element := range elements {
			if keyValue, keyed := element.(*ast.KeyValueExpr); keyed {
				element = keyValue.Value
			}
			addScalar(element, surface+":element", false)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.KeyValueExpr:
			if key, ok := fixturePredicateKeyName(typed.Key); ok {
				add(typed.Value, "go-field:"+key, key)
			}
		case *ast.AssignStmt:
			for index, left := range typed.Lhs {
				if index >= len(typed.Rhs) {
					continue
				}
				if name, known := fixturePredicateAssignmentTarget(left); known {
					add(typed.Rhs[index], "go-assignment:"+name, name)
				}
			}
		case *ast.ValueSpec:
			for index, name := range typed.Names {
				if index < len(typed.Values) && isPredicateDeclarationName(name.Name) {
					add(typed.Values[index], "go-declaration:"+name.Name, name.Name)
				}
			}
		case *ast.CallExpr:
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Register" || len(typed.Args) == 0 {
				break
			}
			identifier, ok := selector.X.(*ast.Ident)
			if ok && fixtureImportPath(file, identifier.Name) == "github.com/c360studio/semstreams/vocabulary" {
				addScalar(typed.Args[0], "go-register", false)
			}
		}
		return true
	})
	return out
}

func fixtureNonStringPredicateControl(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	return normalized == "include_predicates" || normalized == "includepredicates"
}

func fixturePluralPredicateName(name string) bool {
	if fixtureNonStringPredicateControl(name) {
		return false
	}
	normalized := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	return normalized == "predicates" || strings.HasSuffix(normalized, "_predicates") ||
		strings.HasSuffix(normalized, "predicates")
}

// fixtureExpressionIsDefinitelyNonString excludes control values that happen
// to sit below a key such as include_predicates. The decision is based only on
// the expression's intrinsic Go shape; identifiers and calls remain unresolved
// because this syntax-only audit cannot prove their type.
func fixtureExpressionIsDefinitelyNonString(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		return typed.Kind != token.STRING
	case *ast.Ident:
		return typed.Name == "true" || typed.Name == "false" || typed.Name == "nil"
	case *ast.UnaryExpr:
		switch typed.Op {
		case token.NOT:
			return true
		case token.ADD, token.SUB, token.XOR:
			literal, ok := typed.X.(*ast.BasicLit)
			return ok && literal.Kind != token.STRING
		}
	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND, token.LOR, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return true
		}
	case *ast.CallExpr:
		identifier, ok := typed.Fun.(*ast.Ident)
		return ok && identifier.Name == "bool" && identifier.Obj == nil
	}
	return false
}

func fixtureInlinePredicateSliceElements(expression ast.Expr) ([]ast.Expr, bool) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	array, ok := literal.Type.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return nil, false
	}
	if !fixturePredicateContainerElementType(array.Elt) {
		return nil, false
	}
	return literal.Elts, true
}

func fixturePredicateContainerElementType(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name == "string" || typed.Name == "any"
	case *ast.InterfaceType:
		return typed.Methods != nil && len(typed.Methods.List) == 0
	default:
		return false
	}
}

func fixturePredicateKeyName(expression ast.Expr) (string, bool) {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name, isPredicateName(typed.Name)
	case *ast.BasicLit:
		value, ok := stringLiteral(typed)
		return value, ok && isPredicateConfigKey(value)
	default:
		return "", false
	}
}

func fixturePredicateAssignmentTarget(expression ast.Expr) (string, bool) {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name, strings.EqualFold(typed.Name, "predicate") || isPredicateName(typed.Name)
	case *ast.SelectorExpr:
		return typed.Sel.Name, isPredicateName(typed.Sel.Name)
	case *ast.IndexExpr:
		value, ok := stringLiteral(typed.Index)
		return value, ok && isPredicateConfigKey(value)
	default:
		return "", false
	}
}

func fixtureFuzzSeedCandidates(
	path string,
	fset *token.FileSet,
	file *ast.File,
	resolve func(ast.Expr) (string, bool),
) []FixtureCandidate {
	if file.Name.Name != "vocabulary" || !pathHasPackage(path, "vocabulary") {
		return nil
	}
	fileTables := fixtureCompositeTables(file)
	var out []FixtureCandidate
	add := func(expression ast.Expr) {
		position := fset.Position(expression.Pos())
		predicate, resolved := resolve(expression)
		out = append(out, FixtureCandidate{
			File: path, Line: position.Line, Column: position.Column,
			Location:  fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
			Predicate: predicate, Surface: "go-fuzz-seed", Unresolved: !resolved,
		})
	}
	parents := fixtureParentMap(file)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || fixtureGoTestEntrypointKind(function, file) != "fuzz" {
			continue
		}
		fuzzReceivers := make(map[string]bool)
		parameter := function.Type.Params.List[0]
		if len(parameter.Names) == 1 {
			fuzzReceivers[parameter.Names[0].Name] = true
		}
		addAliases := make(map[string]bool)
		addUnresolved := func(node ast.Node, surface string) {
			position := fset.Position(node.Pos())
			out = append(out, FixtureCandidate{
				File: path, Line: position.Line, Column: position.Column,
				Location: fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
				Surface:  surface, Unresolved: true,
			})
		}
		isReceiver := func(expression ast.Expr) bool {
			identifier, ok := expression.(*ast.Ident)
			return ok && fuzzReceivers[identifier.Name]
		}
		isAddSelector := func(expression ast.Expr) bool {
			selector, ok := expression.(*ast.SelectorExpr)
			return ok && selector.Sel.Name == "Add" && isReceiver(selector.X)
		}
		tables := make(map[string]*ast.CompositeLit, len(fileTables))
		for name, literal := range fileTables {
			tables[name] = literal
		}
		for name, literal := range fixtureCompositeTables(function.Body) {
			tables[name] = literal
		}
		rangeValues := make(map[string][]ast.Expr)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			rangeStatement, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			value, ok := rangeStatement.Value.(*ast.Ident)
			if !ok {
				return true
			}
			literal, ok := rangeStatement.X.(*ast.CompositeLit)
			if !ok {
				if identifier, identifierOK := rangeStatement.X.(*ast.Ident); identifierOK {
					literal, ok = tables[identifier.Name]
				}
			}
			if ok {
				rangeValues[value.Name] = literal.Elts
			}
			return true
		})
		addArguments := func(arguments []ast.Expr) {
			for _, argument := range arguments {
				if identifier, identifierOK := argument.(*ast.Ident); identifierOK {
					if elements, expanded := rangeValues[identifier.Name]; expanded {
						for _, element := range elements {
							add(element)
						}
						continue
					}
				}
				add(argument)
			}
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				for index, right := range typed.Rhs {
					if index >= len(typed.Lhs) {
						continue
					}
					left, ok := typed.Lhs[index].(*ast.Ident)
					if !ok || left.Name == "_" {
						continue
					}
					if isReceiver(right) {
						fuzzReceivers[left.Name] = true
						addUnresolved(right, "go-fuzz-receiver-alias")
					} else if isAddSelector(right) {
						addAliases[left.Name] = true
					}
				}
			case *ast.ValueSpec:
				for index, right := range typed.Values {
					if index >= len(typed.Names) || typed.Names[index].Name == "_" {
						continue
					}
					if isReceiver(right) {
						fuzzReceivers[typed.Names[index].Name] = true
						addUnresolved(right, "go-fuzz-receiver-alias")
					} else if isAddSelector(right) {
						addAliases[typed.Names[index].Name] = true
					}
				}
			case *ast.SelectorExpr:
				if !isAddSelector(typed) {
					break
				}
				if call, ok := parents[typed].(*ast.CallExpr); !ok || call.Fun != typed {
					addUnresolved(typed, "go-fuzz-add-alias")
				}
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, selectorOK := call.Fun.(*ast.SelectorExpr); selectorOK && isAddSelector(selector) {
				addArguments(call.Args)
				return true
			}
			if identifier, identifierOK := call.Fun.(*ast.Ident); identifierOK && addAliases[identifier.Name] {
				addArguments(call.Args)
			}
			return true
		})
	}
	return out
}

func fixtureCompositeTables(root ast.Node) map[string]*ast.CompositeLit {
	tables := make(map[string]*ast.CompositeLit)
	ast.Inspect(root, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ValueSpec:
			for index, name := range typed.Names {
				if index >= len(typed.Values) {
					continue
				}
				if literal, ok := typed.Values[index].(*ast.CompositeLit); ok {
					tables[name.Name] = literal
				}
			}
		case *ast.AssignStmt:
			for index, left := range typed.Lhs {
				if index >= len(typed.Rhs) {
					continue
				}
				identifier, identifierOK := left.(*ast.Ident)
				literal, literalOK := typed.Rhs[index].(*ast.CompositeLit)
				if identifierOK && literalOK {
					tables[identifier.Name] = literal
				}
			}
		}
		return true
	})
	return tables
}

func fixtureImportPath(file *ast.File, localName string) string {
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == localName {
			return importPath
		}
	}
	return ""
}

func fixtureAuthorityCallCandidate(
	path string,
	fset *token.FileSet,
	file *ast.File,
	call *ast.CallExpr,
	resolve func(ast.Expr) (string, bool),
) (FixtureCandidate, bool) {
	name, authoritative := fixtureAuthorityFunction(file, path, call.Fun)
	if !authoritative {
		return FixtureCandidate{}, false
	}
	position := fset.Position(call.Pos())
	candidate := FixtureCandidate{
		File: path, Line: position.Line, Column: position.Column,
		Location: fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
		Surface:  "go-call:" + name, RuntimeAuthoritative: true,
	}
	var predicate string
	var resolved bool
	switch name {
	case "ParsePredicate", "IsValidPredicate":
		if len(call.Args) == 0 {
			return FixtureCandidate{}, false
		}
		predicate, resolved = resolve(call.Args[0])
	case "validatePredicateFixture":
		if len(call.Args) != 3 {
			return FixtureCandidate{}, false
		}
		parts := make([]string, 0, 3)
		resolved = true
		for _, argument := range call.Args {
			part, ok := resolve(argument)
			if !ok {
				resolved = false
				break
			}
			parts = append(parts, part)
		}
		predicate = strings.Join(parts, ".")
	default:
		return FixtureCandidate{}, false
	}
	if !resolved {
		return candidate, true
	}
	candidate.Predicate = predicate
	return candidate, true
}

func fixtureAuthorityFunction(file *ast.File, path string, function ast.Expr) (string, bool) {
	allowed := func(name string) bool {
		return name == "ParsePredicate" || name == "IsValidPredicate"
	}
	switch typed := function.(type) {
	case *ast.Ident:
		if typed.Name == "validatePredicateFixture" && file.Name.Name == "semantictest" &&
			pathHasPackage(path, "internal/semantictest") {
			return typed.Name, true
		}
		return typed.Name, allowed(typed.Name) && file.Name.Name == "vocabulary" && pathHasPackage(path, "vocabulary")
	case *ast.SelectorExpr:
		identifier, ok := typed.X.(*ast.Ident)
		if !ok || !allowed(typed.Sel.Name) {
			return "", false
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || importPath != "github.com/c360studio/semstreams/vocabulary" {
				continue
			}
			importName := "vocabulary"
			if spec.Name != nil {
				importName = spec.Name.Name
			}
			if importName == identifier.Name {
				return typed.Sel.Name, true
			}
		}
	}
	return "", false
}

// fixtureNegativePartsCandidates inventories the explicit three positions in
// the semantictest helper's own negative table. The authoritative reason field
// makes this structural rather than a guess over arbitrary [3]string values.
func fixtureNegativePartsCandidates(path string, fset *token.FileSet, file *ast.File) []FixtureCandidate {
	var out []FixtureCandidate
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		var partsExpression ast.Expr
		hasPredicateReason := false
		for _, element := range literal.Elts {
			keyValue, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := keyValue.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "parts":
				partsExpression = keyValue.Value
			case "wantReason":
				hasPredicateReason = strings.HasPrefix(fixtureExpressionName(keyValue.Value), "PredicateReason")
			}
		}
		if partsExpression == nil || !hasPredicateReason {
			return true
		}
		partsLiteral, ok := partsExpression.(*ast.CompositeLit)
		if !ok || len(partsLiteral.Elts) != 3 {
			return true
		}
		parts := make([]string, 0, 3)
		for _, element := range partsLiteral.Elts {
			part, ok := stringLiteral(element)
			if !ok {
				return true
			}
			parts = append(parts, part)
		}
		position := fset.Position(partsExpression.Pos())
		out = append(out, FixtureCandidate{
			File: path, Line: position.Line, Column: position.Column,
			Location:  fmt.Sprintf("line:%d:column:%d", position.Line, position.Column),
			Predicate: strings.Join(parts, "."), Surface: "go-negative-predicate-parts",
		})
		return true
	})
	return out
}

func fixtureExpressionName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

// fixtureDescriptiveStringPositions prevents a test-case label that merely
// quotes malformed syntax from becoming a semantic candidate. Only the
// conventional table-test `name` field is descriptive; expected/input/value
// fields remain audited because they can model runtime contracts.
func fixtureDescriptiveStringPositions(fset *token.FileSet, file *ast.File) map[string]bool {
	positions := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		keyValue, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		identifier, ok := keyValue.Key.(*ast.Ident)
		if !ok || identifier.Name != "name" {
			return true
		}
		literal, ok := keyValue.Value.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		position := fset.Position(literal.Pos())
		positions[fmt.Sprintf("%d:%d", position.Line, position.Column)] = true
		return true
	})
	return positions
}

func fixtureGoResolver(path string, file *ast.File, symbols *goSymbols) func(ast.Expr) (string, bool) {
	imports := make(map[string]string)
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		localName := filepath.Base(importPath)
		if spec.Name != nil {
			localName = spec.Name.Name
		}
		if localName == "." || localName == "_" {
			continue
		}
		imports[localName] = filepath.Base(importPath)
	}
	var resolve func(ast.Expr) (string, bool)
	resolve = func(expr ast.Expr) (string, bool) {
		if value, ok := stringLiteral(expr); ok {
			return value, true
		}
		switch typed := expr.(type) {
		case *ast.Ident:
			value, ok := symbols.byFile[path][typed.Name]
			if !ok {
				value, ok = symbols.byDirectory[filepath.Clean(filepath.Dir(path))][typed.Name]
			}
			return value, ok
		case *ast.SelectorExpr:
			identifier, ok := typed.X.(*ast.Ident)
			if !ok {
				return "", false
			}
			packageName, imported := imports[identifier.Name]
			if !imported {
				return "", false
			}
			value, ok := symbols.byPackage[packageName][typed.Sel.Name]
			return value, ok
		case *ast.BinaryExpr:
			if typed.Op != token.ADD {
				return "", false
			}
			left, leftOK := resolve(typed.X)
			right, rightOK := resolve(typed.Y)
			return left + right, leftOK && rightOK
		case *ast.CallExpr:
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok {
				return "", false
			}
			packageName, ok := selector.X.(*ast.Ident)
			if !ok || packageName.Name != "strings" {
				return "", false
			}
			switch selector.Sel.Name {
			case "Repeat":
				if len(typed.Args) != 2 {
					return "", false
				}
				value, valueOK := resolve(typed.Args[0])
				count, countOK := fixtureIntegerValue(typed.Args[1])
				if !valueOK || !countOK || count < 0 || count > 4096 {
					return "", false
				}
				return strings.Repeat(value, count), true
			case "Join":
				if len(typed.Args) != 2 {
					return "", false
				}
				literal, ok := typed.Args[0].(*ast.CompositeLit)
				if !ok {
					return "", false
				}
				parts := make([]string, 0, len(literal.Elts))
				for _, element := range literal.Elts {
					part, ok := resolve(element)
					if !ok {
						return "", false
					}
					parts = append(parts, part)
				}
				separator, ok := resolve(typed.Args[1])
				if !ok {
					return "", false
				}
				return strings.Join(parts, separator), true
			}
			return "", false
		default:
			return "", false
		}
	}
	return resolve
}

func fixtureIntegerValue(expression ast.Expr) (int, bool) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.INT {
			return 0, false
		}
		value, err := strconv.Atoi(typed.Value)
		return value, err == nil
	case *ast.Ident:
		switch typed.Name {
		case "MaxPredicateSegmentBytes":
			return vocabulary.MaxPredicateSegmentBytes, true
		case "MaxPredicateBytes":
			return vocabulary.MaxPredicateBytes, true
		default:
			return 0, false
		}
	case *ast.BinaryExpr:
		left, leftOK := fixtureIntegerValue(typed.X)
		right, rightOK := fixtureIntegerValue(typed.Y)
		if !leftOK || !rightOK {
			return 0, false
		}
		switch typed.Op {
		case token.ADD:
			return left + right, true
		case token.SUB:
			return left - right, true
		}
	}
	return 0, false
}

func validateSemanticTestPredicateImport(file *ast.File, path string) (bool, error) {
	if file.Name.Name == "semantictest" && pathHasPackage(path, "internal/semantictest") &&
		(goFileDeclaresName(file, "Predicate", true) || goFileImportDeclaresName(file, "Predicate")) {
		return false, fmt.Errorf("%s: declaration shadows the package semantictest.Predicate helper", path)
	}
	importsHelper := false
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != semanticTestImportPath {
			continue
		}
		importsHelper = true
		if spec.Name != nil {
			return true, fmt.Errorf("%s: internal/semantictest must use its canonical unaliased import name", path)
		}
	}
	if importsHelper && goFileDeclaresName(file, "semantictest", false) {
		return true, fmt.Errorf("%s: declaration shadows the canonical internal/semantictest import name", path)
	}
	return importsHelper, nil
}

func isSemanticTestPredicateCall(function ast.Expr, packageName, path string, importsSemanticTest bool) bool {
	switch typed := function.(type) {
	case *ast.SelectorExpr:
		identifier, ok := typed.X.(*ast.Ident)
		return importsSemanticTest && ok && identifier.Name == "semantictest" && typed.Sel.Name == "Predicate"
	case *ast.Ident:
		return typed.Name == "Predicate" && packageName == "semantictest" && pathHasPackage(path, "internal/semantictest")
	default:
		return false
	}
}

func loadSourceFixtureAnnotations(
	path string,
	fset *token.FileSet,
	groups []*ast.CommentGroup,
) ([]sourceFixtureClassification, []sourceFixtureDisposition, error) {
	var classifications []sourceFixtureClassification
	var dispositions []sourceFixtureDisposition
	for _, group := range groups {
		for _, comment := range group.List {
			invalidMarker := strings.Index(comment.Text, FixtureInvalidMarker)
			unrelatedMarker := strings.Index(comment.Text, FixtureUnrelatedMarker)
			if invalidMarker < 0 && unrelatedMarker < 0 {
				continue
			}
			line := fset.Position(comment.Pos()).Line
			if !strings.HasPrefix(comment.Text, "//") {
				return nil, nil, fmt.Errorf("%s:%d: predicate source annotation must use a same-line // comment", path, line)
			}
			if invalidMarker >= 0 && unrelatedMarker >= 0 {
				return nil, nil, fmt.Errorf("%s:%d: predicate occurrence cannot have both invalid and unrelated annotations", path, line)
			}
			if invalidMarker >= 0 {
				payload := strings.TrimSpace(comment.Text[invalidMarker+len(FixtureInvalidMarker):])
				var classification sourceFixtureClassification
				if err := decodeExactJSON([]byte(payload), &classification); err != nil {
					return nil, nil, fmt.Errorf("%s:%d: decode predicate invalid classification: %w", path, line, err)
				}
				if classification.Kind == "" || classification.Value == nil || classification.Reason == "" {
					return nil, nil, fmt.Errorf(
						"%s:%d: predicate invalid classification must set kind, value, and reason",
						path,
						line,
					)
				}
				classification.File = path
				classification.Line = line
				classifications = append(classifications, classification)
				continue
			}

			payload := strings.TrimSpace(comment.Text[unrelatedMarker+len(FixtureUnrelatedMarker):])
			var disposition sourceFixtureDisposition
			if err := decodeUnrelatedDispositionJSON([]byte(payload), &disposition); err != nil {
				return nil, nil, fmt.Errorf("%s:%d: decode predicate unrelated disposition: %w", path, line, err)
			}
			if disposition.Column < 1 || disposition.Surface == "" || disposition.Value == nil ||
				strings.TrimSpace(disposition.Basis) == "" {
				return nil, nil, fmt.Errorf(
					"%s:%d: predicate unrelated disposition must set a positive column, surface, value, and non-blank basis",
					path,
					line,
				)
			}
			if len(disposition.Basis) > maxFixtureDispositionBasis {
				return nil, nil, fmt.Errorf(
					"%s:%d: predicate unrelated disposition basis exceeds %d bytes",
					path,
					line,
					maxFixtureDispositionBasis,
				)
			}
			if !eligibleSourceFixtureDispositionSurface(disposition.Surface) {
				return nil, nil, fmt.Errorf(
					"%s:%d: predicate unrelated disposition surface %q is not an ambiguous name-derived Go surface",
					path,
					line,
					disposition.Surface,
				)
			}
			disposition.File = path
			disposition.Line = line
			dispositions = append(dispositions, disposition)
		}
	}
	return classifications, dispositions, nil
}

func eligibleSourceFixtureDispositionSurface(surface string) bool {
	for _, prefix := range []string{"go-field:", "go-assignment:", "go-declaration:"} {
		if strings.HasPrefix(surface, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(surface, prefix)) != ""
		}
	}
	return false
}

func decodeUnrelatedDispositionJSON(data []byte, disposition *sourceFixtureDisposition) error {
	value, err := decodeOrderedJSON(data)
	if err != nil {
		return err
	}
	object, ok := value.(orderedJSONObject)
	if !ok {
		return errors.New("predicate unrelated disposition must be a JSON object")
	}
	seen := make(map[string]struct{}, len(object))
	for _, entry := range object {
		if _, duplicate := seen[entry.Key]; duplicate {
			return fmt.Errorf("duplicate JSON member %q", entry.Key)
		}
		seen[entry.Key] = struct{}{}
	}
	return decodeExactJSON(data, disposition)
}

func auditFixtureStructured(path string) ([]FixtureCandidate, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return auditFixtureJSON(path)
	case ".jsonl", ".ndjson":
		return auditFixtureJSONL(path)
	case ".yaml", ".yml":
		return auditFixtureYAML(path)
	default:
		return auditFixtureText(path)
	}
}

func auditFixtureJSON(path string) ([]FixtureCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := decodeOrderedJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse JSON fixture %s: %w", path, err)
	}
	counter := make(structuredOccurrenceCounter)
	return walkFixtureValue(path, value, "", "", counter, 1, 0), nil
}

func auditFixtureJSONL(path string) ([]FixtureCandidate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []FixtureCandidate
	counter := make(structuredOccurrenceCounter)
	scanner := bufio.NewScanner(file)
	record := 0
	line := 0
	for scanner.Scan() {
		line++
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		record++
		value, err := decodeOrderedJSON(scanner.Bytes())
		if err != nil {
			return nil, fmt.Errorf("parse JSONL fixture %s record %d: %w", path, record, err)
		}
		extracted := walkFixtureValue(path, value, "", "", counter, 1, record)
		for i := range extracted {
			extracted[i].Line = line
		}
		out = append(out, extracted...)
	}
	return out, scanner.Err()
}

func auditFixtureYAML(path string) ([]FixtureCandidate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	var out []FixtureCandidate
	counter := make(structuredOccurrenceCounter)
	for document := 1; ; document++ {
		var root yaml.Node
		if err := decoder.Decode(&root); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("parse YAML fixture %s document %d: %w", path, document, err)
		}
		if len(root.Content) == 0 {
			continue
		}
		walkFixtureYAMLNode(
			path,
			&root,
			"",
			"",
			&out,
			counter,
			document,
			0,
			nil,
			make(map[*yaml.Node]bool),
		)
	}
	return out, nil
}

func auditFixtureText(path string) ([]FixtureCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []FixtureCandidate
	counter := make(structuredOccurrenceCounter)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		for _, indexed := range structuredRE.FindAllStringSubmatchIndex(text, -1) {
			value := text[indexed[2]:indexed[3]]
			column := indexed[2] + 1
			out = append(out, FixtureCandidate{
				File: path, Line: line, Column: column, Predicate: value, Surface: "structured-text",
				Location: fmt.Sprintf("/line/%d/column/%d/structured-text", line, column),
				Document: 1, Record: 0,
				Occurrence: counter.occurrence(1, 0, fmt.Sprintf("/line/%d/column/%d/structured-text", line, column)),
			})
		}
		for _, indexed := range substitutionRE.FindAllStringSubmatchIndex(text, -1) {
			value := fixtureSubstitutionPredicate(text[indexed[2]:indexed[3]])
			column := indexed[2] + 1
			out = append(out, FixtureCandidate{
				File: path, Line: line, Column: column, Predicate: value, Surface: "structured-substitution",
				Location: fmt.Sprintf("/line/%d/column/%d/structured-substitution", line, column),
				Document: 1, Record: 0,
				Occurrence: counter.occurrence(1, 0, fmt.Sprintf("/line/%d/column/%d/structured-substitution", line, column)),
			})
		}
	}
	return out, scanner.Err()
}

func fixtureSubstitutionPredicate(value string) string {
	value = strings.TrimSuffix(value, ".length")
	value = strings.TrimSuffix(value, ".triples")
	if candidate := strings.TrimSuffix(value, ".value"); candidate != value {
		if _, err := vocabulary.ParsePredicate(candidate); err == nil {
			return candidate
		}
	}
	return value
}

type orderedJSONEntry struct {
	Key   string
	Value any
}

type orderedJSONObject []orderedJSONEntry
type orderedJSONArray []any
type structuredOccurrenceCounter map[string]int

func (counter structuredOccurrenceCounter) occurrence(document, record int, location string) int {
	key := fmt.Sprintf("%d:%d:%s", document, record, location)
	counter[key]++
	return counter[key]
}

func decodeOrderedJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	value, err := decodeOrderedJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected trailing JSON value")
		}
		return nil, err
	}
	return value, nil
}

func decodeOrderedJSONValue(decoder *json.Decoder) (any, error) {
	tokenValue, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := tokenValue.(json.Delim)
	if !isDelimiter {
		return tokenValue, nil
	}
	switch delimiter {
	case '{':
		var object orderedJSONObject
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("JSON object key has type %T", keyToken)
			}
			value, err := decodeOrderedJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object = append(object, orderedJSONEntry{Key: key, Value: value})
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		var array orderedJSONArray
		for decoder.More() {
			value, err := decodeOrderedJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func walkFixtureValue(
	path string,
	value any,
	key string,
	pointer string,
	counter structuredOccurrenceCounter,
	document int,
	record int,
) []FixtureCandidate {
	var out []FixtureCandidate
	switch typed := value.(type) {
	case orderedJSONObject:
		for _, entry := range typed {
			out = append(out, walkFixtureValue(
				path,
				entry.Value,
				entry.Key,
				joinJSONPointer(pointer, entry.Key),
				counter,
				document,
				record,
			)...)
		}
	case orderedJSONArray:
		for index, child := range typed {
			out = append(out, walkFixtureValue(
				path,
				child,
				key,
				joinJSONPointer(pointer, strconv.Itoa(index)),
				counter,
				document,
				record,
			)...)
		}
	case string:
		occurrence := 0
		occurrenceForPhysicalValue := func() int {
			if occurrence == 0 {
				occurrence = counter.occurrence(document, record, pointer)
			}
			return occurrence
		}
		if isPredicateConfigKey(key) && !strings.HasPrefix(typed, "$") {
			out = append(out, FixtureCandidate{
				File: path, Predicate: typed, Surface: "config:" + key, Location: pointer,
				Document: document, Record: record, Occurrence: occurrenceForPhysicalValue(),
			})
		}
		for _, predicate := range substitutionCandidates(typed) {
			out = append(out, FixtureCandidate{
				File: path, Predicate: predicate, Surface: "config-substitution", Location: pointer,
				Document: document, Record: record, Occurrence: occurrenceForPhysicalValue(),
			})
		}
	}
	return out
}

func walkFixtureYAMLNode(
	path string,
	node *yaml.Node,
	key string,
	pointer string,
	out *[]FixtureCandidate,
	counter structuredOccurrenceCounter,
	document int,
	record int,
	usePosition *yaml.Node,
	aliasStack map[*yaml.Node]bool,
) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			walkFixtureYAMLNode(path, child, key, pointer, out, counter, document, record, usePosition, aliasStack)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			childKey := node.Content[i].Value
			walkFixtureYAMLNode(
				path,
				node.Content[i+1],
				childKey,
				joinJSONPointer(pointer, childKey),
				out,
				counter,
				document,
				record,
				usePosition,
				aliasStack,
			)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			walkFixtureYAMLNode(
				path,
				child,
				key,
				joinJSONPointer(pointer, strconv.Itoa(index)),
				out,
				counter,
				document,
				record,
				usePosition,
				aliasStack,
			)
		}
	case yaml.AliasNode:
		if node.Alias == nil || aliasStack[node.Alias] {
			return
		}
		aliasStack[node.Alias] = true
		walkFixtureYAMLNode(path, node.Alias, key, pointer, out, counter, document, record, node, aliasStack)
		delete(aliasStack, node.Alias)
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return
		}
		position := node
		if usePosition != nil {
			position = usePosition
		}
		occurrence := 0
		occurrenceForPhysicalValue := func() int {
			if occurrence == 0 {
				occurrence = counter.occurrence(document, record, pointer)
			}
			return occurrence
		}
		if isPredicateConfigKey(key) && !strings.HasPrefix(node.Value, "$") {
			*out = append(*out, FixtureCandidate{
				File: path, Line: position.Line, Column: position.Column, Predicate: node.Value,
				Surface: "config:" + key, Location: pointer,
				Document: document, Record: record, Occurrence: occurrenceForPhysicalValue(),
			})
		}
		for _, predicate := range substitutionCandidates(node.Value) {
			*out = append(*out, FixtureCandidate{
				File: path, Line: position.Line, Column: position.Column, Predicate: predicate,
				Surface: "config-substitution", Location: pointer,
				Document: document, Record: record, Occurrence: occurrenceForPhysicalValue(),
			})
		}
	}
}

func classifyFixtureDisposition(
	candidate FixtureCandidate,
	source []sourceFixtureClassification,
	dispositions []sourceFixtureDisposition,
	usedSource []int,
	usedDispositions []int,
) (bool, []FixtureFinding) {
	var matches []int
	for index, disposition := range dispositions {
		if disposition.File == candidate.File && disposition.Line == candidate.Line &&
			disposition.Column == candidate.Column && disposition.Surface == candidate.Surface {
			matches = append(matches, index)
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	for _, index := range matches {
		usedDispositions[index]++
	}
	if len(matches) > 1 {
		return true, []FixtureFinding{fixtureFinding(
			candidate,
			"duplicate-disposition",
			fmt.Sprintf("%d unrelated dispositions match this exact occurrence", len(matches)),
		)}
	}

	var conflictingClassifications []int
	for index, classification := range source {
		locationMatches := classification.Location != "" && classification.Location == candidate.Location
		lineMatches := classification.Location == "" && classification.Line == candidate.Line &&
			classification.Value != nil && *classification.Value == candidate.Predicate
		if classification.File == candidate.File && (locationMatches || lineMatches) {
			conflictingClassifications = append(conflictingClassifications, index)
		}
	}
	if len(conflictingClassifications) > 0 {
		for _, index := range conflictingClassifications {
			usedSource[index]++
		}
		return true, []FixtureFinding{fixtureFinding(
			candidate,
			"conflicting-disposition",
			"the same occurrence is marked both intentional-invalid and unrelated",
		)}
	}

	disposition := dispositions[matches[0]]
	if disposition.Value == nil || *disposition.Value != candidate.Predicate {
		return true, []FixtureFinding{fixtureFinding(
			candidate,
			"wrong-disposition-value",
			fmt.Sprintf(
				"unrelated disposition value %s, physical occurrence contains %q",
				fixtureValueDescription(disposition.Value),
				candidate.Predicate,
			),
		)}
	}
	return true, nil
}

func classifyFixtureCandidates(
	candidates []FixtureCandidate,
	source []sourceFixtureClassification,
	dispositions []sourceFixtureDisposition,
	manifest []FixtureClassificationEntry,
) FixtureAuditResult {
	result := FixtureAuditResult{Candidates: candidates, Classifications: len(source) + len(manifest)}
	usedSource := make([]int, len(source))
	usedDispositions := make([]int, len(dispositions))
	usedManifest := make([]int, len(manifest))

	for _, candidate := range candidates {
		if handled, findings := classifyFixtureDisposition(
			candidate, source, dispositions, usedSource, usedDispositions,
		); handled {
			result.Findings = append(result.Findings, findings...)
			continue
		}

		if candidate.Unresolved {
			code := "unresolved-go-surface"
			message := "known predicate surface is not a statically resolved exact occurrence"
			if strings.Contains(candidate.Surface, "semantictest.Predicate:value") {
				code = "helper-alias"
				message = "semantictest.Predicate may not be taken as a function value or alias"
			} else if strings.Contains(candidate.Surface, "semantictest.Predicate:wrapper") {
				code = "helper-wrapper"
				message = "semantictest.Predicate may not be hidden behind a wrapper"
			}
			result.Findings = append(result.Findings, fixtureFinding(candidate, code, message))
			continue
		}
		if strings.HasSuffix(candidate.Surface, ":wrong-arity") {
			result.Findings = append(result.Findings, fixtureFinding(candidate, "helper-arity", "semantictest.Predicate must receive testing.TB plus three explicit positions"))
			continue
		}
		if candidate.RuntimeAuthoritative && candidate.Predicate == "" {
			continue
		}
		_, err := vocabulary.ParsePredicate(candidate.Predicate)
		if err == nil {
			continue
		}
		reason, ok := predicateReason(err)
		if !ok {
			result.Findings = append(result.Findings, fixtureFinding(candidate, "authority-error", err.Error()))
			continue
		}

		var matches []int
		if strings.HasSuffix(candidate.File, ".go") {
			for index, classification := range source {
				locationMatches := classification.Location != "" && classification.Location == candidate.Location
				lineMatches := classification.Location == "" && classification.Line == candidate.Line &&
					classification.Value != nil && *classification.Value == candidate.Predicate
				if classification.File == candidate.File && (locationMatches || lineMatches) {
					matches = append(matches, index)
				}
			}
			if len(matches) == 1 {
				usedSource[matches[0]]++
				classification := source[matches[0]]
				if classification.Value == nil || *classification.Value != candidate.Predicate {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-value", fmt.Sprintf("classification value %s, physical occurrence contains %q", fixtureValueDescription(classification.Value), candidate.Predicate)))
					continue
				}
				if classification.Kind != FixtureStoredPredicateKind {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-kind", fmt.Sprintf("classification kind %q, want %q", classification.Kind, FixtureStoredPredicateKind)))
					continue
				}
				if classification.Reason != string(reason) {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-reason", fmt.Sprintf("classification reason %q, authority returned %q", classification.Reason, reason)))
				}
				continue
			}
		} else {
			for index, classification := range manifest {
				if classification.File == candidate.File && classification.Location == candidate.Location &&
					classification.Document != nil && *classification.Document == candidate.Document &&
					classification.Record != nil && *classification.Record == candidate.Record &&
					classification.Occurrence != nil && *classification.Occurrence == candidate.Occurrence {
					matches = append(matches, index)
				}
			}
			if len(matches) == 1 {
				usedManifest[matches[0]]++
				classification := manifest[matches[0]]
				if classification.Value == nil || *classification.Value != candidate.Predicate {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-value", fmt.Sprintf("classification value %s, physical occurrence contains %q", fixtureValueDescription(classification.Value), candidate.Predicate)))
					continue
				}
				if classification.Kind != FixtureStoredPredicateKind {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-kind", fmt.Sprintf("classification kind %q, want %q", classification.Kind, FixtureStoredPredicateKind)))
					continue
				}
				if classification.Reason != string(reason) {
					result.Findings = append(result.Findings, fixtureFinding(candidate, "wrong-reason", fmt.Sprintf("classification reason %q, authority returned %q", classification.Reason, reason)))
				}
				continue
			}
		}
		code := "unclassified"
		message := fmt.Sprintf("intentional invalid requires one exact %s classification with reason %q", FixtureStoredPredicateKind, reason)
		if len(matches) > 1 {
			code = "duplicate-classification"
			message = fmt.Sprintf("%d classifications match this occurrence", len(matches))
		}
		result.Findings = append(result.Findings, fixtureFinding(candidate, code, message))
	}

	result.Findings = append(result.Findings, unusedSourceClassificationFindings(source, usedSource)...)
	result.Findings = append(result.Findings, unusedDispositionFindings(candidates, dispositions, usedDispositions)...)
	result.Findings = append(result.Findings, unusedManifestClassificationFindings(manifest, usedManifest)...)
	sortFixtureFindings(result.Findings)
	return result
}

func unusedSourceClassificationFindings(
	source []sourceFixtureClassification,
	used []int,
) []FixtureFinding {
	var findings []FixtureFinding
	for index, classification := range source {
		switch {
		case used[index] == 0:
			findings = append(findings, FixtureFinding{
				File: classification.File, Line: classification.Line, Predicate: fixtureValue(classification.Value),
				Code: "stale-classification", Message: "source classification does not resolve to one malformed candidate on the same line",
			})
		case used[index] > 1:
			findings = append(findings, FixtureFinding{
				File: classification.File, Line: classification.Line, Predicate: fixtureValue(classification.Value),
				Code: "broad-classification", Message: fmt.Sprintf("source classification resolves to %d candidates", used[index]),
			})
		}
	}
	return findings
}

func unusedDispositionFindings(
	candidates []FixtureCandidate,
	dispositions []sourceFixtureDisposition,
	used []int,
) []FixtureFinding {
	var findings []FixtureFinding
	for index, disposition := range dispositions {
		switch {
		case used[index] == 0:
			code := "stale-disposition"
			message := "unrelated disposition does not resolve to one exact candidate at its file, line, and column"
			var surfaces []string
			for _, candidate := range candidates {
				if candidate.File == disposition.File && candidate.Line == disposition.Line &&
					candidate.Column == disposition.Column {
					surfaces = append(surfaces, candidate.Surface)
				}
			}
			if len(surfaces) > 0 {
				sort.Strings(surfaces)
				code = "wrong-disposition-surface"
				message = fmt.Sprintf(
					"unrelated disposition surface %q, exact position contains surfaces %q",
					disposition.Surface,
					strings.Join(surfaces, ", "),
				)
			}
			findings = append(findings, FixtureFinding{
				File: disposition.File, Line: disposition.Line,
				Location:  fmt.Sprintf("line:%d:column:%d", disposition.Line, disposition.Column),
				Predicate: fixtureValue(disposition.Value), Code: code, Message: message,
			})
		case used[index] > 1:
			findings = append(findings, FixtureFinding{
				File: disposition.File, Line: disposition.Line,
				Location:  fmt.Sprintf("line:%d:column:%d", disposition.Line, disposition.Column),
				Predicate: fixtureValue(disposition.Value), Code: "broad-disposition",
				Message: fmt.Sprintf("unrelated disposition resolves to %d candidates", used[index]),
			})
		}
	}
	return findings
}

func unusedManifestClassificationFindings(
	manifest []FixtureClassificationEntry,
	used []int,
) []FixtureFinding {
	var findings []FixtureFinding
	for index, classification := range manifest {
		switch {
		case used[index] == 0:
			findings = append(findings, FixtureFinding{
				File: classification.File, Location: classification.Location,
				Document: fixtureInt(classification.Document), Record: fixtureInt(classification.Record),
				Occurrence: fixtureInt(classification.Occurrence), Predicate: fixtureValue(classification.Value),
				Code: "stale-classification", Message: "manifest classification does not resolve to one malformed candidate at the exact location",
			})
		case used[index] > 1:
			findings = append(findings, FixtureFinding{
				File: classification.File, Location: classification.Location,
				Document: fixtureInt(classification.Document), Record: fixtureInt(classification.Record),
				Occurrence: fixtureInt(classification.Occurrence), Predicate: fixtureValue(classification.Value),
				Code: "broad-classification", Message: fmt.Sprintf("manifest classification resolves to %d candidates", used[index]),
			})
		}
	}
	return findings
}

func loadFixtureClassificationManifest(path string) (FixtureClassificationManifest, error) {
	if path == "" {
		return FixtureClassificationManifest{Version: 1}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FixtureClassificationManifest{}, fmt.Errorf("read predicate fixture classifications: %w", err)
	}
	var manifest FixtureClassificationManifest
	if err := decodeExactJSON(data, &manifest); err != nil {
		return FixtureClassificationManifest{}, fmt.Errorf("decode predicate fixture classifications: %w", err)
	}
	if manifest.Version != 1 {
		return FixtureClassificationManifest{}, fmt.Errorf("predicate fixture classification manifest version = %d, want 1", manifest.Version)
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for index := range manifest.Entries {
		entry := &manifest.Entries[index]
		entry.File = filepath.ToSlash(filepath.Clean(entry.File))
		if entry.File == "." || filepath.IsAbs(entry.File) || strings.HasPrefix(entry.File, "../") {
			return FixtureClassificationManifest{}, fmt.Errorf("predicate fixture classification %d has non-repository-relative file %q", index, entry.File)
		}
		if entry.Location == "" || !strings.HasPrefix(entry.Location, "/") ||
			entry.Document == nil || *entry.Document < 1 ||
			entry.Record == nil || *entry.Record < 0 ||
			entry.Occurrence == nil || *entry.Occurrence < 1 ||
			entry.Value == nil || entry.Kind == "" || entry.Reason == "" {
			return FixtureClassificationManifest{}, fmt.Errorf(
				"predicate fixture classification %d must set an RFC6901 location, document, record, occurrence, kind, value, and reason",
				index,
			)
		}
		key := strings.Join([]string{
			entry.File,
			entry.Location,
			strconv.Itoa(*entry.Document),
			strconv.Itoa(*entry.Record),
			strconv.Itoa(*entry.Occurrence),
		}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			return FixtureClassificationManifest{}, fmt.Errorf(
				"duplicate predicate fixture classification %d for physical occurrence %s %s",
				index,
				entry.File,
				entry.Location,
			)
		}
		seen[key] = struct{}{}
	}
	dispositions := make(map[string]struct{}, len(manifest.UnrelatedArtifacts))
	for index := range manifest.UnrelatedArtifacts {
		entry := &manifest.UnrelatedArtifacts[index]
		entry.File = filepath.ToSlash(filepath.Clean(entry.File))
		if entry.File == "." || filepath.IsAbs(entry.File) || strings.HasPrefix(entry.File, "../") {
			return FixtureClassificationManifest{}, fmt.Errorf("predicate artifact disposition %d has non-repository-relative file %q", index, entry.File)
		}
		if entry.Classification != "unrelated" || strings.TrimSpace(entry.Basis) == "" {
			return FixtureClassificationManifest{}, fmt.Errorf(
				"predicate artifact disposition %d must set classification unrelated and a non-empty basis",
				index,
			)
		}
		if _, duplicate := dispositions[entry.File]; duplicate {
			return FixtureClassificationManifest{}, fmt.Errorf("duplicate predicate artifact disposition for %s", entry.File)
		}
		dispositions[entry.File] = struct{}{}
	}
	return manifest, nil
}

func classifyUnsupportedArtifacts(
	artifacts []string,
	dispositions []FixtureArtifactDisposition,
) []FixtureFinding {
	sort.Strings(artifacts)
	seenArtifacts := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		seenArtifacts[artifact] = true
	}
	dispositionByFile := make(map[string]FixtureArtifactDisposition, len(dispositions))
	for _, disposition := range dispositions {
		dispositionByFile[disposition.File] = disposition
	}
	var findings []FixtureFinding
	for _, artifact := range artifacts {
		if _, classified := dispositionByFile[artifact]; classified {
			continue
		}
		findings = append(findings, FixtureFinding{
			File: artifact, Code: "unsupported-artifact",
			Message: "tracked testdata format has no structural predicate parser or exact unrelated disposition",
		})
	}
	for _, disposition := range dispositions {
		if seenArtifacts[disposition.File] {
			continue
		}
		findings = append(findings, FixtureFinding{
			File: disposition.File, Code: "stale-artifact-disposition",
			Message: "unrelated artifact disposition does not resolve to an unsupported testdata file",
		})
	}
	return findings
}

func decodeExactJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func predicateReason(err error) (vocabulary.PredicateValidationReason, bool) {
	var validationError *vocabulary.PredicateValidationError
	if !errors.As(err, &validationError) {
		return "", false
	}
	return validationError.Reason, true
}

func fixtureFinding(candidate FixtureCandidate, code, message string) FixtureFinding {
	return FixtureFinding{
		File: candidate.File, Line: candidate.Line, Location: candidate.Location,
		Document: candidate.Document, Record: candidate.Record, Occurrence: candidate.Occurrence,
		Predicate: candidate.Predicate, Code: code, Message: message,
	}
}

func fixtureValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func fixtureValueDescription(value *string) string {
	if value == nil {
		return "<missing>"
	}
	return strconv.Quote(*value)
}

func fixtureInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeFixtureCandidates(root string, candidates []FixtureCandidate) []FixtureCandidate {
	for index := range candidates {
		candidates[index].File = normalizeFixturePath(root, candidates[index].File)
	}
	return candidates
}

func normalizeFixturePath(root, path string) string {
	rootAbs, rootErr := filepath.Abs(root)
	pathAbs, pathErr := filepath.Abs(path)
	if rootErr == nil && pathErr == nil {
		if relative, err := filepath.Rel(rootAbs, pathAbs); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(filepath.Clean(relative))
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func ignoredFixtureDir(name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func pathHasDirectory(path, wanted string) bool {
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		if part == wanted {
			return true
		}
	}
	return false
}

func pathHasPackage(path, wanted string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, wanted+"/") || strings.Contains(clean, "/"+wanted+"/")
}

func isStructuredFixtureExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".jsonl", ".ndjson", ".yaml", ".yml", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".svelte", ".graphql", ".gql", ".proto", ".cue", ".md", ".txt":
		return true
	default:
		return false
	}
}

func joinJSONPointer(base, part string) string {
	escaped := strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	if base == "" {
		return "/" + escaped
	}
	return base + "/" + escaped
}

func deduplicateFixtureCandidates(in []FixtureCandidate) []FixtureCandidate {
	seen := make(map[string]struct{}, len(in))
	out := make([]FixtureCandidate, 0, len(in))
	for _, candidate := range in {
		key := strings.Join([]string{
			candidate.File,
			candidate.Location,
			strconv.Itoa(candidate.Document),
			strconv.Itoa(candidate.Record),
			strconv.Itoa(candidate.Occurrence),
			candidate.Predicate,
			strconv.FormatBool(candidate.RuntimeAuthoritative),
			strconv.FormatBool(candidate.Unresolved),
			candidate.Surface,
		}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Location != out[j].Location {
			return out[i].Location < out[j].Location
		}
		if out[i].Document != out[j].Document {
			return out[i].Document < out[j].Document
		}
		if out[i].Record != out[j].Record {
			return out[i].Record < out[j].Record
		}
		if out[i].Occurrence != out[j].Occurrence {
			return out[i].Occurrence < out[j].Occurrence
		}
		if out[i].Predicate != out[j].Predicate {
			return out[i].Predicate < out[j].Predicate
		}
		return out[i].Surface < out[j].Surface
	})
	return out
}

func sortFixtureFindings(findings []FixtureFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Location != findings[j].Location {
			return findings[i].Location < findings[j].Location
		}
		if findings[i].Document != findings[j].Document {
			return findings[i].Document < findings[j].Document
		}
		if findings[i].Record != findings[j].Record {
			return findings[i].Record < findings[j].Record
		}
		if findings[i].Occurrence != findings[j].Occurrence {
			return findings[i].Occurrence < findings[j].Occurrence
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Predicate < findings[j].Predicate
	})
}

func goFileDeclaresName(file *ast.File, wanted string, allowPackageFunction bool) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		switch typed := node.(type) {
		case *ast.ValueSpec:
			found = identifiersInclude(typed.Names, wanted)
		case *ast.TypeSpec:
			found = typed.Name.Name == wanted
		case *ast.FuncDecl:
			declaresName := typed.Name.Name == wanted
			if allowPackageFunction && typed.Recv == nil {
				declaresName = false
			}
			found = declaresName || fieldListIncludes(typed.Recv, wanted) || fieldListIncludes(typed.Type.Params, wanted) || fieldListIncludes(typed.Type.Results, wanted)
		case *ast.FuncLit:
			found = fieldListIncludes(typed.Type.Params, wanted) || fieldListIncludes(typed.Type.Results, wanted)
		case *ast.AssignStmt:
			if typed.Tok == token.DEFINE {
				for _, expression := range typed.Lhs {
					identifier, ok := expression.(*ast.Ident)
					found = found || (ok && identifier.Name == wanted)
				}
			}
		case *ast.RangeStmt:
			if typed.Tok == token.DEFINE {
				for _, expression := range []ast.Expr{typed.Key, typed.Value} {
					identifier, ok := expression.(*ast.Ident)
					found = found || (ok && identifier.Name == wanted)
				}
			}
		}
		return !found
	})
	return found
}

func goFileImportDeclaresName(file *ast.File, wanted string) bool {
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

func identifiersInclude(identifiers []*ast.Ident, wanted string) bool {
	for _, identifier := range identifiers {
		if identifier != nil && identifier.Name == wanted {
			return true
		}
	}
	return false
}

func fieldListIncludes(list *ast.FieldList, wanted string) bool {
	if list == nil {
		return false
	}
	for _, field := range list.List {
		if identifiersInclude(field.Names, wanted) {
			return true
		}
	}
	return false
}

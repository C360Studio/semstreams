// Package modelresolveaudit is a static invariant audit that fails when a
// name-keyed model-registry lookup skips capability resolution.
//
// A loop is spawned with whatever name a rule handed it — that name can be a
// CAPABILITY (coordinator/developer/reviewer), not a concrete endpoint. A raw
// reg.GetEndpoint(name) / reg.GetMaxTokens(name) / reg.Endpoints[name] lookup
// MISSES on a capability and silently returns wrong/zero endpoint accounting,
// limits, or provider — the #584 (cost-usd) / #594 (max_tokens) bug class. The
// invariant (model/registry.go ResolveEndpointName): every name-keyed lookup on
// the accounting/limit/aux paths MUST resolve the name through
// model.ResolveEndpointName (or reg.Resolve) first.
//
// This audit is a heuristic go/ast scan over owned source. A lookup passes when
// its argument is provably a resolved name (an inline ResolveEndpointName/Resolve
// call, or a variable assigned from one earlier in the same function), or when
// the site carries an inline `// modelresolveaudit:allow <reason>` escape-hatch,
// mirroring internal/entityidaudit and internal/predicateaudit.
package modelresolveaudit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Surface labels for the three name-keyed lookup shapes the audit detects.
const (
	// SurfaceGetEndpoint marks a reg.GetEndpoint(name) call.
	SurfaceGetEndpoint = "go-call:GetEndpoint"
	// SurfaceGetMaxTokens marks a reg.GetMaxTokens(name) call.
	SurfaceGetMaxTokens = "go-call:GetMaxTokens"
	// SurfaceEndpointsIndex marks a reg.Endpoints[name] index expression.
	SurfaceEndpointsIndex = "go-index:Endpoints"

	maxAnnotationReasonBytes = 160
)

// Candidate records one structurally extracted name-keyed lookup.
type Candidate struct {
	File    string
	Line    int
	Column  int
	Surface string
	Arg     string
}

// Finding is an unresolved lookup that carries no allow annotation.
type Finding struct {
	Candidate
	Reason string
}

var allowRE = regexp.MustCompile(`modelresolveaudit:allow[[:space:]]+(.+)$`)

type site struct {
	cand     Candidate
	autoPass bool
}

type annotation struct {
	line   int
	reason string
}

// Audit walks roots and returns every extracted lookup candidate plus every
// candidate that is neither provably resolved nor covered by an allow
// annotation. A stale allow annotation (one that covers no unresolved lookup)
// is an error, keeping the escape-hatch set honest.
func Audit(roots ...string) ([]Candidate, []Finding, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	var candidates []Candidate
	var findings []Finding
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
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") ||
				isResolverHome(path) || isAuditHome(path) {
				// Skip tests (this gate protects production accounting paths;
				// test files legitimately construct registries and call
				// GetEndpoint directly for assertions), the resolver's own home
				// model/registry.go (whose primitives are the authorized
				// raw-lookup site), and this audit's own home (which names the
				// lookup methods and the allow token in prose and regexes).
				return nil
			}
			sites, err := extractSites(path)
			if err != nil {
				return err
			}
			annotations, err := loadAnnotations(path)
			if err != nil {
				return err
			}
			fileFindings, err := reconcile(path, sites, annotations)
			if err != nil {
				return err
			}
			for _, s := range sites {
				candidates = append(candidates, s.cand)
			}
			findings = append(findings, fileFindings...)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	candidates = dedupeAndSort(candidates)
	sortFindings(findings)
	return candidates, findings, nil
}

// reconcile classifies each extracted site against the allow annotations for a
// file. An unresolved site with no covering annotation becomes a finding; an
// annotation covering no unresolved site is rejected as stale.
//
// Each annotation shields exactly one line (see coverageLine): its own line
// when inline, or the line immediately below when it sits on its own line. This
// prevents an inline allow (a trailing comment on a lookup's line N) from
// silently shielding a raw lookup on line N+1.
func reconcile(path string, sites []site, annotations []annotation) ([]Finding, error) {
	siteLines := make(map[int]bool)
	unresolvedLines := make(map[int]bool)
	var unresolved []Candidate
	for _, s := range sites {
		siteLines[s.cand.Line] = true
		if !s.autoPass {
			unresolved = append(unresolved, s.cand)
			unresolvedLines[s.cand.Line] = true
		}
	}
	covered := make(map[int]bool)
	for _, a := range annotations {
		target := coverageLine(a, siteLines)
		if !unresolvedLines[target] {
			return nil, fmt.Errorf("%s:%d: stale modelresolveaudit:allow annotation covers no unresolved lookup", path, a.line)
		}
		covered[target] = true
	}
	var findings []Finding
	for _, c := range unresolved {
		if covered[c.Line] {
			continue
		}
		findings = append(findings, Finding{Candidate: c, Reason: unresolvedReason(c.Surface)})
	}
	return findings, nil
}

// coverageLine returns the single lookup line an allow annotation shields. An
// inline annotation (its own line carries a lookup site) shields ONLY that
// line; a preceding-line annotation (no lookup on its own line) shields ONLY
// the line immediately below. The split closes the adjacency-bleed hole where a
// trailing comment on line N would otherwise also shield a raw lookup on N+1.
func coverageLine(a annotation, siteLines map[int]bool) int {
	if siteLines[a.line] {
		return a.line
	}
	return a.line + 1
}

func unresolvedReason(surface string) string {
	return "unresolved capability lookup via " + surface +
		"; feed the name through model.ResolveEndpointName before the lookup (the sanctioned auto-pass)," +
		" or annotate the site with // modelresolveaudit:allow <reason>"
}

// extractSites parses one Go file and returns every name-keyed lookup, tagging
// each with whether its argument is provably a resolved endpoint name.
func extractSites(path string) ([]site, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse Go %s: %w", path, err)
	}
	var out []site
	record := func(arg ast.Expr, pos token.Pos, surface string, resolved map[string]bool) {
		position := fset.Position(pos)
		out = append(out, site{
			cand: Candidate{
				File: path, Line: position.Line, Column: position.Column,
				Surface: surface, Arg: exprText(arg),
			},
			autoPass: argIsResolved(arg, resolved),
		})
	}
	var walk func(node ast.Node, resolved map[string]bool)
	walk = func(node ast.Node, resolved map[string]bool) {
		ast.Inspect(node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				walk(x.Body, copySet(resolved))
				return false
			case *ast.AssignStmt:
				updateResolved(resolved, x)
			case *ast.CallExpr:
				if surface, arg, ok := lookupCall(x); ok {
					record(arg, x.Pos(), surface, resolved)
				}
			case *ast.IndexExpr:
				if arg, ok := endpointsIndex(x); ok {
					record(arg, x.Pos(), SurfaceEndpointsIndex, resolved)
				}
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body != nil {
				walk(fn.Body, map[string]bool{})
			}
			continue
		}
		walk(decl, map[string]bool{})
	}
	return out, nil
}

// lookupCall reports whether call is a name-keyed GetEndpoint/GetMaxTokens
// method call and returns its surface and first argument.
func lookupCall(call *ast.CallExpr) (string, ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return "", nil, false
	}
	switch sel.Sel.Name {
	case "GetEndpoint":
		return SurfaceGetEndpoint, call.Args[0], true
	case "GetMaxTokens":
		return SurfaceGetMaxTokens, call.Args[0], true
	default:
		return "", nil, false
	}
}

// endpointsIndex reports whether idx is a reg.Endpoints[key] index expression
// and returns the index (key) expression.
func endpointsIndex(idx *ast.IndexExpr) (ast.Expr, bool) {
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Endpoints" {
		return nil, false
	}
	return idx.Index, true
}

// argIsResolved reports whether a lookup argument is provably a resolved
// endpoint name: an inline ResolveEndpointName/Resolve call, or an identifier
// assigned from one earlier in the enclosing function.
func argIsResolved(arg ast.Expr, resolved map[string]bool) bool {
	switch a := arg.(type) {
	case *ast.CallExpr:
		return isResolverCall(a)
	case *ast.Ident:
		return resolved[a.Name]
	default:
		return false
	}
}

// isResolverCall reports whether call is a model.ResolveEndpointName invocation
// — the ONLY sanctioned name-keyed resolution primitive. Bare reg.Resolve is
// deliberately NOT auto-passed: Resolve maps a DIRECT endpoint name to
// Defaults.Model (a different wrong-endpoint bug the audit must not bless), and
// the bare method name also over-matches unrelated packages (graph.Resolve,
// etc.). A legitimate reg.Resolve(capability) site must carry an explicit
// annotation asserting its input is provably a capability.
func isResolverCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "ResolveEndpointName"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "ResolveEndpointName"
	default:
		return false
	}
}

// updateResolved records identifiers assigned from a resolver call and drops
// identifiers reassigned to any other value, so resolution is tracked in source
// order within a function scope.
func updateResolved(resolved map[string]bool, assign *ast.AssignStmt) {
	for i, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		if i < len(assign.Rhs) {
			if call, isCall := assign.Rhs[i].(*ast.CallExpr); isCall && isResolverCall(call) {
				resolved[ident.Name] = true
				continue
			}
		}
		delete(resolved, ident.Name)
	}
}

func copySet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// exprText renders a lookup argument for diagnostics. It is informational only.
func exprText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprText(e.Fun) + "(...)"
	case *ast.BasicLit:
		return e.Value
	default:
		return "<expr>"
	}
}

// loadAnnotations extracts every `modelresolveaudit:allow <reason>` comment with
// its source line.
func loadAnnotations(path string) ([]annotation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go annotations %s: %w", path, err)
	}
	var annotations []annotation
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !strings.Contains(comment.Text, "modelresolveaudit:allow") {
				continue
			}
			line := fset.Position(comment.Pos()).Line
			match := allowRE.FindStringSubmatch(comment.Text)
			if match == nil {
				return nil, fmt.Errorf("%s:%d: malformed modelresolveaudit:allow annotation", path, line)
			}
			reason := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(match[1]), "*/"))
			if reason == "" || len(reason) > maxAnnotationReasonBytes {
				return nil, fmt.Errorf("%s:%d: modelresolveaudit:allow reason must be 1-%d bytes", path, line, maxAnnotationReasonBytes)
			}
			annotations = append(annotations, annotation{line: line, reason: reason})
		}
	}
	return annotations, nil
}

func ignoredDir(name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build", "testdata":
		return true
	default:
		return false
	}
}

// isResolverHome reports whether path is model/registry.go, the resolver's own
// home. Its primitives (ResolveEndpoint, ResolveEndpointName,
// ResolveCapabilityTimeout, GetMaxTokens, Resolve, buildChain) legitimately do
// the raw Endpoints[]/GetEndpoint lookups this audit guards elsewhere.
func isResolverHome(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == "model/registry.go" || strings.HasSuffix(clean, "/model/registry.go")
}

// isAuditHome reports whether path is under this audit's own package. Its
// source names the guarded lookup methods and the allow token in doc comments
// and regexes, which would otherwise self-trip the scan.
func isAuditHome(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(clean, "internal/modelresolveaudit/") ||
		strings.HasSuffix(clean, "internal/modelresolveaudit")
}

func dedupeAndSort(in []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(in))
	out := make([]Candidate, 0, len(in))
	for _, candidate := range in {
		key := fmt.Sprintf("%s:%d:%d:%s:%s", candidate.File, candidate.Line, candidate.Column, candidate.Surface, candidate.Arg)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return lessCandidate(out[i], out[j]) })
	return out
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool { return lessCandidate(findings[i].Candidate, findings[j].Candidate) })
}

func lessCandidate(a, b Candidate) bool {
	switch {
	case a.File != b.File:
		return a.File < b.File
	case a.Line != b.Line:
		return a.Line < b.Line
	case a.Column != b.Column:
		return a.Column < b.Column
	case a.Surface != b.Surface:
		return a.Surface < b.Surface
	default:
		return a.Arg < b.Arg
	}
}

package testinfra_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type cleanupSelection struct {
	Name       string
	BuildFlags []string
}

type cleanupSite struct {
	Path            string   `json:"path"`
	StartOffset     int      `json:"-"`
	EndOffset       int      `json:"-"`
	DeclarationOnly bool     `json:"-"`
	Line            int      `json:"line"`
	Function        string   `json:"function"`
	Origin          string   `json:"origin"`
	Target          string   `json:"target"`
	Receiver        string   `json:"receiver,omitempty"`
	Context         string   `json:"context"`
	Classification  string   `json:"classification"`
	Applicability   string   `json:"applicability"`
	Identity        string   `json:"identity"`
	Fingerprint     string   `json:"fingerprint"`
	Reason          string   `json:"reason,omitempty"`
	Selections      []string `json:"selections"`
}

type cleanupReport struct {
	Sources           int            `json:"sources"`
	TypedSources      int            `json:"typed_sources"`
	SelectionPackages map[string]int `json:"selection_packages"`
	SelectionFiles    map[string]int `json:"selection_files"`
	NestedModules     []string       `json:"nested_modules,omitempty"`
	Exclusions        []string       `json:"exclusions,omitempty"`
	Sites             []cleanupSite  `json:"sites"`
}

type cleanupBaseline struct {
	Version     int                 `json:"version"`
	Entries     []cleanupApproval   `json:"entries"`
	Resolutions []cleanupResolution `json:"resolutions,omitempty"`
}

type cleanupApproval struct {
	Identity       string `json:"identity"`
	Fingerprint    string `json:"fingerprint"`
	Classification string `json:"classification"`
	Reason         string `json:"reason"`
	OwnerIssue     string `json:"owner_issue"`
}

type cleanupResolution struct {
	Identity        string              `json:"identity"`
	SiteFingerprint string              `json:"site_fingerprint"`
	Classification  string              `json:"classification"`
	Applicability   string              `json:"applicability,omitempty"`
	Question        string              `json:"question"`
	Reason          string              `json:"reason"`
	Reviewer        string              `json:"reviewer"`
	OwnerIssue      string              `json:"owner_issue"`
	Dependencies    []cleanupDependency `json:"dependencies"`
}

type cleanupDependency struct {
	Path        string `json:"path,omitempty"`
	Declaration string `json:"declaration,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Symbol      string `json:"symbol,omitempty"`
	Module      string `json:"module,omitempty"`
	Version     string `json:"version,omitempty"`
}

const (
	cleanupUnbounded    = "unbounded-terminal-cleanup"
	cleanupBounded      = "bounded-cleanup"
	cleanupContract     = "deliberate-contract-call"
	cleanupNonLifecycle = "non-lifecycle-stop"
	cleanupUnknown      = "uncertain-owner-provenance"
)

func defaultCleanupSelections() []cleanupSelection {
	return []cleanupSelection{
		{Name: "default"},
		{Name: "integration", BuildFlags: []string{"-tags=integration"}},
		{Name: "live_llm", BuildFlags: []string{"-tags=live_llm"}},
	}
}

func analyzeCleanupRoots(ctx context.Context, root string, baselinePath string, selections []cleanupSelection) (cleanupReport, error) {
	report := cleanupReport{SelectionPackages: make(map[string]int), SelectionFiles: make(map[string]int)}
	if ctx == nil {
		return report, errors.New("cleanup guard: nil context")
	}
	if len(selections) == 0 {
		selections = defaultCleanupSelections()
	}
	sourcePaths, err := cleanupGitSources(ctx, root)
	if err != nil {
		return report, err
	}
	report.Sources = len(sourcePaths)
	modulePath, err := cleanupModulePath(root)
	if err != nil {
		return report, err
	}
	parsed := make(map[string]*ast.File, len(sourcePaths))
	for _, rel := range sourcePaths {
		path := filepath.Join(root, rel)
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if parseErr != nil {
			return report, fmt.Errorf("parse %s: %w", rel, parseErr)
		}
		parsed[rel] = f
	}
	modules := make(map[string]string)
	replacedModules := make(map[string]bool)
	typed := make(map[string]bool)
	eligible := make(map[string]bool)
	physical := make(map[string]*cleanupSite)
	for _, selection := range selections {
		if err := scanCleanupSelection(ctx, root, modulePath, selection, &report, parsed, modules, replacedModules, typed, eligible, physical); err != nil {
			return report, err
		}
	}
	report.TypedSources = len(typed)
	for _, rel := range sourcePaths {
		if typed[rel] {
			continue
		}
		if nested := cleanupNestedModule(root, rel); nested != "" {
			report.NestedModules = appendUniqueSelection(report.NestedModules, nested)
		}
		if cleanupHasCandidate(parsed[rel]) {
			report.Sites = append(report.Sites, cleanupSite{
				Path: rel, Function: "<untyped>", Origin: "unrepresented-source",
				Context: "unknown", Classification: cleanupUnknown, Applicability: "unresolved",
				Reason:   "candidate-bearing file has no type evidence in selected builds",
				Identity: rel + "|unrepresented-source", Fingerprint: cleanupHash(rel),
			})
		} else {
			report.Exclusions = append(report.Exclusions, rel+": no candidate in selected build")
		}
	}
	sort.Strings(report.NestedModules)
	for _, site := range physical {
		// Declaration scans inventory a possible site. Once typed invocation paths
		// reach it, retain those concrete ownership/provenance variants instead.
		if site.DeclarationOnly {
			reached := false
			for _, other := range physical {
				if !other.DeclarationOnly && other.Path == site.Path && other.StartOffset == site.StartOffset && other.EndOffset == site.EndOffset {
					reached = true
					break
				}
			}
			if reached {
				continue
			}
		}
		report.Sites = append(report.Sites, *site)
		eligible[site.Path] = true
	}
	for _, rel := range sourcePaths {
		if typed[rel] && !eligible[rel] {
			report.Exclusions = append(report.Exclusions, rel+": typed source outside test-support cleanup paths")
		}
	}
	sort.Slice(report.Sites, func(i, j int) bool {
		a, b := report.Sites[i], report.Sites[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.StartOffset != b.StartOffset {
			return a.StartOffset < b.StartOffset
		}
		if a.EndOffset != b.EndOffset {
			return a.EndOffset < b.EndOffset
		}
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		if a.Fingerprint != b.Fingerprint {
			return a.Fingerprint < b.Fingerprint
		}
		if a.Applicability != b.Applicability {
			return a.Applicability < b.Applicability
		}
		return !a.DeclarationOnly && b.DeclarationOnly
	})
	counts := map[string]int{}
	for i := range report.Sites {
		site := &report.Sites[i]
		base := strings.Join([]string{site.Path, site.Function, site.Origin, site.Target, site.Receiver, site.Context}, "|")
		counts[base]++
		site.Identity = base + "|" + strconv.Itoa(counts[base])
		sort.Strings(site.Selections)
	}
	if err := reconcileCleanupBaseline(&report, baselinePath, root, parsed, modules, replacedModules); err != nil {
		return report, err
	}
	return report, nil
}

func scanCleanupSelection(ctx context.Context, root, modulePath string, selection cleanupSelection, report *cleanupReport, parsed map[string]*ast.File, modules map[string]string, replacedModules map[string]bool, typed, eligible map[string]bool, physical map[string]*cleanupSite) error {
	cfg := &packages.Config{
		Context: ctx, Dir: root, Tests: true,
		BuildFlags: selection.BuildFlags,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
	}
	loaded, loadErr := packages.Load(cfg, "./...")
	report.SelectionPackages[selection.Name] = len(loaded)
	if loadErr != nil {
		return fmt.Errorf("load %s packages: %w", selection.Name, loadErr)
	}
	// Keep typed syntax and TypeInfo confined to selected root packages.
	// A separate metadata load retains the complete selected dependency graph
	// needed to validate exact manual module dependencies and replacements.
	metadataCfg := &packages.Config{
		Context: ctx, Dir: root, Tests: true, BuildFlags: selection.BuildFlags,
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
	}
	metadata, metadataErr := packages.Load(metadataCfg, "./...")
	if metadataErr != nil {
		return fmt.Errorf("load %s module metadata: %w", selection.Name, metadataErr)
	}
	for _, pkg := range metadata {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("module metadata %s %s: %s", selection.Name, pkg.PkgPath, pkg.Errors[0])
		}
	}
	selectionFiles := make(map[string]bool)
	packages.Visit(metadata, nil, func(pkg *packages.Package) {
		if pkg.Module != nil {
			modules[pkg.Module.Path] = pkg.Module.Version
			if pkg.Module.Replace != nil {
				replacedModules[pkg.Module.Path] = true
			}
		}
	})
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("type-check %s %s: %s", selection.Name, pkg.PkgPath, pkg.Errors[0])
		}
		helpers := make(map[types.Object]*ast.FuncDecl)
		for _, syntax := range pkg.Syntax {
			for _, decl := range syntax.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					helpers[pkg.TypesInfo.Defs[fn.Name]] = fn
				}
			}
		}
		for i, file := range pkg.Syntax {
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}
			rel, relErr := filepath.Rel(root, pkg.CompiledGoFiles[i])
			if relErr != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			rel = filepath.ToSlash(rel)
			if _, ok := parsed[rel]; !ok {
				continue
			}
			typed[rel] = true
			selectionFiles[rel] = true
			if !strings.HasSuffix(rel, "_test.go") && !cleanupHasTestingEntry(file, pkg.TypesInfo) {
				continue
			}
			eligible[rel] = true
			sites := scanCleanupFile(root, rel, file, pkg.Fset, pkg.TypesInfo, modulePath, helpers)
			for _, site := range sites {
				key := fmt.Sprintf("%s:%d:%d:%s:%s:%s:%s:%s:%t", site.Path, site.StartOffset, site.EndOffset,
					site.Function, site.Origin, site.Context, site.Fingerprint, site.Applicability, site.DeclarationOnly)
				if existing := physical[key]; existing != nil {
					if existing.Classification != site.Classification || existing.Context != site.Context || existing.Target != site.Target || existing.Applicability != site.Applicability {
						existing.Classification = cleanupUnknown
						existing.Applicability = "unresolved"
						existing.Reason = "conflicting typed classification across build selections"
						existing.Fingerprint = cleanupHash(existing.Fingerprint + "|" + site.Fingerprint)
					}
					existing.Selections = appendUniqueSelection(existing.Selections, selection.Name)
				} else {
					selectedSite := site
					selectedSite.Selections = []string{selection.Name}
					physical[key] = &selectedSite
				}
			}
		}
	}
	report.SelectionFiles[selection.Name] = len(selectionFiles)
	return nil
}

func cleanupGitSources(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git source enumeration: %w", err)
	}
	var paths []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := string(raw)
		if strings.Contains(path, "/vendor/") || strings.HasPrefix(path, "vendor/") || strings.Contains(path, "/worktrees/") {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func cleanupHasTestingEntry(file *ast.File, info *types.Info) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		for _, field := range fn.Type.Params.List {
			typ := info.TypeOf(field.Type)
			if typ != nil && strings.HasPrefix(types.TypeString(typ, nil), "*testing.") {
				return true
			}
		}
	}
	return false
}

func cleanupHasCandidate(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if _, deferred := n.(*ast.DeferStmt); deferred {
			found = true // untyped callback target may conceal terminal cleanup
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Stop" || sel.Sel.Name == "StopAll" || sel.Sel.Name == "Cleanup") {
			found = true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Stop" || sel.Sel.Name == "StopAll" || sel.Sel.Name == "Cleanup") {
			found = true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			name := strings.ToLower(id.Name)
			if strings.Contains(name, "stop") || strings.Contains(name, "cleanup") {
				found = true
			}
		}
		return true
	})
	return found
}

func appendUniqueSelection(in []string, name string) []string {
	for _, existing := range in {
		if existing == name {
			return in
		}
	}
	return append(in, name)
}

func cleanupHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cleanupAST(node ast.Node) string {
	var out bytes.Buffer
	if node != nil {
		_ = format.Node(&out, token.NewFileSet(), node)
	}
	return out.String()
}

func reconcileCleanupBaseline(report *cleanupReport, baselinePath, root string, parsed map[string]*ast.File, modules map[string]string, replacedModules map[string]bool) error {
	baseline := cleanupBaseline{Version: 1}
	raw, err := os.ReadFile(baselinePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read cleanup baseline: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(raw, &baseline); err != nil {
			return fmt.Errorf("parse cleanup baseline: %w", err)
		}
	}
	if baseline.Version != 1 {
		return fmt.Errorf("cleanup baseline version %d, want 1", baseline.Version)
	}
	approved := map[string]cleanupApproval{}
	for _, entry := range baseline.Entries {
		if entry.Identity == "" || entry.Fingerprint == "" || entry.Classification != cleanupUnbounded || entry.Reason == "" || entry.OwnerIssue == "" {
			return fmt.Errorf("invalid cleanup debt approval: exact identity, fingerprint, unbounded classification, reason, owner/issue required")
		}
		if _, exists := approved[entry.Identity]; exists {
			return fmt.Errorf("duplicate cleanup baseline entry %s", entry.Identity)
		}
		approved[entry.Identity] = entry
	}
	resolved := map[string]cleanupResolution{}
	for _, item := range baseline.Resolutions {
		validClass := item.Classification == cleanupBounded || item.Classification == cleanupContract || item.Classification == cleanupNonLifecycle || item.Classification == cleanupUnbounded
		ordinaryDisposition := item.Classification == cleanupUnknown && item.Applicability == "ordinary-only"
		if item.Identity == "" || item.SiteFingerprint == "" || item.Question == "" || item.Reason == "" || item.Reviewer == "" || item.OwnerIssue == "" || len(item.Dependencies) == 0 ||
			(!validClass && !ordinaryDisposition) || (validClass && item.Applicability != "") {
			return fmt.Errorf("invalid manual cleanup resolution %q: exact site, permitted class, question, reviewer, owner/issue, and finite evidence dependencies required", item.Identity)
		}
		if _, exists := resolved[item.Identity]; exists {
			return fmt.Errorf("duplicate manual cleanup resolution %s", item.Identity)
		}
		for _, dependency := range item.Dependencies {
			if dependency.Fingerprint == "" {
				return fmt.Errorf("manual resolution %s has dependency without fingerprint", item.Identity)
			}
			if dependency.Path != "" {
				if dependency.Declaration == "" || dependency.Symbol != "" || dependency.Module != "" || dependency.Version != "" {
					return fmt.Errorf("manual resolution %s has incomplete source dependency", item.Identity)
				}
			} else if dependency.Symbol == "" || dependency.Module == "" || dependency.Version == "" {
				return fmt.Errorf("manual resolution %s has incomplete module dependency", item.Identity)
			}
		}
		resolved[item.Identity] = item
	}
	var problems []string
	for i := range report.Sites {
		site := &report.Sites[i]
		automaticClass := site.Classification
		if resolution, ok := resolved[site.Identity]; ok {
			delete(resolved, site.Identity)
			if automaticClass != cleanupUnknown || resolution.SiteFingerprint != site.Fingerprint ||
				(resolution.Applicability == "ordinary-only" && site.Applicability != "unresolved") {
				problems = append(problems, "stale manual cleanup resolution: "+site.Identity)
			} else {
				stale := false
				for _, dependency := range resolution.Dependencies {
					if dependency.Path == "" {
						version, selected := modules[dependency.Module]
						if !selected || replacedModules[dependency.Module] || version != dependency.Version || dependency.Fingerprint != cleanupHash(dependency.Module+"@"+version+"|"+dependency.Symbol) {
							stale = true
						}
						if stale {
							break
						}
						continue
					}
					current, depErr := cleanupSourceDependencyFingerprint(root, parsed, dependency.Path, dependency.Declaration)
					if depErr != nil || current != dependency.Fingerprint {
						stale = true
						break
					}
				}
				if stale {
					problems = append(problems, "stale manual cleanup resolution dependency: "+site.Identity)
				} else {
					site.Classification = resolution.Classification
					if resolution.Applicability == "ordinary-only" {
						site.Applicability = resolution.Applicability
					}
					site.Reason = "manual review: " + resolution.Reason
				}
			}
		}
		entry, exists := approved[site.Identity]
		if exists {
			delete(approved, site.Identity)
			if site.Classification != cleanupUnbounded || entry.Fingerprint != site.Fingerprint {
				problems = append(problems, "stale cleanup approval: "+site.Identity)
			}
		} else if site.Applicability != "ordinary-only" && (site.Classification == cleanupUnbounded || site.Classification == cleanupUnknown) {
			problems = append(problems, "new "+site.Classification+": "+site.Identity)
		}
	}
	for identity := range approved {
		problems = append(problems, "stale cleanup approval: "+identity)
	}
	for identity := range resolved {
		problems = append(problems, "stale manual cleanup resolution: "+identity)
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func cleanupSourceDependencyFingerprint(root string, parsed map[string]*ast.File, path, declaration string) (string, error) {
	if parsed[path] == nil {
		return "", fmt.Errorf("missing review dependency %s", path)
	}
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	// Parse without ordinary comments for semantic evidence; build constraints
	// are captured separately below and remain freshness-sensitive.
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), raw, 0)
	if err != nil {
		return "", err
	}
	var found ast.Node
	receiverName, methodName, qualified := strings.Cut(declaration, ".")
	add := func(node ast.Node) error {
		if found != nil {
			return fmt.Errorf("ambiguous review declaration %s in %s", declaration, path)
		}
		found = node
		return nil
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if qualified {
				if d.Recv == nil || len(d.Recv.List) != 1 || d.Name.Name != methodName {
					continue
				}
				receiver := d.Recv.List[0].Type
				if ptr, ok := receiver.(*ast.StarExpr); ok {
					receiver = ptr.X
				}
				id, ok := receiver.(*ast.Ident)
				if !ok || id.Name != receiverName {
					continue
				}
			} else if d.Name.Name != declaration {
				continue
			}
			if err := add(d); err != nil {
				return "", err
			}
		case *ast.GenDecl:
			if qualified {
				continue
			}
			for _, spec := range d.Specs {
				switch value := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range value.Names {
						if name.Name == declaration {
							if err := add(value); err != nil {
								return "", err
							}
						}
					}
				case *ast.TypeSpec:
					if value.Name.Name == declaration {
						if err := add(value); err != nil {
							return "", err
						}
					}
				}
			}
		}
	}
	if found == nil {
		return "", fmt.Errorf("missing review declaration %s in %s", declaration, path)
	}
	var constraints []string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//go:build ") || strings.HasPrefix(trimmed, "// +build ") {
			constraints = append(constraints, trimmed)
		}
	}
	var importBindings []string
	for _, item := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(item.Path.Value)
		if unquoteErr != nil {
			return "", unquoteErr
		}
		alias := filepath.Base(importPath)
		if item.Name != nil {
			alias = item.Name.Name
		}
		importBindings = append(importBindings, alias+"="+importPath)
	}
	sort.Strings(importBindings)
	return cleanupHash(cleanupAST(found) + "|" + strings.Join(constraints, "|") + "|" + strings.Join(importBindings, "|")), nil
}

type cleanupCallable struct {
	target          string
	receiver        string
	repositoryOwner bool
	evidence        []string
}

type cleanupPending struct {
	lit               *ast.FuncLit
	origin            string
	argValues         []string
	argEvidence       [][]string
	ownershipEvidence []string
}

type cleanupWalker struct {
	root                string
	path                string
	modulePath          string
	fset                *token.FileSet
	info                *types.Info
	function            string
	applicability       string
	origin              string
	env                 map[types.Object]string
	envEvidence         map[types.Object][]string
	assigned            map[types.Object]bool
	callables           map[types.Object]cleanupCallable
	verifiedCancel      map[types.Object]bool
	closures            map[types.Object]*ast.FuncLit
	closureOwner        map[*ast.FuncLit]string
	returnedContextless map[types.Object]cleanupCallable
	bindingEvidence     map[types.Object][]string
	ownershipEvidence   []string
	provEvidence        []string
	sites               []cleanupSite
	pending             []cleanupPending
	helpers             map[types.Object]*ast.FuncDecl
	activeHelpers       map[*ast.FuncDecl]bool
	activeClosures      map[*ast.FuncLit]bool
	declarationOnly     bool
	helperDepth         int
}

func cleanupEnclosingName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return "(" + cleanupAST(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

func (w *cleanupWalker) receiverBindingEvidence(expr ast.Expr) []string {
	if id, ok := expr.(*ast.Ident); ok {
		obj := w.info.Uses[id]
		return append([]string(nil), w.bindingEvidence[obj]...)
	}
	if unary, ok := expr.(*ast.UnaryExpr); ok {
		if lit, ok := unary.X.(*ast.CompositeLit); ok && len(lit.Elts) == 0 {
			return nil
		}
	}
	return []string{cleanupHash(cleanupAST(expr))}
}

func (w *cleanupWalker) receiverExpressionEvidence(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.Ident:
		return append([]string(nil), w.bindingEvidence[w.info.Uses[e]]...)
	case *ast.SelectorExpr:
		return w.receiverExpressionEvidence(e.X)
	case *ast.ParenExpr:
		return w.receiverExpressionEvidence(e.X)
	}
	return nil
}

func scanCleanupFile(root, rel string, file *ast.File, fset *token.FileSet, info *types.Info, modulePath string, helpers map[types.Object]*ast.FuncDecl) []cleanupSite {
	var sites []cleanupSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		applicability := "unresolved"
		if cleanupIsTestRoot(fn, info) {
			applicability = "ordinary-only"
		}
		w := &cleanupWalker{root: root, path: rel, modulePath: modulePath, fset: fset, info: info, function: cleanupEnclosingName(fn),
			applicability: applicability, origin: "ordinary", declarationOnly: applicability == "unresolved", env: make(map[types.Object]string), envEvidence: make(map[types.Object][]string), assigned: make(map[types.Object]bool), callables: make(map[types.Object]cleanupCallable), verifiedCancel: make(map[types.Object]bool), closures: make(map[types.Object]*ast.FuncLit), closureOwner: make(map[*ast.FuncLit]string), returnedContextless: make(map[types.Object]cleanupCallable), bindingEvidence: make(map[types.Object][]string), helpers: helpers, activeHelpers: make(map[*ast.FuncDecl]bool), activeClosures: make(map[*ast.FuncLit]bool)}
		w.walkBlock(fn.Body)
		w.finish()
		sites = append(sites, w.sites...)
	}
	return sites
}

func (w *cleanupWalker) walkBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		w.walkStmt(stmt)
	}
}

func (w *cleanupWalker) finish() {
	for len(w.pending) > 0 {
		pending := w.pending
		w.pending = nil
		for _, item := range pending {
			child := w.child(item.origin)
			child.useClosureOwner(item.lit)
			child.bindClosureArgs(item.lit, item.argValues, item.argEvidence)
			child.ownershipEvidence = append(child.ownershipEvidence, item.ownershipEvidence...)
			child.walkBlock(item.lit.Body)
			child.finish()
			w.sites = append(w.sites, child.sites...)
		}
	}
}

func (w *cleanupWalker) child(origin string) *cleanupWalker {
	env := make(map[types.Object]string, len(w.env))
	for k, v := range w.env {
		env[k] = v
	}
	envEvidence := make(map[types.Object][]string, len(w.envEvidence))
	for k, v := range w.envEvidence {
		envEvidence[k] = append([]string(nil), v...)
	}
	callables := make(map[types.Object]cleanupCallable, len(w.callables))
	for k, v := range w.callables {
		callables[k] = v
	}
	verifiedCancel := make(map[types.Object]bool, len(w.verifiedCancel))
	for k, v := range w.verifiedCancel {
		verifiedCancel[k] = v
	}
	closures := make(map[types.Object]*ast.FuncLit, len(w.closures))
	for k, v := range w.closures {
		closures[k] = v
	}
	bindingEvidence := make(map[types.Object][]string, len(w.bindingEvidence))
	for k, v := range w.bindingEvidence {
		bindingEvidence[k] = append([]string(nil), v...)
	}
	closureOwner := make(map[*ast.FuncLit]string, len(w.closureOwner))
	for k, v := range w.closureOwner {
		closureOwner[k] = v
	}
	returnedContextless := make(map[types.Object]cleanupCallable, len(w.returnedContextless))
	for k, v := range w.returnedContextless {
		returnedContextless[k] = v
	}
	return &cleanupWalker{root: w.root, path: w.path, modulePath: w.modulePath, fset: w.fset, info: w.info, function: w.function,
		applicability: w.applicability, origin: origin, declarationOnly: w.declarationOnly, helperDepth: w.helperDepth, env: env, envEvidence: envEvidence, assigned: make(map[types.Object]bool), callables: callables, verifiedCancel: verifiedCancel, closures: closures, closureOwner: closureOwner, returnedContextless: returnedContextless, bindingEvidence: bindingEvidence, ownershipEvidence: append([]string(nil), w.ownershipEvidence...), helpers: w.helpers, activeHelpers: w.activeHelpers, activeClosures: w.activeClosures}
}

func (w *cleanupWalker) walkStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		w.walkAssign(s)
	case *ast.DeclStmt:
		w.walkDecl(s)
	case *ast.DeferStmt:
		w.walkDefer(s)
	case *ast.ExprStmt:
		w.walkExpr(s)
	case *ast.ReturnStmt:
		for _, expr := range s.Results {
			w.inspectCalls(expr)
		}
	case *ast.IfStmt:
		w.walkIf(s)
	case *ast.BlockStmt:
		w.walkBlock(s)
	default:
		w.invalidateAssigned(stmt)
		w.inspectCalls(stmt)
	}
}

func (w *cleanupWalker) walkAssign(s *ast.AssignStmt) {
	values := make([]string, len(s.Lhs))
	for i := range values {
		values[i] = "unknown"
	}
	evidence := make([][]string, len(s.Lhs))
	if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
		for i := range s.Lhs {
			values[i], evidence[i] = w.evalIndexedProvenance(s.Rhs[0], i)
			evidence[i] = append(evidence[i], cleanupHash(cleanupAST(s.Rhs[0])))
		}
	} else {
		for i, expr := range s.Rhs {
			if i >= len(values) {
				break
			}
			values[i], evidence[i] = w.evalProvenance(expr)
			evidence[i] = append(evidence[i], cleanupHash(cleanupAST(expr)))
		}
	}
	parallelValues := len(s.Lhs) > 1 && len(s.Rhs) > 1
	var parallelBindingEvidence [][]string
	if parallelValues {
		parallelBindingEvidence = make([][]string, len(s.Lhs))
		for i, expr := range s.Rhs {
			if i < len(parallelBindingEvidence) {
				parallelBindingEvidence[i] = w.receiverBindingEvidence(expr)
			}
		}
	}
	for i, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		obj := w.info.Defs[id]
		if obj == nil {
			obj = w.info.Uses[id]
		}
		if obj == nil {
			continue
		}
		value := "unknown"
		if i < len(values) {
			value = values[i]
		}
		w.env[obj] = value
		if parallelValues {
			// Callable RHS aliases require simultaneous evaluation. Until they are
			// modeled, preserve context values but refuse callable provenance.
			w.bindingEvidence[obj] = parallelBindingEvidence[i]
			w.verifiedCancel[obj] = false
			w.envEvidence[obj] = evidence[i]
			w.assigned[obj] = true
			delete(w.closures, obj)
			delete(w.callables, obj)
			delete(w.returnedContextless, obj)
			continue
		}
		if len(s.Rhs) == 1 {
			w.bindingEvidence[obj] = w.receiverBindingEvidence(s.Rhs[0])
		} else if i < len(s.Rhs) {
			w.bindingEvidence[obj] = w.receiverBindingEvidence(s.Rhs[i])
		}
		w.verifiedCancel[obj] = (len(s.Rhs) == 1 && i == 1 && w.isContextCancelConstructor(s.Rhs[0])) ||
			(i < len(s.Rhs) && w.isVerifiedCancelAlias(s.Rhs[i]))
		w.envEvidence[obj] = evidence[i]
		w.assigned[obj] = true
		if i < len(s.Rhs) {
			w.closures[obj] = w.localClosure(s.Rhs[i])
		} else {
			delete(w.closures, obj)
		}
		if i < len(s.Rhs) {
			if callable, ok := w.callableProvenance(s.Rhs[i]); ok {
				w.callables[obj] = callable
			} else {
				delete(w.callables, obj)
			}
		}
		var contextlessAlias cleanupCallable
		var hasContextlessAlias bool
		if i < len(s.Rhs) {
			if source, ok := s.Rhs[i].(*ast.Ident); ok {
				contextlessAlias, hasContextlessAlias = w.returnedContextless[w.info.Uses[source]]
			}
		}
		delete(w.returnedContextless, obj)
		if hasContextlessAlias {
			w.returnedContextless[obj] = contextlessAlias
		}
		if len(s.Rhs) == 1 {
			w.bindReturnedClosure(obj, s.Rhs[0], i)
		}
	}
	for _, expr := range s.Rhs {
		if _, stored := expr.(*ast.FuncLit); !stored {
			w.inspectCalls(expr)
		}
	}
}

func (w *cleanupWalker) walkDecl(s *ast.DeclStmt) {
	gen, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gen.Specs {
		v, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range v.Names {
			obj := w.info.Defs[name]
			if obj == nil {
				continue
			}
			value := "unknown"
			var evidence []string
			if i < len(v.Values) {
				if len(v.Values) == 1 && len(v.Names) > 1 {
					value, evidence = w.evalIndexedProvenance(v.Values[i], i)
				} else {
					value, evidence = w.evalProvenance(v.Values[i])
				}
			} else if len(v.Values) == 1 {
				value, evidence = w.evalIndexedProvenance(v.Values[0], i)
			}
			w.env[obj] = value
			if len(v.Values) == 1 {
				w.bindingEvidence[obj] = w.receiverBindingEvidence(v.Values[0])
			} else if i < len(v.Values) {
				w.bindingEvidence[obj] = w.receiverBindingEvidence(v.Values[i])
			}
			w.verifiedCancel[obj] = (len(v.Values) == 1 && i == 1 && w.isContextCancelConstructor(v.Values[0])) ||
				(i < len(v.Values) && w.isVerifiedCancelAlias(v.Values[i]))
			if i < len(v.Values) {
				evidence = append(evidence, cleanupHash(cleanupAST(v.Values[i])))
			}
			w.envEvidence[obj] = evidence
			w.assigned[obj] = true
			if i < len(v.Values) {
				w.closures[obj] = w.localClosure(v.Values[i])
			} else {
				delete(w.closures, obj)
			}
			if i < len(v.Values) {
				if callable, ok := w.callableProvenance(v.Values[i]); ok {
					w.callables[obj] = callable
				}
			}
			var contextlessAlias cleanupCallable
			var hasContextlessAlias bool
			if i < len(v.Values) {
				if source, ok := v.Values[i].(*ast.Ident); ok {
					contextlessAlias, hasContextlessAlias = w.returnedContextless[w.info.Uses[source]]
				}
			}
			delete(w.returnedContextless, obj)
			if hasContextlessAlias {
				w.returnedContextless[obj] = contextlessAlias
			}
			if len(v.Values) == 1 {
				w.bindReturnedClosure(obj, v.Values[0], i)
			}
		}
		for _, expr := range v.Values {
			if _, stored := expr.(*ast.FuncLit); !stored {
				w.inspectCalls(expr)
			}
		}
	}
}

func (w *cleanupWalker) walkDefer(s *ast.DeferStmt) {
	var lit *ast.FuncLit
	if id, ok := s.Call.Fun.(*ast.Ident); ok {
		lit = w.closures[w.info.Uses[id]]
	} else if direct, ok := s.Call.Fun.(*ast.FuncLit); ok {
		lit = direct
	}
	if lit != nil {
		values, evidence := w.closureArgProvenance(s.Call.Args)
		w.pending = append(w.pending, cleanupPending{lit: lit, origin: "defer", argValues: values, argEvidence: evidence, ownershipEvidence: append([]string(nil), w.ownershipEvidence...)})
	} else {
		child := w.child("defer")
		child.inspectCalls(s.Call)
		w.sites = append(w.sites, child.sites...)
	}
}

func (w *cleanupWalker) walkExpr(s *ast.ExprStmt) {
	if call, ok := s.X.(*ast.CallExpr); ok && w.isTestingCleanup(call) {
		if len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.FuncLit); ok {
				w.pending = append(w.pending, cleanupPending{lit: lit, origin: "cleanup", ownershipEvidence: append([]string(nil), w.ownershipEvidence...)})
				return
			}
			if sel, ok := call.Args[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "Stop" {
				child := w.child("cleanup")
				child.recordMethodValueStop(sel)
				w.sites = append(w.sites, child.sites...)
				return
			}
			if w.recordKnownContextlessCleanup(call.Args[0], "cleanup") {
				return
			}
			if id, ok := call.Args[0].(*ast.Ident); ok {
				if lit := w.closures[w.info.Uses[id]]; lit != nil {
					w.pending = append(w.pending, cleanupPending{lit: lit, origin: "cleanup", ownershipEvidence: append([]string(nil), w.ownershipEvidence...)})
					return
				}
				child := w.child("cleanup")
				if helper := w.helpers[w.info.Uses[id]]; helper != nil {
					if child.helperHasCandidate(helper, 0, make(map[*ast.FuncDecl]bool)) {
						child.followHelper(&ast.CallExpr{Fun: id})
					}
				} else {
					child.recordUnresolvedEdge(id, "Cleanup callback target unresolved")
				}
				w.sites = append(w.sites, child.sites...)
				return
			}
			w.recordUnresolvedEdge(call.Args[0], "Cleanup callback expression unresolved")
			return
		}
	}
	w.inspectCalls(s.X)
}

func (w *cleanupWalker) walkIf(s *ast.IfStmt) {
	if s.Init != nil {
		w.walkStmt(s.Init)
	}
	before := w.child(w.origin)
	branch := w.child(w.origin)
	branch.ownershipEvidence = append(branch.ownershipEvidence, cleanupHash(cleanupAST(s.Cond)+"|then"))
	branch.walkBlock(s.Body)
	other := before
	if s.Else != nil {
		other = w.child(w.origin)
		other.ownershipEvidence = append(other.ownershipEvidence, cleanupHash(cleanupAST(s.Cond)+"|else"))
		if block, ok := s.Else.(*ast.BlockStmt); ok {
			other.walkBlock(block)
		} else {
			other.walkStmt(s.Else)
		}
	}
	w.pending = append(w.pending, branch.pending...)
	w.pending = append(w.pending, other.pending...)
	w.sites = append(w.sites, branch.sites...)
	w.sites = append(w.sites, other.sites...)
	changed := make(map[types.Object]bool)
	for obj := range branch.assigned {
		changed[obj] = true
	}
	for obj := range other.assigned {
		changed[obj] = true
	}
	for obj := range changed {
		left, right := branch.env[obj], other.env[obj]
		if left == right {
			w.env[obj] = left
		} else {
			w.env[obj] = "unknown"
		}
		w.envEvidence[obj] = []string{cleanupHash(cleanupAST(s.Cond) + "|" + strings.Join(branch.envEvidence[obj], "|") + "|" + strings.Join(other.envEvidence[obj], "|"))}
		w.bindingEvidence[obj] = []string{cleanupHash(cleanupAST(s.Cond) + "|" + strings.Join(branch.bindingEvidence[obj], "|") + "|" + strings.Join(other.bindingEvidence[obj], "|"))}
		w.verifiedCancel[obj] = branch.verifiedCancel[obj] && other.verifiedCancel[obj]
		leftCallable, leftOK := branch.callables[obj]
		rightCallable, rightOK := other.callables[obj]
		if !leftOK || !rightOK || !reflect.DeepEqual(leftCallable, rightCallable) {
			delete(w.callables, obj)
		} else {
			w.callables[obj] = leftCallable
		}
		if branch.closures[obj] != other.closures[obj] {
			delete(w.closures, obj)
		} else {
			w.closures[obj] = branch.closures[obj]
		}
		leftContextless, leftKnown := branch.returnedContextless[obj]
		rightContextless, rightKnown := other.returnedContextless[obj]
		if !leftKnown || !rightKnown || !reflect.DeepEqual(leftContextless, rightContextless) {
			delete(w.returnedContextless, obj)
		} else {
			w.returnedContextless[obj] = leftContextless
		}
		w.assigned[obj] = true
	}
}

func (w *cleanupWalker) isTestingCleanup(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Cleanup" {
		return false
	}
	recv := w.info.TypeOf(sel.X)
	if recv == nil {
		return false
	}
	name := types.TypeString(recv, nil)
	return strings.HasPrefix(name, "*testing.") || name == "testing.TB"
}

func (w *cleanupWalker) inspectCalls(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			child := w.child(w.origin)
			child.walkBlock(lit.Body)
			child.finish()
			w.sites = append(w.sites, child.sites...)
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := cleanupUnparen(call.Fun)
		if sel, ok := fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Stop" || w.isTypedManagerStopAll(sel)) {
			w.recordStop(call, sel)
			return false // typed lifecycle contract is the outward boundary
		}
		if id, ok := fun.(*ast.Ident); ok {
			if len(call.Args) == 0 {
				if _, known := w.returnedContextless[w.info.Uses[id]]; known {
					w.recordKnownContextlessCleanup(id, w.origin)
					return false
				}
			}
			if lit := w.closures[w.info.Uses[id]]; lit != nil {
				if w.origin != "ordinary" && cleanupIsTerminalCallback(w.info.TypeOf(id)) && len(call.Args) >= 1 {
					w.recordCallback(call, id)
				}
				if w.activeClosures[lit] {
					w.recordUnresolvedEdge(id, "cyclic local cleanup closure")
				} else {
					w.activeClosures[lit] = true
					child := w.child(w.origin)
					child.useClosureOwner(lit)
					values, evidence := w.closureArgProvenance(call.Args)
					child.bindClosureArgs(lit, values, evidence)
					child.walkBlock(lit.Body)
					child.finish()
					w.sites = append(w.sites, child.sites...)
					for obj := range child.assigned {
						w.env[obj] = child.env[obj]
						w.envEvidence[obj] = append([]string(nil), child.envEvidence[obj]...)
						w.verifiedCancel[obj] = child.verifiedCancel[obj]
						if callable, ok := child.callables[obj]; ok {
							w.callables[obj] = callable
						} else {
							delete(w.callables, obj)
						}
						if closure, ok := child.closures[obj]; ok {
							w.closures[obj] = closure
						} else {
							delete(w.closures, obj)
						}
						if known, ok := child.returnedContextless[obj]; ok {
							w.returnedContextless[obj] = known
						} else {
							delete(w.returnedContextless, obj)
						}
						w.assigned[obj] = true
					}
					delete(w.activeClosures, lit)
				}
				return false
			}
			if w.origin != "ordinary" {
				if len(call.Args) >= 1 && cleanupIsTerminalCallback(w.info.TypeOf(id)) {
					w.recordCallback(call, id)
				} else if _, variable := w.info.Uses[id].(*types.Var); variable && !w.verifiedCancel[w.info.Uses[id]] {
					w.recordUnresolvedEdge(id, "deferred or cleanup callback variable target unresolved")
				}
			}
		}
		if w.origin != "ordinary" {
			switch fun := fun.(type) {
			case *ast.Ident:
				// Resolved helpers and verified cancellation were handled above.
			case *ast.SelectorExpr:
				if _, variable := w.info.Uses[fun.Sel].(*types.Var); variable && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
					w.recordUnresolvedEdge(fun, "function-valued selector target unresolved on cleanup path")
					return false
				}
			case *ast.IndexExpr:
				if cleanupIsFunctionValue(w.info.TypeOf(fun)) {
					w.recordUnresolvedEdge(fun, "function-valued index target unresolved on cleanup path")
					return false
				}
			default:
				if _, literal := fun.(*ast.FuncLit); !literal && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
					w.recordUnresolvedEdge(fun, "function-valued expression target unresolved on cleanup path")
					return false
				}
			}
		}
		resolvedCall := *call
		resolvedCall.Fun = fun
		w.followHelper(&resolvedCall)
		return true
	})
}

func cleanupUnparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func cleanupIsFunctionValue(typ types.Type) bool {
	if typ == nil {
		return false
	}
	_, ok := typ.Underlying().(*types.Signature)
	return ok
}

func cleanupIsContextType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	return types.TypeString(typ, nil) == "context.Context"
}

func cleanupIsTerminalCallback(typ types.Type) bool {
	if typ == nil {
		return false
	}
	sig, ok := typ.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() < 1 || sig.Results().Len() != 1 {
		return false
	}
	return cleanupIsContextType(sig.Params().At(sig.Params().Len()-1).Type()) && types.TypeString(sig.Results().At(0).Type(), nil) == "error"
}

func (w *cleanupWalker) isTypedManagerStopAll(sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "StopAll" {
		return false
	}
	obj := w.info.Uses[sel.Sel]
	if selection := w.info.Selections[sel]; selection != nil {
		obj = selection.Obj()
	}
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "github.com/c360studio/semstreams/service" || obj.Name() != "StopAll" {
		return false
	}
	if types.TypeString(w.info.TypeOf(sel.X), nil) != "*github.com/c360studio/semstreams/service.Manager" {
		return false
	}
	sig, ok := w.info.TypeOf(sel).(*types.Signature)
	return ok && sig.Params().Len() >= 1 && cleanupIsContextType(sig.Params().At(sig.Params().Len()-1).Type()) &&
		sig.Results().Len() == 1 && types.TypeString(sig.Results().At(0).Type(), nil) == "error"
}

func (w *cleanupWalker) recordStop(call *ast.CallExpr, sel *ast.SelectorExpr) {
	target := sel.Sel.Name
	receiver := "unknown"
	if typ := w.info.TypeOf(sel.X); typ != nil {
		receiver = types.TypeString(typ, nil)
	}
	obj := w.info.Uses[sel.Sel]
	if selection := w.info.Selections[sel]; selection != nil {
		obj = selection.Obj()
	}
	if obj != nil && obj.Pkg() != nil {
		target = obj.Pkg().Path() + "." + obj.Name()
	}
	class := cleanupNonLifecycle
	contextState := "none"
	var evidence []string
	reason := "Stop does not have the admitted repository-owned context-taking contract"
	sig, ok := w.info.TypeOf(call.Fun).(*types.Signature)
	signatureContext := ok && sig.Params().Len() > 0 && cleanupIsContextType(sig.Params().At(sig.Params().Len()-1).Type()) &&
		sig.Results().Len() == 1 && types.TypeString(sig.Results().At(0).Type(), nil) == "error"
	repositoryOwner := obj != nil && obj.Pkg() != nil && (obj.Pkg().Path() == w.modulePath || strings.HasPrefix(obj.Pkg().Path(), w.modulePath+"/"))
	if signatureContext && repositoryOwner && len(call.Args) > 0 {
		ctxArg := call.Args[len(call.Args)-1]
		contextState, evidence = w.evalProvenance(ctxArg)
		class = cleanupBounded
		if contextState == "unbounded" {
			if w.origin == "ordinary" {
				class = cleanupUnknown
				reason = "ordinary unbounded lifecycle call requires exact intent review"
			} else {
				class = cleanupUnbounded
			}
		}
		if contextState != "finite" && contextState != "unbounded" {
			class = cleanupUnknown
			reason = "context provenance unresolved"
		} else if class != cleanupUnknown {
			reason = ""
		}
	}
	line := w.fset.Position(call.Pos()).Line
	normalized := cleanupAST(call)
	fingerprintSource := normalized + "|" + contextState + "|" + w.origin + "|" + strings.Join(evidence, "|")
	ownerEvidence := append([]string(nil), w.ownershipEvidence...)
	ownerEvidence = append(ownerEvidence, w.receiverExpressionEvidence(sel.X)...)
	if len(ownerEvidence) > 0 {
		fingerprintSource += "|owner:" + strings.Join(ownerEvidence, "|")
	}
	w.sites = append(w.sites, cleanupSite{Path: w.path, StartOffset: w.fset.Position(call.Pos()).Offset, EndOffset: w.fset.Position(call.End()).Offset, Line: line, Function: w.function, DeclarationOnly: w.declarationOnly,
		Origin: w.origin, Target: target, Receiver: receiver, Context: contextState,
		Classification: class, Applicability: w.siteApplicability(), Reason: reason, Fingerprint: cleanupHash(fingerprintSource)})
}

func (w *cleanupWalker) callableProvenance(expr ast.Expr) (cleanupCallable, bool) {
	if id, ok := expr.(*ast.Ident); ok {
		value, ok := w.callables[w.info.Uses[id]]
		return value, ok
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok {
			fn := w.helpers[w.info.Uses[id]]
			if fn != nil && fn.Body != nil && !w.activeHelpers[fn] {
				direct := make(map[*ast.ReturnStmt]bool)
				for _, stmt := range fn.Body.List {
					if ret, ok := stmt.(*ast.ReturnStmt); ok {
						direct[ret] = true
					}
				}
				nestedReturn := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if _, lit := n.(*ast.FuncLit); lit {
						return false
					}
					if ret, ok := n.(*ast.ReturnStmt); ok && !direct[ret] {
						nestedReturn = true
					}
					return true
				})
				if nestedReturn {
					return cleanupCallable{}, false
				}
				w.activeHelpers[fn] = true
				defer delete(w.activeHelpers, fn)
				var resolved cleanupCallable
				seen := false
				for _, stmt := range fn.Body.List {
					ret, ok := stmt.(*ast.ReturnStmt)
					if !ok || len(ret.Results) != 1 {
						continue
					}
					candidate, ok := w.callableProvenance(ret.Results[0])
					if !ok {
						return cleanupCallable{}, false
					}
					if seen && (resolved.target != candidate.target || resolved.receiver != candidate.receiver) {
						return cleanupCallable{}, false
					}
					resolved = candidate
					seen = true
				}
				if seen {
					resolved.evidence = append(append([]string(nil), resolved.evidence...), cleanupHash(cleanupAST(fn.Body)))
					return resolved, true
				}
			}
		}
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Stop" && !w.isTypedManagerStopAll(sel)) {
		return cleanupCallable{}, false
	}
	obj := w.info.Uses[sel.Sel]
	if selection := w.info.Selections[sel]; selection != nil {
		obj = selection.Obj()
	}
	if obj == nil || obj.Pkg() == nil {
		return cleanupCallable{}, false
	}
	recv := "unknown"
	if typ := w.info.TypeOf(sel.X); typ != nil {
		recv = types.TypeString(typ, nil)
	}
	own := obj.Pkg().Path() == w.modulePath || strings.HasPrefix(obj.Pkg().Path(), w.modulePath+"/")
	return cleanupCallable{target: obj.Pkg().Path() + "." + obj.Name(), receiver: recv, repositoryOwner: own}, true
}

func (w *cleanupWalker) recordMethodValueStop(sel *ast.SelectorExpr) {
	callable, ok := w.callableProvenance(sel)
	if !ok {
		return
	}
	w.sites = append(w.sites, cleanupSite{Path: w.path, StartOffset: w.fset.Position(sel.Pos()).Offset,
		EndOffset: w.fset.Position(sel.End()).Offset, Line: w.fset.Position(sel.Pos()).Line,
		Function: w.function, DeclarationOnly: w.declarationOnly, Origin: w.origin, Target: callable.target, Receiver: callable.receiver,
		Context: "none", Classification: cleanupNonLifecycle, Applicability: w.siteApplicability(),
		Reason:      "contextless Stop method value registered with testing.Cleanup",
		Fingerprint: cleanupHash(cleanupAST(sel) + "|cleanup|contextless")})
}

func (w *cleanupWalker) useClosureOwner(lit *ast.FuncLit) {
	if owner := w.closureOwner[lit]; owner != "" {
		w.function = owner
		if rel, err := filepath.Rel(w.root, w.fset.Position(lit.Pos()).Filename); err == nil && !strings.HasPrefix(rel, "..") {
			w.path = filepath.ToSlash(rel)
		}
	}
}

func (w *cleanupWalker) bindReturnedClosure(obj types.Object, expr ast.Expr, resultIndex int) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	var target types.Object
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		target = w.info.Uses[fun]
	case *ast.SelectorExpr:
		target = w.info.Uses[fun.Sel]
		if selection := w.info.Selections[fun]; selection != nil {
			target = selection.Obj()
		}
	}
	fn := w.helpers[target]
	if fn == nil || fn.Body == nil {
		return
	}
	var returnedMethod cleanupCallable
	methodSeen, methodConflict := false, false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if resultIndex >= len(ret.Results) {
			methodConflict = true
			return false
		}
		if id, nilResult := ret.Results[resultIndex].(*ast.Ident); nilResult && id.Name == "nil" {
			return false
		}
		sel, ok := ret.Results[resultIndex].(*ast.SelectorExpr)
		if !ok {
			methodConflict = true
			return false
		}
		method, known := w.knownForeignContextlessMethod(sel)
		if !known || (methodSeen && (method.target != returnedMethod.target || method.receiver != returnedMethod.receiver)) {
			methodConflict = true
			return false
		}
		returnedMethod, methodSeen = method, true
		return false
	})
	if methodSeen && !methodConflict {
		returnedMethod.evidence = []string{cleanupHash(cleanupAST(fn.Body))}
		w.returnedContextless[obj] = returnedMethod
		return
	}
	assignments := make(map[types.Object]*ast.FuncLit)
	bad := make(map[types.Object]bool)
	topLevel := make(map[*ast.AssignStmt]bool)
	for _, stmt := range fn.Body.List {
		if assign, ok := stmt.(*ast.AssignStmt); ok {
			topLevel[assign] = true
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			key := w.info.Defs[id]
			if key == nil {
				key = w.info.Uses[id]
			}
			if key == nil {
				continue
			}
			if !topLevel[assign] || len(assign.Rhs) != len(assign.Lhs) || i >= len(assign.Rhs) {
				bad[key] = true
				continue
			}
			lit, ok := assign.Rhs[i].(*ast.FuncLit)
			if !ok || assignments[key] != nil {
				bad[key] = true
				continue
			}
			assignments[key] = lit
		}
		return true
	})
	var named types.Object
	if fn.Type.Results != nil {
		index := 0
		for _, field := range fn.Type.Results.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for _, name := range field.Names {
				if index == resultIndex {
					named = w.info.Defs[name]
				}
				index++
			}
			if len(field.Names) == 0 {
				index++
			}
		}
	}
	var returned *ast.FuncLit
	seenReturn, conflict := false, false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, lit := n.(*ast.FuncLit); lit {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		seenReturn = true
		var candidate ast.Expr
		if len(ret.Results) == 0 {
			if named == nil {
				conflict = true
				return false
			}
			if bad[named] {
				conflict = true
				return false
			}
			if lit := assignments[named]; lit != nil {
				candidate = lit
			}
		} else if resultIndex < len(ret.Results) {
			candidate = ret.Results[resultIndex]
		} else {
			conflict = true
			return false
		}
		if id, ok := candidate.(*ast.Ident); ok {
			if id.Name == "nil" {
				return false
			}
			key := w.info.Uses[id]
			if bad[key] {
				conflict = true
				return false
			}
			candidate = assignments[key]
		}
		lit, ok := candidate.(*ast.FuncLit)
		if !ok || (returned != nil && returned != lit) {
			conflict = true
			return false
		}
		returned = lit
		return false
	})
	if !seenReturn || conflict || returned == nil {
		return
	}
	w.closures[obj] = returned
	w.closureOwner[returned] = cleanupEnclosingName(fn)
	if cleanupIsTerminalCallback(w.info.TypeOf(returned)) {
		targetName := w.modulePath + "." + fn.Name.Name + "#returned-closure"
		w.callables[obj] = cleanupCallable{target: targetName, receiver: "closure", repositoryOwner: true,
			evidence: []string{cleanupHash(cleanupAST(fn.Body))}}
	}
}

func (w *cleanupWalker) closureArgProvenance(args []ast.Expr) ([]string, [][]string) {
	values := make([]string, len(args))
	evidence := make([][]string, len(args))
	for i, arg := range args {
		values[i], evidence[i] = w.evalProvenance(arg)
		evidence[i] = append(evidence[i], cleanupHash(cleanupAST(arg)))
	}
	return values, evidence
}

func (w *cleanupWalker) bindClosureArgs(lit *ast.FuncLit, values []string, evidence [][]string) {
	if lit.Type.Params == nil {
		return
	}
	index := 0
	for _, field := range lit.Type.Params.List {
		for _, name := range field.Names {
			obj := w.info.Defs[name]
			if obj != nil && index < len(values) {
				w.env[obj] = values[index]
				w.envEvidence[obj] = evidence[index]
			}
			index++
		}
	}
}

func (w *cleanupWalker) localClosure(expr ast.Expr) *ast.FuncLit {
	if lit, ok := expr.(*ast.FuncLit); ok {
		return lit
	}
	if id, ok := expr.(*ast.Ident); ok {
		return w.closures[w.info.Uses[id]]
	}
	return nil
}

func (w *cleanupWalker) isContextCancelConstructor(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj := w.info.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "context" {
		return false
	}
	switch obj.Name() {
	case "WithCancel", "WithCancelCause", "WithDeadline", "WithDeadlineCause", "WithTimeout", "WithTimeoutCause":
		return true
	}
	return false
}

func (w *cleanupWalker) isVerifiedCancelAlias(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && w.verifiedCancel[w.info.Uses[id]]
}

func (w *cleanupWalker) knownForeignContextlessMethod(sel *ast.SelectorExpr) (cleanupCallable, bool) {
	obj := w.info.Uses[sel.Sel]
	if selection := w.info.Selections[sel]; selection != nil {
		obj = selection.Obj()
	}
	if obj == nil || obj.Pkg() == nil {
		return cleanupCallable{}, false
	}
	receiver := types.TypeString(w.info.TypeOf(sel.X), nil)
	known := (obj.Pkg().Path() == "net/http/httptest" && obj.Name() == "Close" && receiver == "*net/http/httptest.Server") ||
		(obj.Pkg().Path() == "github.com/nats-io/nats-server/v2/server" && obj.Name() == "Shutdown" && receiver == "*github.com/nats-io/nats-server/v2/server.Server")
	if !known {
		return cleanupCallable{}, false
	}
	return cleanupCallable{target: obj.Pkg().Path() + "." + obj.Name(), receiver: receiver}, true
}

func (w *cleanupWalker) recordKnownContextlessCleanup(expr ast.Expr, origin string) bool {
	target := cleanupAST(expr)
	receiver := ""
	reason := ""
	var evidence []string
	if id, ok := expr.(*ast.Ident); ok {
		if known, exists := w.returnedContextless[w.info.Uses[id]]; exists {
			target, receiver, evidence = known.target, known.receiver, known.evidence
			reason = "exact returned contextless foreign method outside repository Stop contract"
		} else if w.verifiedCancel[w.info.Uses[id]] {
			reason = "cancel function from typed context constructor has no lifecycle Stop contract"
		}
	} else if sel, ok := expr.(*ast.SelectorExpr); ok {
		if known, exists := w.knownForeignContextlessMethod(sel); exists {
			target, receiver = known.target, known.receiver
			reason = "resolved contextless foreign server lifecycle method outside repository Stop contract"
		}
	}
	if reason == "" {
		return false
	}
	pos := w.fset.Position(expr.Pos())
	applicability := w.applicability
	if origin == "defer" || origin == "cleanup" {
		applicability = "cleanup-owned"
	}
	w.sites = append(w.sites, cleanupSite{Path: w.path, StartOffset: pos.Offset, EndOffset: w.fset.Position(expr.End()).Offset,
		Line: pos.Line, Function: w.function, DeclarationOnly: w.declarationOnly, Origin: origin, Target: target, Receiver: receiver,
		Context: "none", Classification: cleanupNonLifecycle, Applicability: applicability, Reason: reason,
		Fingerprint: cleanupHash(cleanupAST(expr) + "|" + reason + "|" + strings.Join(evidence, "|"))})
	return true
}

func (w *cleanupWalker) recordCallback(call *ast.CallExpr, id *ast.Ident) {
	ctx, evidence := w.evalProvenance(call.Args[len(call.Args)-1])
	class := cleanupUnknown
	reason := "context-taking cleanup callback target unresolved"
	target := id.Name
	receiver := "callback"
	var callableEvidence []string
	if callable, ok := w.callables[w.info.Uses[id]]; ok {
		callableEvidence = callable.evidence
		target = callable.target
		receiver = callable.receiver
		if callable.repositoryOwner {
			if ctx == "finite" {
				class = cleanupBounded
				reason = ""
			}
			if ctx == "unbounded" {
				class = cleanupUnbounded
				reason = ""
			}
		} else {
			class = cleanupNonLifecycle
			reason = "foreign Stop method value"
		}
	}
	w.sites = append(w.sites, cleanupSite{Path: w.path, StartOffset: w.fset.Position(call.Pos()).Offset,
		EndOffset: w.fset.Position(call.End()).Offset, Line: w.fset.Position(call.Pos()).Line,
		Function: w.function, DeclarationOnly: w.declarationOnly, Origin: w.origin, Target: target, Receiver: receiver,
		Context: ctx, Classification: class, Applicability: w.siteApplicability(), Reason: reason,
		Fingerprint: cleanupHash(cleanupAST(call) + "|" + target + "|" + ctx + "|" + w.origin + "|" + strings.Join(evidence, "|") + "|" + strings.Join(callableEvidence, "|"))})
}

func (w *cleanupWalker) contextProvenance(expr ast.Expr, depth int) string {
	if depth > 12 {
		return "unknown"
	}
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return w.contextProvenance(e.X, depth+1)
	case *ast.Ident:
		obj := w.info.Uses[e]
		if obj == nil {
			obj = w.info.Defs[e]
		}
		if obj != nil {
			if value, ok := w.env[obj]; ok {
				w.provEvidence = append(w.provEvidence, w.envEvidence[obj]...)
				return value
			}
		}
		return "unknown"
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && cleanupCallReturnsContextAt(w.info.TypeOf(e), 0) {
			if helper := w.helpers[w.info.Uses[id]]; helper != nil {
				return w.helperContext(helper, e.Args, 0, depth+1)
			}
		}
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return "unknown"
		}
		obj := w.info.Uses[sel.Sel]
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "testing" && obj.Name() == "Context" &&
			types.TypeString(w.info.TypeOf(sel.X), nil) == "*testing.T" {
			// The admitted toolchain contract test checks this root remains deadline-free.
			return "unbounded"
		}
		if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "context" {
			return "unknown"
		}
		switch obj.Name() {
		case "Background", "TODO", "WithoutCancel":
			return "unbounded"
		case "WithTimeout", "WithDeadline", "WithTimeoutCause", "WithDeadlineCause":
			return "finite"
		case "WithCancel", "WithCancelCause":
			if len(e.Args) > 0 {
				return w.contextProvenance(e.Args[0], depth+1)
			}
		}
	}
	return "unknown"
}

func (w *cleanupWalker) helperContext(fn *ast.FuncDecl, args []ast.Expr, resultIndex, depth int) string {
	if depth > 12 || fn.Body == nil || w.activeHelpers[fn] {
		return "unknown"
	}
	directReturns := make(map[*ast.ReturnStmt]bool)
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			directReturns[ret] = true
		}
	}
	nestedReturn := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok && !directReturns[ret] {
			nestedReturn = true
		}
		return true
	})
	if nestedReturn {
		return "unknown"
	}
	w.activeHelpers[fn] = true
	defer delete(w.activeHelpers, fn)
	child := w.child(w.origin)
	offset := 0
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if offset < len(args) {
					child.env[w.info.Defs[name]] = w.contextProvenance(args[offset], depth+1)
				}
				offset++
			}
		}
	}
	result := ""
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			if len(ret.Results) <= resultIndex {
				return "unknown"
			}
			value := child.contextProvenance(ret.Results[resultIndex], depth+1)
			if result == "" {
				result = value
			} else if result != value {
				return "unknown"
			}
		} else {
			child.walkStmt(stmt)
		}
	}
	if result == "" {
		return "unknown"
	}
	w.provEvidence = append(w.provEvidence, cleanupHash(cleanupAST(fn.Body)))
	w.provEvidence = append(w.provEvidence, child.provEvidence...)
	return result
}

func cleanupCallReturnsContextAt(typ types.Type, index int) bool {
	if index == 0 && cleanupIsContextType(typ) {
		return true
	}
	tuple, ok := typ.(*types.Tuple)
	return ok && index < tuple.Len() && cleanupIsContextType(tuple.At(index).Type())
}

func cleanupModulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read root go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", errors.New("root go.mod has no module declaration")
}

func (w *cleanupWalker) evalIndexedProvenance(expr ast.Expr, index int) (string, []string) {
	if index == 0 {
		return w.evalProvenance(expr)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok || !cleanupCallReturnsContextAt(w.info.TypeOf(call), index) {
		return "unknown", nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "unknown", nil
	}
	helper := w.helpers[w.info.Uses[id]]
	if helper == nil {
		return "unknown", nil
	}
	saved := w.provEvidence
	w.provEvidence = nil
	state := w.helperContext(helper, call.Args, index, 0)
	evidence := append([]string(nil), w.provEvidence...)
	w.provEvidence = saved
	sort.Strings(evidence)
	return state, evidence
}

func (w *cleanupWalker) evalProvenance(expr ast.Expr) (string, []string) {
	saved := w.provEvidence
	w.provEvidence = nil
	state := w.contextProvenance(expr, 0)
	evidence := append([]string(nil), w.provEvidence...)
	w.provEvidence = saved
	sort.Strings(evidence)
	return state, evidence
}

func (w *cleanupWalker) followHelper(call *ast.CallExpr) {
	var obj types.Object
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		obj = w.info.Uses[fun]
	case *ast.SelectorExpr:
		obj = w.info.Uses[fun.Sel]
		if selection := w.info.Selections[fun]; selection != nil {
			obj = selection.Obj()
		}
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && w.isTypedManagerStopAll(sel) {
		return
	}
	fn := w.helpers[obj]
	if fn == nil || fn.Body == nil {
		return
	}
	// A declaration scan records its own candidate syntax; it does not prove
	// arbitrary callees reachable from a test cleanup. Likewise, an ordinary
	// test call into a production lifecycle method is a contract boundary,
	// unless the callee itself exposes testing/cleanup callback ownership.
	if w.declarationOnly || (w.origin == "ordinary" && !w.ordinaryHelperRelevant(fn)) {
		return
	}
	if !w.helperHasCandidate(fn, 0, make(map[*ast.FuncDecl]bool)) {
		return
	}
	if w.helperDepth >= 4 {
		w.recordUnresolvedEdge(call.Fun, "cleanup helper traversal exceeds bounded depth")
		return
	}
	if w.activeHelpers[fn] {
		w.sites = append(w.sites, cleanupSite{Path: w.path, Line: w.fset.Position(call.Pos()).Line,
			StartOffset: w.fset.Position(call.Pos()).Offset, EndOffset: w.fset.Position(call.End()).Offset,
			Function: w.function, DeclarationOnly: w.declarationOnly, Origin: w.origin, Target: "cyclic-helper", Context: "unknown", Classification: cleanupUnknown, Applicability: "unresolved",
			Reason: "statically resolved cleanup helper cycle", Fingerprint: cleanupHash(cleanupAST(call))})
		return
	}
	w.activeHelpers[fn] = true
	defer delete(w.activeHelpers, fn)
	child := w.child(w.origin)
	child.function = cleanupEnclosingName(fn)
	child.declarationOnly = false
	child.helperDepth = w.helperDepth + 1
	if rel, err := filepath.Rel(w.root, w.fset.Position(fn.Pos()).Filename); err == nil && !strings.HasPrefix(rel, "..") {
		child.path = filepath.ToSlash(rel)
	}
	offset := 0
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if offset < len(call.Args) {
					state, evidence := w.evalProvenance(call.Args[offset])
					child.env[w.info.Defs[name]] = state
					child.envEvidence[w.info.Defs[name]] = evidence
					// The helper's physical Stop site sees the declared receiver type.
					// Distinct constructor configurations of the same typed owner do
					// not create distinct cleanup obligations at that source site.
					// Direct local receiver bindings remain fingerprint-sensitive.
					if argType := w.info.TypeOf(call.Args[offset]); argType != nil {
						child.bindingEvidence[w.info.Defs[name]] = []string{cleanupHash(types.TypeString(argType, nil))}
					}
					if callable, ok := w.callableProvenance(call.Args[offset]); ok {
						child.callables[w.info.Defs[name]] = callable
					}
				}
				offset++
			}
		}
	}
	child.walkBlock(fn.Body)
	child.finish()
	w.sites = append(w.sites, child.sites...)
}

func (w *cleanupWalker) ordinaryHelperRelevant(fn *ast.FuncDecl) bool {
	file := w.fset.Position(fn.Pos()).Filename
	if strings.HasSuffix(file, "_test.go") {
		return true
	}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			typ := w.info.TypeOf(field.Type)
			if typ != nil && (strings.HasPrefix(types.TypeString(typ, nil), "*testing.") || types.TypeString(typ, nil) == "testing.TB") {
				return true
			}
		}
	}
	relevant := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cleanup" {
			relevant = true
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && cleanupIsTerminalCallback(w.info.TypeOf(id)) {
				relevant = true
			}
		}
		return !relevant
	})
	return relevant
}

func (w *cleanupWalker) helperHasCandidate(fn *ast.FuncDecl, depth int, active map[*ast.FuncDecl]bool) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	if depth >= 4 || active[fn] {
		// A bare ordinary call chain is not positive evidence of a cleanup
		// candidate. Once cleanup owns the path, this edge must remain visible.
		return w.origin != "ordinary"
	}
	active[fn] = true
	defer delete(active, fn)
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Cleanup" || sel.Sel.Name == "Stop") {
			found = true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		var obj types.Object
		funExpr := cleanupUnparen(call.Fun)
		switch fun := funExpr.(type) {
		case *ast.Ident:
			obj = w.info.Uses[fun]
			if cleanupIsTerminalCallback(w.info.TypeOf(fun)) {
				found = true
			}
			if _, variable := obj.(*types.Var); w.origin != "ordinary" && variable && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
				found = true
			}
		case *ast.SelectorExpr:
			obj = w.info.Uses[fun.Sel]
			if selection := w.info.Selections[fun]; selection != nil {
				obj = selection.Obj()
			}
			if _, variable := obj.(*types.Var); w.origin != "ordinary" && variable && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
				found = true
			}
		case *ast.IndexExpr:
			if w.origin != "ordinary" && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
				found = true
			}
		default:
			if _, literal := fun.(*ast.FuncLit); w.origin != "ordinary" && !literal && cleanupIsFunctionValue(w.info.TypeOf(fun)) {
				found = true
			}
		}
		if next := w.helpers[obj]; next != nil && w.helperHasCandidate(next, depth+1, active) {
			found = true
		}
		return !found
	})
	return found
}

func (w *cleanupWalker) siteApplicability() string {
	if w.origin == "defer" || w.origin == "cleanup" {
		return "cleanup-owned"
	}
	return w.applicability
}

func cleanupIsTestRoot(fn *ast.FuncDecl, info *types.Info) bool {
	name := fn.Name.Name
	if name == "TestMain" {
		return true
	}
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) && fn.Type.Params != nil {
			for _, field := range fn.Type.Params.List {
				typ := info.TypeOf(field.Type)
				if typ != nil && strings.HasPrefix(types.TypeString(typ, nil), "*testing.") {
					return true
				}
			}
		}
	}
	return false
}

func (w *cleanupWalker) recordUnresolvedEdge(expr ast.Expr, reason string) {
	pos := w.fset.Position(expr.Pos())
	w.sites = append(w.sites, cleanupSite{Path: w.path, StartOffset: pos.Offset, EndOffset: w.fset.Position(expr.End()).Offset,
		Line: pos.Line, Function: w.function, DeclarationOnly: w.declarationOnly, Origin: w.origin, Target: cleanupAST(expr), Receiver: "unresolved callback",
		Context: "unknown", Classification: cleanupUnknown, Applicability: "unresolved", Reason: reason,
		Fingerprint: cleanupHash(cleanupAST(expr) + "|" + w.origin + "|" + reason)})
}

func cleanupNestedModule(root, rel string) string {
	dir := filepath.Dir(rel)
	for dir != "." && dir != "" {
		if info, err := os.Stat(filepath.Join(root, dir, "go.mod")); err == nil && !info.IsDir() {
			return filepath.ToSlash(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (w *cleanupWalker) invalidateAssigned(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			obj := w.info.Uses[id]
			if obj == nil {
				obj = w.info.Defs[id]
			}
			if obj != nil {
				w.env[obj] = "unknown"
				w.assigned[obj] = true
				delete(w.envEvidence, obj)
				delete(w.bindingEvidence, obj)
				delete(w.callables, obj)
				delete(w.verifiedCancel, obj)
				delete(w.closures, obj)
				delete(w.returnedContextless, obj)
			}
		}
		return true
	})
}

// Package natsclient_request_guard_test is an AST-level lint that
// catches the silent-corruption class documented in gh#93 and the
// feedback_silent_handler_error_payload_audit.md discipline memory.
//
// # The bug class
//
// natsclient.Request / RequestWithRetry return the raw reply body as
// []byte. When a handler errors it sends "error: <msg>" as the body
// with err == nil — the caller has no transport error to branch on.
// Callers that go straight to json.Unmarshal silently mis-decode the
// "error: …" body as success data. This class shipped at least three
// times:
//
//	commit c626854  FindSimilar + searchEntitiesSemantic
//	commit 895ec44  searchGraph fallback adapter
//
// The structural fix is RequestClassified (gh#93 Phase 1), which
// surfaces handler errors via the error return. Until all callers
// are migrated, this lint prevents regressions.
//
// # Detection heuristic
//
// For each non-test, non-vendor .go file the linter:
//
//  1. Walks every function/method body.
//  2. Finds assignment statements where the RHS is a call to any
//     selector ending in "Request" or "RequestWithRetry" (the
//     unclassified legacy forms).
//  3. Records the variable name that receives the []byte result
//     (first LHS for "_, err := ..." or "resp, err := ...").
//  4. Scans subsequent statements in the same block for:
//     - json.Unmarshal(var, ...) — violation if reached first.
//     - bytes.HasPrefix(var, ...) — clears the taint (guard present).
//  5. Flags if the variable flows into json.Unmarshal without a
//     preceding bytes.HasPrefix guard in the same block.
//
// # False-positive risk
//
// The heuristic is block-local and name-based. It does not follow
// variables that are renamed, wrapped in a helper, or passed to
// sub-functions before unmarshal. Known safe patterns outside this
// scope are covered by the allowlist below.
//
// # Suppression
//
// Add a one-line entry to requestGuardAllowlist with a mandatory
// justification reference ("gh#NNN" or "deferred: reason").  A bare
// exemption without a reference causes an additional test failure.
//
// # Self-test
//
// TestRequestGuardSelfTest injects a deliberate violation into a
// temporary file and asserts the lint flags it. This proves the
// detection logic fires — a lint that matches nothing is a broken
// filter (feedback_investigate_before_classifying_as_flake.md).
package natsclient_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requestGuardAllowlist exempts known sites from the lint.
//
// Map key is "relative/path/to/file.go:funcName" — the relative path
// is relative to the repo root.  Value MUST contain a tracking
// reference ("gh#NNN" or a clearly-worded justification); a blank or
// one-word justification is rejected by TestRequestGuardAllowlistRefsNotBlank.
//
// Entries here are tracked violations, NOT silent exclusions.
// Dead entries (keys that match no live violation) are rejected by
// TestRequestGuardAllowlistNoDeadEntries — keep this list exact.
var requestGuardAllowlist = map[string]string{
	// gh#326 — graphrag global-search batch: loadEntities passes NATS
	// respData directly to json.Unmarshal without a bytes.HasPrefix guard.
	"processor/graph-query/graphrag.go:loadEntities": "deferred to gh#326: migrate to RequestClassified",

	// gh#326 — fetchEntityCommunityFromStorage in graphrag.go: clustering
	// community lookup uses .Request + json.Unmarshal without a guard.
	"processor/graph-query/graphrag.go:fetchEntityCommunityFromStorage": "deferred to gh#326: same graphrag batch as loadEntities",

	// gh#334 — handleQueryRelationships in query.go uses .Request + json.Unmarshal
	// for the graph-index outgoing/incoming query path. Fires twice in the
	// same function (incoming + outgoing branches) — one allowlist entry covers both.
	"processor/graph-query/query.go:handleQueryRelationships": "deferred to gh#334: migrate to RequestClassified",

	// gh#334 — entity_resolver: both resolveViaAlias and resolveViaSuffix
	// unmarshal respData from .Request without a guard.
	"processor/graph-query/entity_resolver.go:resolveViaAlias":  "deferred to gh#334: migrate to RequestClassified",
	"processor/graph-query/entity_resolver.go:resolveViaSuffix": "deferred to gh#334: migrate to RequestClassified",

	// gh#334 — pathrag: getOutgoingRelationships / getIncomingRelationships
	// unmarshal relsResponse from .Request without a bytes.HasPrefix guard.
	"processor/graph-query/pathrag.go:getOutgoingRelationships": "deferred to gh#334: migrate to RequestClassified",
	"processor/graph-query/pathrag.go:getIncomingRelationships": "deferred to gh#334: migrate to RequestClassified",

	// gh#334 — graph/query/client.go: four natsClient methods each call
	// .Request then json.Unmarshal without a guard.
	"graph/query/client.go:GetEntitiesByPredicate":  "deferred to gh#334: migrate to RequestClassified",
	"graph/query/client.go:ListPredicates":          "deferred to gh#334: migrate to RequestClassified",
	"graph/query/client.go:GetPredicateStats":       "deferred to gh#334: migrate to RequestClassified",
	"graph/query/client.go:QueryCompoundPredicates": "deferred to gh#334: migrate to RequestClassified",

	// gh#334 — graph/llm/nats_content_fetcher.go: fetchSingleEntity uses .Request
	// then json.Unmarshal without a guard.
	"graph/llm/nats_content_fetcher.go:fetchSingleEntity": "deferred to gh#334: migrate to RequestClassified",

	// gh#334 — pkg/lifecycle/graph_emit.go: update() and create() use
	// RequestWithRetry + json.Unmarshal without a bytes.HasPrefix guard.
	// Migration to RequestWithRetryClassified tracked under gh#334.
	"pkg/lifecycle/graph_emit.go:update": "deferred to gh#334: migrate to RequestWithRetryClassified",
	"pkg/lifecycle/graph_emit.go:create": "deferred to gh#334: migrate to RequestWithRetryClassified",

	// gh#334 — examples/github-pr-workflow: example component uses .Request
	// + json.Unmarshal without a guard. NOT graphrag (gh#326 is the global-search
	// batch only); this is a generic caller-to-migrate tracked in the gh#334
	// burn-down. Example code, low priority — but linted so the example doesn't
	// teach the footgun.
	"examples/github-pr-workflow/component.go:writeTriple": "deferred to gh#334: example code, migrate to RequestWithRetryClassified",

	// gh#334 — E2E test infrastructure: these are regular (non-_test.go) Go
	// files that the walker scans. They predate gh#93 and are low-priority;
	// migration is tracked under gh#334.
	"test/e2e/scenarios/tiered_structural.go:executeValidateReferentialStub": "deferred to gh#334: e2e test infrastructure, low priority migration",
	"test/e2e/client/nats.go:GetTrajectory":                                  "deferred to gh#334: e2e test client, low priority migration",
}

// requestMethodNames is the set of method names (selector suffixes)
// that constitute the unclassified legacy call forms. RequestClassified
// and RequestWithRetryClassified are SAFE — they surface handler errors
// via err before returning body bytes.
var requestMethodNames = map[string]bool{
	"Request":          true,
	"RequestWithRetry": true,
}

// TestRequestGuardAllowlistRefsNotBlank enforces that every allowlist
// entry has a non-trivial justification. A bare or one-word exemption
// is rejected to prevent quiet expansion of the allowlist without
// tracking references.
func TestRequestGuardAllowlistRefsNotBlank(t *testing.T) {
	t.Parallel()
	for key, justification := range requestGuardAllowlist {
		words := strings.Fields(justification)
		if len(words) < 3 {
			t.Errorf("allowlist entry %q has a blank or too-short justification %q — must include a gh# reference or multi-word reason", key, justification)
		}
	}
}

// TestRequestGuardAllowlistNoDeadEntries asserts that every key in
// requestGuardAllowlist matches at least one live violation found by
// the walker. A dead entry silently pre-allows a future violation and
// defeats the lint's purpose. This test walks the repo (skipping
// _test.go files, the same scope as TestRequestGuardProductionCode)
// and fails if any allowlist key is never hit.
func TestRequestGuardAllowlistNoDeadEntries(t *testing.T) {
	// Not parallel — full repo walk.
	root := mustFindRepoRoot(t)

	// Collect every key that the walker actually hits.
	hitKeys := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files — matches the scope of TestRequestGuardProductionCode.
		if strings.HasSuffix(filepath.Base(path), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		violations := checkFile(root, path, nil)
		for _, v := range violations {
			funcName := extractFuncName(v)
			hitKeys[relPath+":"+funcName] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	for key := range requestGuardAllowlist {
		if !hitKeys[key] {
			t.Errorf("allowlist entry %q is dead — no live violation matches it on current main; remove the entry", key)
		}
	}
}

// TestRequestGuardSelfTest injects a deliberate violation into a
// temp directory and asserts the lint catches it. This proves the
// detection logic is live — a lint that passes everywhere without
// ever matching anything is worthless.
func TestRequestGuardSelfTest(t *testing.T) {
	t.Parallel()

	// Craft a minimal Go file with the violation pattern:
	// Request call → json.Unmarshal of the response without a guard.
	violationSrc := `package fake

import (
	"context"
	"encoding/json"
	"time"
)

type fakeClient struct{}
func (c *fakeClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) { return nil, nil }

func violatingCaller(c *fakeClient) error {
	resp, err := c.Request(context.Background(), "subject", nil, time.Second)
	if err != nil {
		return err
	}
	var result struct{ Name string }
	if err := json.Unmarshal(resp, &result); err != nil {
		return err
	}
	return nil
}
`
	tmp := t.TempDir()
	violationFile := filepath.Join(tmp, "violation.go")
	if err := os.WriteFile(violationFile, []byte(violationSrc), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations := checkFile(tmp, violationFile, nil)
	if len(violations) == 0 {
		t.Fatal("self-test FAIL: lint did not detect the deliberate Request+Unmarshal-without-guard violation — the filter is broken")
	}
	t.Logf("self-test OK: lint correctly flagged %d violation(s) in the fixture", len(violations))
	for _, v := range violations {
		t.Logf("  %s", v)
	}
}

// TestRequestGuardSelfTestGuardedIsClean verifies that the lint does
// NOT flag a caller that properly checks bytes.HasPrefix before
// calling json.Unmarshal. This prevents false-positive regressions.
func TestRequestGuardSelfTestGuardedIsClean(t *testing.T) {
	t.Parallel()

	guardedSrc := `package fake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type fakeClient struct{}
func (c *fakeClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) { return nil, nil }

func guardedCaller(c *fakeClient) error {
	resp, err := c.Request(context.Background(), "subject", nil, time.Second)
	if err != nil {
		return err
	}
	if bytes.HasPrefix(resp, []byte("error: ")) {
		return fmt.Errorf("handler error: %s", resp)
	}
	var result struct{ Name string }
	if err := json.Unmarshal(resp, &result); err != nil {
		return err
	}
	return nil
}
`
	tmp := t.TempDir()
	guardedFile := filepath.Join(tmp, "guarded.go")
	if err := os.WriteFile(guardedFile, []byte(guardedSrc), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations := checkFile(tmp, guardedFile, nil)
	if len(violations) != 0 {
		t.Errorf("self-test FAIL: lint incorrectly flagged guarded caller (false positive):")
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
	} else {
		t.Log("self-test OK: guarded caller passes lint (no false positive)")
	}
}

// TestRequestGuardSelfTestClassifiedIsClean verifies that the lint
// does NOT flag callers using RequestClassified — the safe path.
func TestRequestGuardSelfTestClassifiedIsClean(t *testing.T) {
	t.Parallel()

	classifiedSrc := `package fake

import (
	"context"
	"encoding/json"
	"time"
)

type fakeClient struct{}
func (c *fakeClient) RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) { return nil, nil }

func classifiedCaller(c *fakeClient) error {
	resp, err := c.RequestClassified(context.Background(), "subject", nil, time.Second)
	if err != nil {
		return err
	}
	var result struct{ Name string }
	if err := json.Unmarshal(resp, &result); err != nil {
		return err
	}
	return nil
}
`
	tmp := t.TempDir()
	classifiedFile := filepath.Join(tmp, "classified.go")
	if err := os.WriteFile(classifiedFile, []byte(classifiedSrc), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations := checkFile(tmp, classifiedFile, nil)
	if len(violations) != 0 {
		t.Errorf("self-test FAIL: lint incorrectly flagged RequestClassified caller (false positive):")
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
	} else {
		t.Log("self-test OK: RequestClassified caller passes lint (no false positive)")
	}
}

// TestRequestGuardProductionCode is the main lint run over the full
// production codebase. It fails if it finds any Request+Unmarshal
// violations not present in requestGuardAllowlist.
func TestRequestGuardProductionCode(t *testing.T) {
	// Not parallel — walks the whole repo tree; parallelism doesn't help here.
	root := mustFindRepoRoot(t)

	type finding struct {
		relPath  string
		funcName string
		msg      string
	}
	var findings []finding
	var allowlistHits []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip vendor, hidden dirs, testdata.
			name := d.Name()
			if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files — the lint targets non-test production code. Test
		// files that call Request+Unmarshal (e.g. integration test helpers)
		// are lower risk: they fail loudly in test output rather than silently
		// corrupting production responses.
		if strings.HasSuffix(filepath.Base(path), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		violations := checkFile(root, path, nil)
		for _, v := range violations {
			// Extract function name from the violation string for allowlist lookup.
			funcName := extractFuncName(v)
			key := relPath + ":" + funcName

			if justification, ok := requestGuardAllowlist[key]; ok {
				allowlistHits = append(allowlistHits, fmt.Sprintf("%s — %s", key, justification))
				continue
			}
			findings = append(findings, finding{relPath: relPath, funcName: funcName, msg: v})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	if len(allowlistHits) > 0 {
		t.Logf("allowlisted violations (%d, all require gh# tracking):", len(allowlistHits))
		for _, h := range allowlistHits {
			t.Logf("  [allowlisted] %s", h)
		}
	}

	if len(findings) > 0 {
		t.Errorf("FAIL: %d Request+Unmarshal-without-guard violation(s) found (gh#162):", len(findings))
		t.Errorf("")
		t.Errorf("Each site below uses natsclient.Request or RequestWithRetry and then calls")
		t.Errorf("json.Unmarshal on the response without first checking bytes.HasPrefix(resp, []byte(\"error: \")).")
		t.Errorf("Fix by migrating to RequestClassified / RequestWithRetryClassified (gh#93),")
		t.Errorf("or add an explicit bytes.HasPrefix guard, or add a justified entry to")
		t.Errorf("requestGuardAllowlist in test/natsclient_request_guard_test.go with a gh# ref.")
		t.Errorf("")
		for _, f := range findings {
			t.Errorf("  VIOLATION: %s", f.msg)
		}
	}
}

// checkFile parses a single .go file and returns violation descriptions.
// root is the repo root (used to compute relative paths in messages).
// allowlist may be nil (used in self-test paths that don't want the
// production allowlist applied).
func checkFile(root, path string, _ map[string]string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// Unparseable files are not violations; they'll fail go build.
		return nil
	}

	relPath := path
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil {
			relPath = r
		}
	}

	var violations []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		funcName := fd.Name.Name
		vs := checkFuncBody(fset, fd.Body, relPath, funcName)
		violations = append(violations, vs...)
	}
	return violations
}

// checkFuncBody scans a single function body for the anti-pattern.
// It operates on the list of statements in the outermost block; nested
// blocks (if, for, switch) are flattened into the scan so that a
// Request call in an outer block and an Unmarshal in an inner block
// (a common real pattern) is still caught.
func checkFuncBody(fset *token.FileSet, body *ast.BlockStmt, relPath, funcName string) []string {
	// Collect all statements in DFS order (flatten nested blocks).
	stmts := flattenStmts(body)

	// taint tracks which variable names received a Request/RequestWithRetry
	// call result and have NOT yet been guarded by bytes.HasPrefix.
	// key: variable name; value: source position of the Request call.
	taint := make(map[string]token.Pos)

	var violations []string

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			// Look for:  resp, err := c.Request(...)  or  resp, _ := ...
			// or single:  resp := c.Request(...)
			if len(s.Rhs) == 1 {
				if isRequestCall(s.Rhs[0]) {
					// Record the first LHS variable (the []byte result).
					if len(s.Lhs) >= 1 {
						if ident, ok := s.Lhs[0].(*ast.Ident); ok && ident.Name != "_" {
							taint[ident.Name] = s.Pos()
						}
					}
				}
			}

			// Look for json.Unmarshal(taintedVar, ...) on the RHS.
			if len(s.Rhs) == 1 {
				if call, ok := s.Rhs[0].(*ast.CallExpr); ok {
					if isJSONUnmarshalCall(call) && len(call.Args) >= 1 {
						if varName := argIdent(call.Args[0]); varName != "" {
							if pos, tainted := taint[varName]; tainted {
								violations = append(violations, fmt.Sprintf(
									"%s:%d: func %s: json.Unmarshal(%s, ...) without bytes.HasPrefix guard (Request call at %s:%d)",
									relPath,
									fset.Position(call.Pos()).Line,
									funcName,
									varName,
									relPath,
									fset.Position(pos).Line,
								))
							}
						}
					}
				}
			}

		case *ast.ExprStmt:
			// Bare call statement: json.Unmarshal(resp, &v)
			if call, ok := s.X.(*ast.CallExpr); ok {
				if isJSONUnmarshalCall(call) && len(call.Args) >= 1 {
					if varName := argIdent(call.Args[0]); varName != "" {
						if pos, tainted := taint[varName]; tainted {
							violations = append(violations, fmt.Sprintf(
								"%s:%d: func %s: json.Unmarshal(%s, ...) without bytes.HasPrefix guard (Request call at %s:%d)",
								relPath,
								fset.Position(call.Pos()).Line,
								funcName,
								varName,
								relPath,
								fset.Position(pos).Line,
							))
						}
					}
				}

				// Detect bytes.HasPrefix(taintedVar, ...) — clears taint.
				if isBytesHasPrefixCall(call) && len(call.Args) >= 1 {
					if varName := argIdent(call.Args[0]); varName != "" {
						delete(taint, varName)
					}
				}
			}

		case *ast.IfStmt:
			// Detect bytes.HasPrefix in the condition of an if statement.
			// Pattern: if bytes.HasPrefix(resp, []byte("error: ")) { ... }
			// clearTaintFromExpr removes the variable from taint in-place.
			clearTaintFromExpr(s.Cond, taint)
		}
	}
	return violations
}

// flattenStmts does a depth-first walk of a block's statements,
// yielding them in encounter order. Nested block bodies (if/for/etc.)
// are included after the controlling statement so the taint set is
// populated before inner uses are checked.
func flattenStmts(body *ast.BlockStmt) []ast.Stmt {
	var out []ast.Stmt
	if body == nil {
		return out
	}
	for _, s := range body.List {
		out = append(out, s)
		switch inner := s.(type) {
		case *ast.IfStmt:
			// Inject the Init statement (e.g. "if err := json.Unmarshal(...); err != nil")
			// so the scan loop sees it as an ordinary statement.
			if inner.Init != nil {
				out = append(out, inner.Init)
			}
			if inner.Body != nil {
				out = append(out, flattenStmts(inner.Body)...)
			}
			if inner.Else != nil {
				switch e := inner.Else.(type) {
				case *ast.BlockStmt:
					out = append(out, flattenStmts(e)...)
				case *ast.IfStmt:
					out = append(out, flattenStmts(&ast.BlockStmt{List: []ast.Stmt{e}})...)
				}
			}
		case *ast.ForStmt:
			if inner.Body != nil {
				out = append(out, flattenStmts(inner.Body)...)
			}
		case *ast.RangeStmt:
			if inner.Body != nil {
				out = append(out, flattenStmts(inner.Body)...)
			}
		case *ast.SwitchStmt:
			if inner.Body != nil {
				for _, clause := range inner.Body.List {
					if cc, ok := clause.(*ast.CaseClause); ok {
						for _, cs := range cc.Body {
							out = append(out, cs)
						}
					}
				}
			}
		case *ast.SelectStmt:
			if inner.Body != nil {
				for _, clause := range inner.Body.List {
					if cc, ok := clause.(*ast.CommClause); ok {
						for _, cs := range cc.Body {
							out = append(out, cs)
						}
					}
				}
			}
		case *ast.BlockStmt:
			out = append(out, flattenStmts(inner)...)
		}
	}
	return out
}

// clearTaintFromExpr inspects an AST expression for bytes.HasPrefix
// calls that reference a tainted variable, and removes them from taint.
// Returns true if any taint was cleared.
func clearTaintFromExpr(expr ast.Expr, taint map[string]token.Pos) bool {
	if expr == nil {
		return false
	}
	cleared := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isBytesHasPrefixCall(call) && len(call.Args) >= 1 {
			if varName := argIdent(call.Args[0]); varName != "" {
				if _, tainted := taint[varName]; tainted {
					delete(taint, varName)
					cleared = true
				}
			}
		}
		return true
	})
	return cleared
}

// isRequestCall returns true if expr is a method call whose selector
// name is one of the legacy unclassified Request forms.
func isRequestCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return requestMethodNames[sel.Sel.Name]
}

// isJSONUnmarshalCall returns true if the call is json.Unmarshal(...)
// in any form (json.Unmarshal, encoding/json.Unmarshal via alias, etc.).
// We match on the selector name "Unmarshal" with a receiver ident
// commonly named "json"; this is the idiomatic form in the codebase.
func isJSONUnmarshalCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Unmarshal" {
		return false
	}
	// Accept any receiver name, but common patterns are "json" or an alias.
	// We accept all to avoid missing non-standard import aliases.
	return true
}

// isBytesHasPrefixCall returns true if the call is bytes.HasPrefix(...).
func isBytesHasPrefixCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "HasPrefix"
}

// argIdent extracts the identifier name from an AST expression if it
// is a simple identifier (e.g., "resp" in json.Unmarshal(resp, &v)).
// Returns "" for all other expression shapes.
func argIdent(expr ast.Expr) string {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// extractFuncName extracts the function name from a violation string
// produced by checkFuncBody. Violation format is:
//
//	"path/file.go:NN: func FuncName: ..."
func extractFuncName(violation string) string {
	// Split on ": func " to find the function name.
	const marker = ": func "
	idx := strings.Index(violation, marker)
	if idx == -1 {
		return "unknown"
	}
	rest := violation[idx+len(marker):]
	// The function name ends at the next ":"
	colonIdx := strings.IndexByte(rest, ':')
	if colonIdx == -1 {
		return rest
	}
	return rest[:colonIdx]
}

// mustFindRepoRoot walks up from the test's working directory until it
// finds a go.mod file and returns that directory. Tests run from the
// package directory under `go test ./...`; the repo root is needed to
// scan the full codebase. Pattern copied from test/reference_configs_test.go.
func mustFindRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (no go.mod found walking up from working dir)")
		}
		dir = parent
	}
}

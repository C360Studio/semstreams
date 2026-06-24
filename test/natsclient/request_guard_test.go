// Package natsclient_test holds the gh#337 replacement guard for the
// Request-classified silent-success class.
//
// # The footgun
//
// natsclient.Request / RequestWithRetry return a handler error as a
// SUCCESS-shaped reply body. Post-ADR-060 (v1.0.0-beta.115) that body is a
// {message,detail} JSON envelope and the legacy "error:" prefix + the
// consumer-side fallback are gone, so a caller that json.Unmarshal's (or
// re-emits) the body silently decodes an error as a zero-valued success
// (404 -> empty 200). The structural fix is RequestClassified /
// RequestWithRetryClassified, which surface the handler failure as a typed
// *errs.ClassifiedError via the err return.
//
// # Why this supersedes the deleted gh#162 lint
//
// The gh#162 lint tracked taint from a Request call to a following
// json.Unmarshal and flagged the pair. ADR-060 PR-D deleted it once its
// allowlist was empty — but it had two structural blind spots that PR-D's
// removal of the legacy fallback turned into live silent-success bugs
// (caught by hand in the PR-D review: graph-query summary.go, gateway/http,
// handleQueryEntityByAlias):
//
//   - json.Unmarshal buried inside an if-condition (BinaryExpr) — never scanned.
//   - passthrough / gateway re-emitters that return the body verbatim with NO
//     in-function json.Unmarshal at all — nothing to taint-match.
//
// This guard sidesteps both by flagging the raw Request CALL SITE itself,
// independent of how the response is later handled. The default answer for a
// new raw Request is "migrate to RequestClassified," not "add an allowlist
// entry."
//
// Residual gap (review-backstopped, NOT lint-caught): a method VALUE capture
// (fn := c.Request; fn(...)) or a reflect MethodByName("Request") call is not
// flagged — the detector matches Request only in call-function position. No such
// site exists in production today (grep-verified); the guard is a structural
// backstop for the common shapes, not an exhaustive proof. The boundary is
// pinned by TestRequestGuardKnownGapMethodValueCapture.
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

// requestMethodNames are the UNCLASSIFIED natsclient request methods. Their
// classified siblings (RequestClassified / RequestWithRetryClassified) are the
// safe path and are deliberately NOT matched — the check is on the exact
// selector name, not a prefix.
var requestMethodNames = map[string]bool{
	"Request":          true,
	"RequestWithRetry": true,
}

// requestGuardAllowlist maps "relpath:funcName" to a justification for a
// legitimate raw-Request call site. Every entry must carry a gh# ref or a
// multi-word reason (TestRequestGuardAllowlistRefsNotBlank), and every entry
// must match a live call site (TestRequestGuardNoDeadAllowlistEntries) so the
// list cannot silently pre-authorize a future violation.
//
// Keep this minimal. Production RPC callers belong on RequestClassified.
var requestGuardAllowlist = map[string]string{
	"test/e2e/client/nats.go:Request": "e2e test-harness low-level passthrough: returns raw reply bytes for e2e assertions (a handler error fails loudly in e2e output, not silently in production). The harness exposes a separate RequestClassified for the mutation lane. gh#337.",
}

// violation is a single raw-Request call site.
type violation struct {
	funcName string
	msg      string
}

// TestRequestGuardProductionCode is the gh#337 guard: it fails if any
// production (.go, non-_test) file outside the natsclient package itself calls
// the raw Request / RequestWithRetry methods without an allowlist entry.
func TestRequestGuardProductionCode(t *testing.T) {
	root := mustFindRepoRoot(t)

	var findings []string
	var allowlistHits []string

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
		rel, skip := productionGoFile(root, path)
		if skip {
			return nil
		}
		for _, v := range checkFile(path, rel) {
			key := rel + ":" + v.funcName
			if just, ok := requestGuardAllowlist[key]; ok {
				allowlistHits = append(allowlistHits, fmt.Sprintf("%s — %s", key, just))
				continue
			}
			findings = append(findings, v.msg)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	for _, h := range allowlistHits {
		t.Logf("[allowlisted] %s", h)
	}

	if len(findings) > 0 {
		t.Errorf("FAIL: %d raw natsclient.Request / RequestWithRetry call site(s) in production code (gh#337):", len(findings))
		t.Errorf("")
		t.Errorf("Raw Request returns a handler error as a success-shaped {message,detail} body with err==nil;")
		t.Errorf("the caller silently decodes it as a zero-valued success (e.g. 404 -> empty 200).")
		t.Errorf("Fix: use RequestClassified / RequestWithRetryClassified (surfaces handler errors via err),")
		t.Errorf("or, if the call site is genuinely safe, add a justified entry to requestGuardAllowlist.")
		t.Errorf("")
		for _, f := range findings {
			t.Errorf("  VIOLATION: %s", f)
		}
	}
}

// TestRequestGuardAllowlistRefsNotBlank enforces that every allowlist entry has
// a substantive justification (a gh# ref or a multi-word reason), so the list
// cannot expand quietly with empty rationales.
func TestRequestGuardAllowlistRefsNotBlank(t *testing.T) {
	for key, just := range requestGuardAllowlist {
		trimmed := strings.TrimSpace(just)
		if len(trimmed) < 12 || (!strings.Contains(trimmed, "gh#") && len(strings.Fields(trimmed)) < 3) {
			t.Errorf("allowlist entry %q has a weak justification %q — require a gh# ref or a multi-word reason", key, just)
		}
	}
}

// TestRequestGuardNoDeadAllowlistEntries fails if any allowlist key no longer
// matches a live call site. A dead entry silently pre-authorizes a future
// violation and defeats the guard.
func TestRequestGuardNoDeadAllowlistEntries(t *testing.T) {
	root := mustFindRepoRoot(t)
	live := make(map[string]bool)

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
		rel, skip := productionGoFile(root, path)
		if skip {
			return nil
		}
		for _, v := range checkFile(path, rel) {
			live[rel+":"+v.funcName] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	for key := range requestGuardAllowlist {
		if !live[key] {
			t.Errorf("allowlist entry %q is dead — no live raw-Request call site matches it on current main; remove the entry", key)
		}
	}
}

// TestRequestGuardSelfTestFlagsRawRequest proves the detector fires on a raw
// Request call (covering a former blind spot: the call is inside an if-init,
// with no following json.Unmarshal in the function at all).
func TestRequestGuardSelfTestFlagsRawRequest(t *testing.T) {
	src := `package fixture
func passthrough(c client, ctx any) ([]byte, error) {
	// No json.Unmarshal anywhere — a re-emitter the gh#162 taint lint missed.
	return c.Request(ctx, "subj", nil, 0)
}
func inIfCond(c client, ctx any) {
	if resp, err := c.Request(ctx, "subj", nil, 0); err == nil {
		_ = resp
	}
}
func rawRetry(c client, ctx any) ([]byte, error) {
	// The RequestWithRetry arm must be flagged too (regression lock for the map entry).
	return c.RequestWithRetry(ctx, "subj", nil, 0, nil)
}`
	violations := checkFixture(t, src)
	if len(violations) != 3 {
		t.Fatalf("self-test FAIL: expected the detector to flag all THREE raw call sites (Request passthrough + Request if-cond + RequestWithRetry), got %d: %v", len(violations), violations)
	}
	t.Logf("self-test OK: detector flagged all three raw call sites (passthrough re-emitter + Unmarshal-less if-cond + RequestWithRetry)")
}

// TestRequestGuardKnownGapMethodValueCapture pins a known, review-backstopped
// limitation: a method VALUE capture (fn := c.Request) is NOT flagged because
// the detector only matches Request in call-function position. No such site
// exists in production (grep-verified). This test makes the boundary explicit
// rather than surprising; if the detector is ever upgraded to catch this, flip
// the assertion and update the package-doc Residual gap note.
func TestRequestGuardKnownGapMethodValueCapture(t *testing.T) {
	src := `package fixture
func captured(c client, ctx any) ([]byte, error) {
	fn := c.Request
	return fn(ctx, "subj", nil, 0)
}`
	violations := checkFixture(t, src)
	if len(violations) != 0 {
		t.Fatalf("the documented method-value gap changed (expected 0 flagged, got %d) — update the package-doc Residual gap note: %v", len(violations), violations)
	}
	t.Logf("known gap documented: method-value capture (fn := c.Request) is not flagged (review-backstopped)")
}

// TestRequestGuardSelfTestIgnoresClassified proves the detector does NOT flag
// the safe RequestClassified / RequestWithRetryClassified call sites.
func TestRequestGuardSelfTestIgnoresClassified(t *testing.T) {
	src := `package fixture
func safe(c client, ctx any) ([]byte, error) {
	if resp, err := c.RequestClassified(ctx, "subj", nil, 0); err == nil {
		return resp, nil
	}
	return c.RequestWithRetryClassified(ctx, "subj", nil, 0, nil)
}`
	violations := checkFixture(t, src)
	if len(violations) != 0 {
		t.Fatalf("self-test FAIL: detector flagged a classified caller (false positive): %v", violations)
	}
	t.Logf("self-test OK: classified callers pass clean (no false positive)")
}

// checkFile parses one .go file and returns every raw-Request call site, tagged
// with its enclosing top-level function name.
func checkFile(path, rel string) []violation {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// Unparseable files are not violations; they fail `go build` instead.
		return nil
	}
	var out []violation
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		funcName := fd.Name.Name
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !requestMethodNames[sel.Sel.Name] {
				return true
			}
			out = append(out, violation{
				funcName: funcName,
				msg: fmt.Sprintf("%s:%d: func %s: raw .%s(...) — use %sClassified (gh#337)",
					rel, fset.Position(call.Pos()).Line, funcName, sel.Sel.Name, sel.Sel.Name),
			})
			return true
		})
	}
	return out
}

// checkFixture writes src to a temp file and runs checkFile over it.
func checkFixture(t *testing.T, src string) []violation {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return checkFile(path, "fixture.go")
}

// productionGoFile reports the repo-relative path and whether the file should be
// skipped: non-.go, _test.go, or inside the natsclient package itself (where
// Request is the building block for RequestClassified — correct by construction).
func productionGoFile(root, path string) (rel string, skip bool) {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(filepath.Base(path), "_test.go") {
		return "", true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", true
	}
	rel = filepath.ToSlash(rel)
	if rel == "natsclient" || strings.HasPrefix(rel, "natsclient/") {
		return "", true
	}
	return rel, false
}

// mustFindRepoRoot walks up from the test's working directory to the dir holding
// go.mod. Tests run from the package directory under `go test ./...`.
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

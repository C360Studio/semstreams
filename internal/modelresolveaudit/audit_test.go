package modelresolveaudit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModelResolveAudit_NoUnhandledLookups is the real CI gate. It scans owned
// production source and fails if any name-keyed model-registry lookup skips
// capability resolution and is neither provably resolved nor annotated. Green =
// every current site is handled (resolved, annotated, or the resolver's home).
func TestModelResolveAudit_NoUnhandledLookups(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatalf("Audit(%q) error: %v", root, err)
	}
	if len(findings) != 0 {
		var b strings.Builder
		for _, f := range findings {
			fmt.Fprintf(&b, "\n  %s:%d %s arg=%q\n    %s", f.File, f.Line, f.Surface, f.Arg, f.Reason)
		}
		t.Fatalf("found %d unresolved model-registry lookup(s) that skip capability resolution:%s", len(findings), b.String())
	}
}

func TestAuditFlagsRawUnresolvedGetEndpoint(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "lookup.go", `package fixture
func f(reg R, name string) *E {
	return reg.GetEndpoint(name)
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 1 || findings[0].Surface != SurfaceGetEndpoint {
		t.Fatalf("findings = %#v, want one GetEndpoint finding", findings)
	}
}

func TestAuditFlagsRawGetMaxTokens(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "limit.go", `package fixture
func f(reg R, name string) int {
	return reg.GetMaxTokens(name)
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 1 || findings[0].Surface != SurfaceGetMaxTokens {
		t.Fatalf("findings = %#v, want one GetMaxTokens finding", findings)
	}
}

func TestAuditFlagsRawEndpointsIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "index.go", `package fixture
func f(reg R, name string) *E {
	return reg.Endpoints[name]
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 1 || findings[0].Surface != SurfaceEndpointsIndex {
		t.Fatalf("findings = %#v, want one Endpoints index finding", findings)
	}
}

func TestAuditPassesResolveEndpointNameFed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "resolved.go", `package fixture
func f(reg R, name string) *E {
	return reg.GetEndpoint(model.ResolveEndpointName(reg, name))
}
`)
	if findings := mustAudit(t, root); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none for ResolveEndpointName-fed lookup", findings)
	}
}

func TestAuditPassesResolveVarFed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "resolvedvar.go", `package fixture
func f(reg R, name string) *E {
	resolved := model.ResolveEndpointName(reg, name)
	return reg.GetEndpoint(resolved)
}
`)
	if findings := mustAudit(t, root); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none for ResolveEndpointName-fed variable", findings)
	}
}

// Bare reg.Resolve is deliberately NOT auto-passed: it maps a direct endpoint
// name to Defaults.Model (a different wrong-endpoint bug) and over-matches
// unrelated packages. Both the inline-call and the assigned-var shapes must
// require an explicit annotation.
func TestAuditBareResolveIsNotAutoPassed(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name    string
		content string
	}{
		{
			name: "inline_call.go",
			content: `package fixture
func f(reg R, capability string) *E {
	return reg.GetEndpoint(reg.Resolve(capability))
}
`,
		},
		{
			name: "assigned_var.go",
			content: `package fixture
func f(reg R, capability string) *E {
	resolved := reg.Resolve(capability)
	return reg.GetEndpoint(resolved)
}
`,
		},
	} {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, fixture.name, fixture.content)
			if findings := mustAudit(t, root); len(findings) != 1 {
				t.Fatalf("findings = %#v, want one finding (bare reg.Resolve is not auto-passed)", findings)
			}
		})
	}
}

func TestAuditReassignedVarIsNotResolved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "reassigned.go", `package fixture
func f(reg R, name, raw string) *E {
	resolved := model.ResolveEndpointName(reg, name)
	resolved = raw
	return reg.GetEndpoint(resolved)
}
`)
	if findings := mustAudit(t, root); len(findings) != 1 {
		t.Fatalf("findings = %#v, want one finding after the var is reassigned to a raw name", findings)
	}
}

func TestAuditVarScopeDoesNotLeakAcrossFunctions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "scope.go", `package fixture
func a(reg R, name string) *E {
	resolved := model.ResolveEndpointName(reg, name)
	return reg.GetEndpoint(resolved)
}
func b(reg R, resolved string) *E {
	return reg.GetEndpoint(resolved)
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want the second function flagged (no in-function resolution)", findings)
	}
}

// TestAuditInlineAnnotationDoesNotBleedToNextLine is the regression guard for
// the annotation adjacency-bleed false negative: an inline allow on a lookup's
// own line must NOT shield a raw lookup on the line immediately below it.
func TestAuditInlineAnnotationDoesNotBleedToNextLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// The raw GetEndpoint(capability) sits on the line IMMEDIATELY below the
	// inline-annotated lookup; the pre-fix `line == a.line || line == a.line+1`
	// rule shielded it too, yielding a false-negative 0 findings.
	writeFixture(t, root, "bleed.go", `package fixture
func f(reg R, name, capability string) *E {
	if ep := reg.GetEndpoint(name); ep != nil { // modelresolveaudit:allow inline on its own lookup line
		return reg.GetEndpoint(capability)
	}
	return nil
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want exactly one finding (the raw lookup below the inline annotation)", findings)
	}
	if findings[0].Arg != "capability" {
		t.Fatalf("finding = %#v, want the unshielded lookup on the following line", findings[0])
	}
}

func TestAuditPassesAnnotatedInline(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "annotated.go", `package fixture
func f(reg R, name string) *E {
	return reg.GetEndpoint(name) // modelresolveaudit:allow real endpoint name from ListEndpoints
}
`)
	if findings := mustAudit(t, root); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none for annotated lookup", findings)
	}
}

func TestAuditPassesAnnotatedOnPrecedingLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "annotated_above.go", `package fixture
func f(reg R, name string) *E {
	// modelresolveaudit:allow real endpoint name from ListEndpoints
	return reg.GetEndpoint(name)
}
`)
	if findings := mustAudit(t, root); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none for lookup annotated on the preceding line", findings)
	}
}

func TestAuditRejectsStaleAnnotation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "stale.go", `package fixture
// modelresolveaudit:allow orphan reason
var x = 1
`)
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want stale annotation rejection")
	}
}

func TestAuditExcludesResolverHome(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "model/registry.go", `package model
func (r *Registry) GetMaxTokens(name string) int {
	ep, ok := r.Endpoints[ResolveEndpointName(r, name)]
	if !ok {
		return 0
	}
	return ep.MaxTokens
}
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 0 {
		t.Fatalf("candidates = %#v, findings = %#v, want model/registry.go fully excluded", candidates, findings)
	}
}

func TestAuditExcludesAuditHome(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "internal/modelresolveaudit/copy.go", `package modelresolveaudit
func f(reg R, name string) *E {
	return reg.GetEndpoint(name) // modelresolveaudit:allow prose token in the audit's own home
}
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 0 {
		t.Fatalf("candidates = %#v, findings = %#v, want the audit's own home excluded", candidates, findings)
	}
}

func TestAuditSkipsTestFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "reg_test.go", `package fixture
func f(reg R, name string) *E {
	return reg.GetEndpoint(name)
}
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 0 {
		t.Fatalf("candidates = %#v, findings = %#v, want _test.go skipped", candidates, findings)
	}
}

func TestAuditCallPathResolutionShapeFlagsEachSite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// A chain -> direct -> default resolution walk resolves capabilities
	// correctly overall, but each raw GetEndpoint is heuristically flagged and
	// intended to carry its own allow annotation.
	writeFixture(t, root, "callpath.go", `package fixture
func resolveEndpoint(reg R, model string) *E {
	chain := reg.GetFallbackChain(model)
	for _, name := range chain {
		if ep := reg.GetEndpoint(name); ep != nil {
			return ep
		}
	}
	return reg.GetEndpoint(reg.GetDefault())
}
`)
	findings := mustAudit(t, root)
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want two raw GetEndpoint sites flagged", findings)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test working directory")
		}
		dir = parent
	}
}

func mustAudit(t *testing.T, root string) []Finding {
	t.Helper()
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

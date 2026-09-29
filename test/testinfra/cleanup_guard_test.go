package testinfra_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupFixtureModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.com/cleanupfixture\n\ngo 1.26.3\n"
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	return root
}

func TestCleanupRootGuardTypedFixture(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import (
    "context"
    "testing"
    "time"
)
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestUnbounded(t *testing.T) {
    o := &owner{}
    defer o.Stop(context.Background())
}
func TestFinite(t *testing.T) {
    o := &owner{}
    t.Cleanup(func() {
        ctx, cancel := context.WithTimeout(context.Background(), time.Second)
        defer cancel()
        _ = o.Stop(ctx)
    })
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("want unbounded refusal, got %v", err)
	}
	got := map[string]string{}
	for _, site := range report.Sites {
		got[site.Function] = site.Classification
	}
	if got["TestUnbounded"] != "unbounded-terminal-cleanup" || got["TestFinite"] != "bounded-cleanup" {
		t.Fatalf("classifications = %#v", got)
	}
}

func TestCleanupRootGuardProvenanceFixture(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import (
    c "context"
    "testing"
    "time"
)
type owner struct{}
func (*owner) Stop(c.Context) error { return nil }
func finite() (c.Context, c.CancelFunc) { return c.WithTimeout(c.Background(), time.Second) }
func TestAliasAndMethodExpression(t *testing.T) {
    o := &owner{}
    ctx := c.Background()
    ctx, cancel := c.WithCancel(ctx)
    defer cancel()
    defer (*owner).Stop(o, ctx)
}
func TestFiniteHelper(t *testing.T) {
    o := &owner{}
    ctx, cancel := finite()
    defer cancel()
    defer o.Stop(ctx)
}
func TestDeadlineRemoval(t *testing.T) {
    o := &owner{}
    ctx, cancel := c.WithTimeout(c.Background(), time.Second)
    defer cancel()
    ctx = c.WithoutCancel(ctx)
    defer o.Stop(ctx)
}
func TestDeferCapture(t *testing.T) {
    o := &owner{}
    ctx, cancel := c.WithTimeout(c.Background(), time.Second)
    defer cancel()
    defer o.Stop(ctx)
    t.Cleanup(func() { _ = o.Stop(ctx) })
    ctx = c.Background()
}
`,
	})
	report, _ := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	got := map[string][]string{}
	for _, site := range report.Sites {
		if strings.HasSuffix(site.Target, ".Stop") {
			got[site.Function] = append(got[site.Function], site.Classification)
		}
	}
	want := map[string][]string{
		"TestAliasAndMethodExpression": {"unbounded-terminal-cleanup"},
		"TestFiniteHelper":             {"bounded-cleanup"},
		"TestDeadlineRemoval":          {"unbounded-terminal-cleanup"},
		"TestDeferCapture":             {"bounded-cleanup", "unbounded-terminal-cleanup"},
	}
	for function, classes := range want {
		if strings.Join(got[function], ",") != strings.Join(classes, ",") {
			t.Errorf("%s classes = %v, want %v", function, got[function], classes)
		}
	}
}

func TestCleanupRootGuardBaselineFreshness(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context"; "testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { defer (&owner{}).Stop(context.Background()) }
`,
	})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("want one proposed unbounded site, got sites=%d err=%v", len(report.Sites), err)
	}
	site := report.Sites[0]
	baseline := cleanupBaseline{Version: 1, Entries: []cleanupApproval{{
		Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: site.Classification,
		Reason: "reviewed fixture debt", OwnerIssue: "#1064",
	}}}
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("exact baseline refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(`package fixture
import ("context"; "testing"; "time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { ctx,cancel:=context.WithTimeout(context.Background(),time.Second); defer cancel(); defer (&owner{}).Stop(ctx) }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("want stale approval refusal, got %v", err)
	}
}

func TestCleanupRootGuard(t *testing.T) {
	root := findRepoRoot(t)
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "test/testinfra/cleanup_baseline.json"), nil)
	serialized, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err != nil {
		t.Fatalf("cleanup root guard: %v\nCensus report:\n%s", err, serialized)
	}
	t.Logf("cleanup root guard: %d sources, %d typed, %d sites, %d exclusions", report.Sources, report.TypedSources, len(report.Sites), len(report.Exclusions))
}

func TestCleanupRootGuardCrossFileHelperEvidence(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"helper_test.go": `package fixture
import "context"
func terminalContext() context.Context { return context.Background() }
`,
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { defer (&owner{}).Stop(terminalContext()) }
`,
	})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnbounded {
		t.Fatalf("cross-file helper must resolve unbounded: sites=%+v err=%v", report.Sites, err)
	}
	site := report.Sites[0]
	raw, err := json.Marshal(cleanupBaseline{Version: 1, Entries: []cleanupApproval{{
		Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: site.Classification,
		Reason: "reviewed fixture debt", OwnerIssue: "#1064",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("unchanged helper refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "helper_test.go"), []byte(`package fixture
import "context"
func terminalContext() context.Context { return context.TODO() }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed helper evidence must stale approval: %v", err)
	}
}

func TestCleanupRootGuardSameLineCardinality(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { o:=&owner{}; defer o.Stop(context.Background()); defer o.Stop(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 2 {
		t.Fatalf("want two physical sites, got %d: %v", len(report.Sites), err)
	}
	if report.Sites[0].Identity == report.Sites[1].Identity {
		t.Fatalf("identical calls collapsed: %+v", report.Sites)
	}
	if report.Sites[0].Classification != cleanupUnbounded || report.Sites[1].Classification != cleanupUnbounded {
		t.Fatalf("same-line classes: %+v", report.Sites)
	}
}

func TestCleanupRootGuardOrdinaryCallNeedsReview(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestContract(t *testing.T) {
    o:=&owner{}
    _=o.Stop(nil)
    _=o.Stop(context.Background())
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 2 {
		t.Fatalf("want two visible ordinary review sites without cleanup refusal, got %d: %v", len(report.Sites), err)
	}
	for _, site := range report.Sites {
		if site.Classification != cleanupUnknown || site.Applicability != "ordinary-only" {
			t.Errorf("ordinary contract candidate classified %s/%s: %+v", site.Classification, site.Applicability, site)
		}
	}
}

func TestCleanupRootGuardManualResolutionDependencyFreshness(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
func terminalStop(ctx context.Context) error { return nil }
var stop = terminalStop
func TestDebt(t *testing.T) { defer stop(context.Background()) }
`,
	})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown {
		t.Fatalf("want unknown callback: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "owner_test.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"owner_test.go": parsed}, "owner_test.go", "terminalStop")
	if err != nil {
		t.Fatal(err)
	}
	baseline := cleanupBaseline{Version: 1,
		Entries: []cleanupApproval{{Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded, Reason: "reviewed fixture debt", OwnerIssue: "#1064"}},
		Resolutions: []cleanupResolution{{Identity: site.Identity, SiteFingerprint: site.Fingerprint, Classification: cleanupUnbounded,
			Question: "is the callback a lifecycle terminal owner", Reason: "the global callback is terminalStop", Reviewer: "fixture-reviewer", OwnerIssue: "#1064",
			Dependencies: []cleanupDependency{{Path: "owner_test.go", Declaration: "terminalStop", Fingerprint: fingerprint}},
		}},
	}
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("reviewed exact unknown refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(`package fixture
import ("context";"testing")
func terminalStop(ctx context.Context) error { _ = ctx; return nil }
var stop = terminalStop
func TestDebt(t *testing.T) { defer stop(context.Background()) }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale manual cleanup resolution dependency") {
		t.Fatalf("changed dependency must stale resolution: %v", err)
	}
}

func TestCleanupRootGuardMethodValueAndContextlessRegistration(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
type set struct{}
func (*set) Stop() {}
func TestValue(t *testing.T) {
    o:=&owner{}
    stop:=o.Stop
    defer stop(context.Background())
}
func TestExpression(t *testing.T) {
    o:=&owner{}
    stop:=(*owner).Stop
    defer stop(o,context.Background())
}
func TestContextless(t *testing.T) { s:=&set{}; t.Cleanup(s.Stop) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatal("method value debt must refuse")
	}
	got := map[string][]string{}
	for _, site := range report.Sites {
		got[site.Function] = append(got[site.Function], site.Classification)
	}
	for _, function := range []string{"TestValue", "TestExpression"} {
		if strings.Join(got[function], ",") != cleanupUnbounded {
			t.Errorf("%s = %v, want unbounded", function, got[function])
		}
	}
	if strings.Join(got["TestContextless"], ",") != cleanupNonLifecycle {
		t.Errorf("contextless registration = %v", got["TestContextless"])
	}
}

func TestCleanupRootGuardDeferredClosureReadsLaterBinding(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestClosure(t *testing.T) {
    o:=&owner{}
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    defer func(){ _=o.Stop(ctx) }()
    ctx=context.Background()
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnbounded {
		t.Fatalf("deferred closure must see later binding: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardCleanupHelperOwnership(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func stopOwner(o *owner,ctx context.Context) { _=o.Stop(ctx) }
func registerCleanup(t *testing.T,o *owner) { t.Cleanup(func(){ stopOwner(o,context.Background()) }) }
func TestDebt(t *testing.T) { registerCleanup(t,&owner{}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatal("unbounded helper cleanup must refuse")
	}
	var found bool
	for _, site := range report.Sites {
		if site.Function == "stopOwner" && site.Origin == "cleanup" && site.Classification == cleanupUnbounded {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleanup helper ownership was not propagated: %+v", report.Sites)
	}
}

func TestCleanupRootGuardNestedTestCallbackSites(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"suite.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestSuite(t *testing.T) {
    t.Run("case",func(t *testing.T) {
        o:=&owner{}
        _=o.Stop(context.Background())
        _=o.Stop(context.Background())
    })
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 2 {
		t.Fatalf("want two visible ordinary nested callback records, got %+v, %v", report.Sites, err)
	}
	for _, site := range report.Sites {
		if site.Classification != cleanupUnknown {
			t.Errorf("nested ordinary call: %+v", site)
		}
	}
}

func TestCleanupRootGuardUntypedNestedCallbackCandidate(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"root_test.go": `package fixture
import "testing"
func TestRoot(t *testing.T) {}
`,
		"nested/go.mod": "module example.com/nested\n\ngo 1.26.3\n",
		"nested/callback_test.go": `package nested
import "context"
func stop(context.Context) error { return nil }
func testCleanup() { defer stop(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatal("untyped nested callback candidate must refuse")
	}
	var found bool
	for _, site := range report.Sites {
		if site.Path == "nested/callback_test.go" && site.Classification == cleanupUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("nested candidate omitted: %+v", report.Sites)
	}
}

func TestCleanupRootGuardUnresolvedCleanupRegistration(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import "testing"
var opaque func()
func TestOpaque(t *testing.T) { t.Cleanup(opaque) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("opaque Cleanup must refuse with one edge: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	if site.Classification != cleanupUnknown || site.Applicability != "unresolved" {
		t.Fatalf("opaque Cleanup edge: %+v", site)
	}
}

func TestCleanupRootGuardUnresolvedDeferredCallback(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import "testing"
var opaque func()
func TestOpaque(t *testing.T) { defer opaque() }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Applicability != "unresolved" {
		t.Fatalf("opaque deferred callback must refuse: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardReturnedMethodValue(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func terminal(o *owner) func(context.Context) error { return o.Stop }
func TestDebt(t *testing.T) { stop:=terminal(&owner{}); defer stop(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatal("returned Stop method value must refuse")
	}
	var found bool
	for _, site := range report.Sites {
		if site.Function == "TestDebt" && site.Classification == cleanupUnbounded && strings.Contains(site.Target, ".Stop") {
			found = true
		}
	}
	if !found {
		t.Fatalf("returned method value unresolved: %+v", report.Sites)
	}
}

func TestCleanupRootGuardUnsupportedFlowFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func branchy(flag bool) context.Context {
    if flag { return context.Background() }
    ctx,_:=context.WithTimeout(context.Background(),time.Second)
    return ctx
}
func pair() (int,context.Context) { return 1,context.Background() }
func TestBranchy(t *testing.T) { defer (&owner{}).Stop(branchy(true)) }
func TestTuple(t *testing.T) { _,ctx:=pair(); defer (&owner{}).Stop(ctx) }
func TestConditionalClosure(t *testing.T) {
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    if true { t.Cleanup(func(){ _=(&owner{}).Stop(ctx) }) }
    ctx=context.Background()
}
func TestLoopMutation(t *testing.T) {
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    for i:=0;i<1;i++ { ctx=context.Background() }
    defer (&owner{}).Stop(ctx)
}
func TestSwitchMutation(t *testing.T) {
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    switch 1 { case 1: ctx=context.Background() }
    defer (&owner{}).Stop(ctx)
}
`,
	})
	report, _ := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	got := map[string]string{}
	for _, site := range report.Sites {
		got[site.Function] = site.Classification
	}
	if got["TestBranchy"] != cleanupUnknown {
		t.Errorf("branchy helper = %s, want unknown", got["TestBranchy"])
	}
	if got["TestTuple"] != cleanupUnbounded {
		t.Errorf("tuple second context = %s, want unbounded", got["TestTuple"])
	}
	if got["TestConditionalClosure"] != cleanupUnbounded {
		t.Errorf("conditional closure = %s, want unbounded", got["TestConditionalClosure"])
	}
	if got["TestLoopMutation"] != cleanupUnknown {
		t.Errorf("loop mutation = %s, want unknown", got["TestLoopMutation"])
	}
	if got["TestSwitchMutation"] != cleanupUnknown {
		t.Errorf("switch mutation = %s, want unknown", got["TestSwitchMutation"])
	}
}

func TestCleanupRootGuardSameClassBindingChangeStales(t *testing.T) {
	source := `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { ctx:=context.Background(); defer (&owner{}).Stop(ctx) }
`
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("want initial debt: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	baseline := cleanupBaseline{Version: 1, Entries: []cleanupApproval{{Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded, Reason: "reviewed fixture debt", OwnerIssue: "#1064"}}}
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("unchanged baseline: %v", err)
	}
	changed := strings.Replace(source, "ctx:=context.Background()", "ctx:=context.TODO()", 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("same-class binding change must stale approval: %v", err)
	}
}

func TestCleanupRootGuardSameClassBranchChangeStales(t *testing.T) {
	source := `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) {
    ctx:=context.Background()
    if true { ctx=context.Background() }
    defer (&owner{}).Stop(ctx)
}
`
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("want initial debt: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	raw, err := json.Marshal(cleanupBaseline{Version: 1, Entries: []cleanupApproval{{Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded, Reason: "reviewed fixture debt", OwnerIssue: "#1064"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "if true", "if false", 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("same-class branch condition change must stale approval: %v", err)
	}
}

func TestCleanupRootGuardManualDependencyImportRetargetStales(t *testing.T) {
	source := `package fixture
import ("context";"testing"; c "example.com/cleanupfixture/one")
func terminalStop(ctx context.Context) error { return c.Check() }
var stop=terminalStop
func TestDebt(t *testing.T) { defer stop(nil) }
`
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": source,
		"one/check.go": `package one
func Check() error { return nil }
`,
		"two/check.go": `package two
func Check() error { return nil }
`,
	})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown {
		t.Fatalf("want initial unknown: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "owner_test.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	dep, err := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"owner_test.go": parsed}, "owner_test.go", "terminalStop")
	if err != nil {
		t.Fatal(err)
	}
	baseline := cleanupBaseline{Version: 1, Resolutions: []cleanupResolution{{Identity: site.Identity, SiteFingerprint: site.Fingerprint,
		Classification: cleanupNonLifecycle, Question: "is callback lifecycle", Reason: "terminalStop delegates to a selected helper", Reviewer: "fixture-reviewer", OwnerIssue: "#1064",
		Dependencies: []cleanupDependency{{Path: "owner_test.go", Declaration: "terminalStop", Fingerprint: dep}},
	}}}
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("initial manual resolution refused: %v", err)
	}
	changed := strings.Replace(source, `c "example.com/cleanupfixture/one"`, `c "example.com/cleanupfixture/two"`, 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale manual cleanup resolution dependency") {
		t.Fatalf("import alias retarget must stale evidence: %v", err)
	}
}

// spec: test-cleanup-policy / Identity stability
func TestCleanupRootGuardFormattingIdentityProperty(t *testing.T) {
	source := func(blanks int, comment bool) string {
		marker := ""
		if comment {
			marker = "// formatting-only marker\n"
		}
		return `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
` + strings.Repeat("\n", blanks) + marker + `func TestDebt(t *testing.T) { defer (&owner{}).Stop(context.Background()) }
`
	}
	baselineRoot := cleanupFixtureModule(t, map[string]string{"owner_test.go": source(0, false)})
	baselineReport, _ := analyzeCleanupRoots(t.Context(), baselineRoot, filepath.Join(baselineRoot, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if len(baselineReport.Sites) != 1 {
		t.Fatalf("baseline sites = %d", len(baselineReport.Sites))
	}
	baseline := baselineReport.Sites[0]
	// The grammar is the full finite product: 0..5 blank lines × comment absent/present.
	for blankLines := 0; blankLines <= 5; blankLines++ {
		for _, addComment := range []bool{false, true} {
			root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source(blankLines, addComment)})
			report, _ := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
			if len(report.Sites) != 1 {
				t.Fatalf("formatting %d/%t: transformed sites = %d", blankLines, addComment, len(report.Sites))
			}
			site := report.Sites[0]
			if site.Identity != baseline.Identity || site.Fingerprint != baseline.Fingerprint || site.Classification != baseline.Classification {
				t.Fatalf("formatting %d/%t changed semantic evidence: baseline=%+v transformed=%+v", blankLines, addComment, baseline, site)
			}
		}
	}
}

func TestCleanupRootGuardSameLineSemanticOrdinals(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { o:=&owner{}; defer o.Stop(context.Background()); defer o.Stop(context.TODO()) }
`,
	})
	for attempt := range 6 {
		report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
		if err == nil || len(report.Sites) != 2 {
			t.Fatalf("attempt %d: want two debt sites: %d, %v", attempt, len(report.Sites), err)
		}
		first, second := report.Sites[0], report.Sites[1]
		if first.StartOffset >= second.StartOffset || first.Identity == second.Identity ||
			!strings.HasSuffix(first.Identity, "|1") || !strings.HasSuffix(second.Identity, "|2") {
			t.Fatalf("attempt %d: ordinals did not follow lexical source order: %+v", attempt, report.Sites)
		}
	}
}

func TestCleanupRootGuardTypedContextlessCleanupExclusions(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"net/http";"net/http/httptest";"testing")
func TestCleanup(t *testing.T) {
    _,cancel:=context.WithCancel(context.Background())
    t.Cleanup(cancel)
    srv:=httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter,*http.Request){}))
    t.Cleanup(srv.Close)
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 2 {
		t.Fatalf("typed contextless cleanup registrations should be accounted without debt: %+v, %v", report.Sites, err)
	}
	for _, site := range report.Sites {
		if site.Classification != cleanupNonLifecycle || site.Applicability != "cleanup-owned" {
			t.Errorf("typed contextless registration: %+v", site)
		}
	}
}

func TestCleanupRootGuardConvertedCancelFuncCanConcealStop(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    cancel:=context.CancelFunc(func(){ _=(&owner{}).Stop(context.Background()) })
    t.Cleanup(cancel)
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("converted arbitrary callback must not be treated as a known context cancel function: %+v", report.Sites)
	}
	found := false
	for _, site := range report.Sites {
		if site.Origin == "cleanup" && site.Classification != cleanupNonLifecycle {
			found = true
		}
	}
	if !found {
		t.Fatalf("converted callback did not retain blocking cleanup evidence: %+v", report.Sites)
	}
}

func TestCleanupRootGuardLocalClosureAliasesCarryCleanupOwnership(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    o:=&owner{}
    later:=func(){ _=o.Stop(context.Background()) }
    defer later()
    t.Cleanup(later)
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("local closure aliases with unbounded lifecycle Stop must block: %+v", report.Sites)
	}
	origins := map[string]bool{}
	for _, site := range report.Sites {
		if site.Classification == cleanupUnbounded && site.Applicability == "cleanup-owned" {
			origins[site.Origin] = true
		}
	}
	if !origins["defer"] || !origins["cleanup"] {
		t.Fatalf("local closure aliases lost cleanup ownership: %+v", report.Sites)
	}
}

func TestCleanupRootGuardLocalClosureCallWithinCleanup(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    o:=&owner{}
    release:=func(){ _=o.Stop(context.Background()) }
    t.Cleanup(func(){ release() })
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("nested local closure Stop must block: %+v", report.Sites)
	}
	for _, site := range report.Sites {
		if site.Origin == "cleanup" && site.Classification == cleanupUnbounded && site.Applicability == "cleanup-owned" {
			return
		}
	}
	t.Fatalf("nested local closure Stop was not resolved: %+v", report.Sites)
}

func TestCleanupRootGuardDeferredClosureParameterCapture(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCapturedArg(t *testing.T) {
    o:=&owner{}
    stop:=func(c context.Context){ _=o.Stop(c) }
    defer stop(context.Background())
}
func TestLateVariable(t *testing.T) {
    o:=&owner{}
    finite,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    ctx:=finite
    stop:=func(c context.Context){ _=o.Stop(c); _=o.Stop(ctx) }
    defer stop(finite)
    ctx=context.Background()
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("deferred closure with unbounded context must block: %+v", report.Sites)
	}
	found := map[string]int{}
	for _, site := range report.Sites {
		if site.Origin == "defer" && site.Classification == cleanupUnbounded {
			found[site.Function]++
		}
	}
	if found["TestCapturedArg"] != 1 || found["TestLateVariable"] != 1 {
		t.Fatalf("defer arguments must capture at registration while lexical variables remain live: %+v", report.Sites)
	}
}

func TestCleanupRootGuardOrdinaryHelperOwnership(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func ordinaryHelper(o *owner) { _=o.Stop(context.Background()) }
func TestOrdinary(t *testing.T) { ordinaryHelper(&owner{}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil {
		t.Fatalf("positively ordinary helper must remain census-only: %+v, %v", report.Sites, err)
	}
	if len(report.Sites) != 1 || report.Sites[0].Function != "ordinaryHelper" || report.Sites[0].Applicability != "ordinary-only" {
		t.Fatalf("ordinary helper source was not attributed to its typed test caller: %+v", report.Sites)
	}
}

func TestCleanupRootGuardIndexedContextHelperReturn(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func finitePair() (int,context.Context) { c,_:=context.WithTimeout(context.Background(),time.Second); return 1,c }
func TestBounded(t *testing.T) { _,ctx:=finitePair(); defer (&owner{}).Stop(ctx) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupBounded {
		t.Fatalf("second context return should retain finite helper provenance: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardLexicalHelperIdentityDeduplicatesCallers(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func cleanupOwner(o *owner) { defer o.Stop(context.Background()) }
func TestFirst(t *testing.T) { cleanupOwner(&owner{}) }
func TestSecond(t *testing.T) { cleanupOwner(&owner{}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("identical invocations of one physical helper cleanup should make one debt record: %+v, %v", report.Sites, err)
	}
	if report.Sites[0].Function != "cleanupOwner" || report.Sites[0].Classification != cleanupUnbounded {
		t.Fatalf("identity must name the lexical enclosing helper: %+v", report.Sites[0])
	}
}

func TestCleanupRootGuardDirectReturnedTerminalClosure(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
func start() (func(context.Context) error,error) { return func(context.Context) error { return nil },nil }
func TestReturned(t *testing.T) {
    stop,err:=start(); if err!=nil { t.Fatal(err) }
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    t.Cleanup(func(){ _=stop(ctx) })
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupBounded || report.Sites[0].Origin != "cleanup" {
		t.Fatalf("direct returned terminal closure should resolve to bounded cleanup: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardDeferredConvertedCancelFuncFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    wrapped:=context.CancelFunc(func(){ _=(&owner{}).Stop(context.Background()) })
    defer wrapped()
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("converted arbitrary CancelFunc deferred callback must not pass: %+v", report.Sites)
	}
	for _, site := range report.Sites {
		if site.Origin == "defer" && site.Classification == cleanupUnknown {
			return
		}
	}
	t.Fatalf("deferred converted CancelFunc lost blocking ownership: %+v", report.Sites)
}

func TestCleanupRootGuardCallbackForwardingHelper(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func finish(f func(context.Context) error) { _=f(context.Background()) }
func TestCleanup(t *testing.T) { o:=&owner{}; defer finish(o.Stop) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("callback-forwarding helper must not disappear: %+v", report.Sites)
	}
	for _, site := range report.Sites {
		if site.Origin == "defer" && site.Classification == cleanupUnbounded && site.Function == "finish" {
			return
		}
	}
	t.Fatalf("callback-forwarding helper did not preserve unbounded cleanup: %+v", report.Sites)
}

func TestCleanupRootGuardOrdinaryClosureLateBinding(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestOrdinary(t *testing.T) {
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    o:=&owner{}
    invoke:=func(){ _=o.Stop(ctx) }
    ctx=context.Background()
    invoke()
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil || len(report.Sites) != 1 || report.Sites[0].Context != "unbounded" || report.Sites[0].Applicability != "ordinary-only" {
		t.Fatalf("ordinary local closure invocation must read later captured context: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardOrdinaryClosureWritesCallerContext(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
    defer cancel()
    mutate:=func(){ ctx=context.Background() }
    mutate()
    defer (&owner{}).Stop(ctx)
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnbounded {
		t.Fatalf("ordinary closure write must update caller context before defer capture: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardCallableBranchJoinFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestCleanup(t *testing.T) {
    o:=&owner{}
    stop:=o.Stop
    var alternate func(context.Context) error
    if t.Name()=="other" { stop=alternate }
    defer stop(context.Background())
}
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown {
		t.Fatalf("ambiguous callable branch must fail closed: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardNamedCallbackConversionFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
type stopFn func(context.Context) error
func TestCleanup(t *testing.T) { o:=&owner{}; stop:=stopFn(o.Stop); defer stop(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown {
		t.Fatalf("named callback conversion must remain a guarded edge: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardNestedCallableReturnFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func choose(flag bool,o *owner,alternate func(context.Context) error) func(context.Context) error {
    if flag { return alternate }
    return o.Stop
}
func TestCleanup(t *testing.T) { o:=&owner{}; var alternate func(context.Context) error; stop:=choose(t.Name()=="other",o,alternate); defer stop(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown {
		t.Fatalf("nested return must not be ignored when resolving terminal callback: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardTwoHopCallbackForwarding(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func finish(f func(context.Context) error) { _=f(context.Background()) }
func finish2(f func(context.Context) error) { finish(f) }
func TestCleanup(t *testing.T) { o:=&owner{}; defer finish2(o.Stop) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("two-hop callback forwarding must not disappear: %+v", report.Sites)
	}
	for _, site := range report.Sites {
		if site.Origin == "defer" && site.Classification == cleanupUnbounded && site.Function == "finish" {
			return
		}
	}
	t.Fatalf("two-hop helper lost terminal callback debt: %+v", report.Sites)
}

func TestCleanupRootGuardUntypedTerminalCallbackCandidate(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"nested/go.mod": "module example.com/nested\n\ngo 1.26.3\n",
		"nested/owner_test.go": `package nested
import ("context";"testing")
var finish func(context.Context) error
func TestNested(t *testing.T) { defer finish(context.Background()) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 || report.Sites[0].Classification != cleanupUnknown || report.Sites[0].Path != "nested/owner_test.go" {
		t.Fatalf("untyped deferred callback without Stop spelling must remain visible and blocking: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardTestingTBRegistration(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func register(tb testing.TB,o *owner) { tb.Cleanup(func(){ _=o.Stop(context.Background()) }) }
func TestCleanup(t *testing.T) { register(t,&owner{}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("typed testing.TB Cleanup callback must block: %+v", report.Sites)
	}
	for _, site := range report.Sites {
		if site.Classification == cleanupUnbounded && site.Applicability == "cleanup-owned" {
			return
		}
	}
	t.Fatalf("testing.TB registration lost cleanup ownership: %+v", report.Sites)
}

func TestCleanupRootGuardManualDependencyDotImportRetarget(t *testing.T) {
	source := `package fixture
import ("context";"testing";. "example.com/cleanupfixture/one")
func evidence(){ Check() }
var stop func(context.Context) error
func TestDebt(t *testing.T){ defer stop(nil) }
`
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": source,
		"one/check.go":  "package one\nfunc Check() {}\n",
		"two/check.go":  "package two\nfunc Check() {}\n",
	})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("want one guarded unknown: %+v, %v", report.Sites, err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "owner_test.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	dep, err := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"owner_test.go": parsed}, "owner_test.go", "evidence")
	if err != nil {
		t.Fatal(err)
	}
	site := report.Sites[0]
	baseline := cleanupBaseline{Version: 1, Resolutions: []cleanupResolution{{Identity: site.Identity, SiteFingerprint: site.Fingerprint,
		Classification: cleanupNonLifecycle, Question: "which Check binding", Reason: "fixture review", Reviewer: "fixture-reviewer", OwnerIssue: "#1064",
		Dependencies: []cleanupDependency{{Path: "owner_test.go", Declaration: "evidence", Fingerprint: dep}},
	}}}
	raw, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err != nil {
		t.Fatalf("initial resolution: %v", err)
	}
	changed := strings.Replace(source, `"example.com/cleanupfixture/one"`, `"example.com/cleanupfixture/two"`, 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale manual cleanup resolution dependency") {
		t.Fatalf("dot import retarget must stale exact manual evidence: %v", err)
	}
}

func TestCleanupRootGuardManualDependencyCommentsAndBuildTags(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"helper_test.go": "//go:build integration\n\npackage fixture\n// original doc\nfunc evidence() {}\n",
	})
	path := filepath.Join(root, "helper_test.go")
	read := func() string {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		fp, err := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"helper_test.go": parsed}, "helper_test.go", "evidence")
		if err != nil {
			t.Fatal(err)
		}
		return fp
	}
	original := read()
	if err := os.WriteFile(path, []byte("//go:build integration\n\npackage fixture\n// changed prose only\nfunc evidence() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if current := read(); current != original {
		t.Fatalf("ordinary doc comment changed manual evidence: %s != %s", current, original)
	}
	if err := os.WriteFile(path, []byte("//go:build live_llm\n\npackage fixture\n// changed prose only\nfunc evidence() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if current := read(); current == original {
		t.Fatal("build constraint change failed to stale manual evidence")
	}
}

func TestCleanupRootGuardReceiverBindingChangeStales(t *testing.T) {
	source := `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func firstOwner() *owner { return &owner{} }
func secondOwner() *owner { return &owner{} }
func TestDebt(t *testing.T) { o:=firstOwner(); defer o.Stop(context.Background()) }
`
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("initial debt: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	raw, err := json.Marshal(cleanupBaseline{Version: 1, Entries: []cleanupApproval{{Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded, Reason: "fixture review", OwnerIssue: "#1064"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "o:=firstOwner()", "o:=secondOwner()", 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("same-type receiver binding change must stale debt approval: %v", err)
	}
}

func TestCleanupRootGuardOwningConditionChangeStales(t *testing.T) {
	source := `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestDebt(t *testing.T) { o:=&owner{}; if true { defer o.Stop(context.Background()) } }
`
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("initial debt: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	raw, err := json.Marshal(cleanupBaseline{Version: 1, Entries: []cleanupApproval{{Identity: site.Identity, Fingerprint: site.Fingerprint, Classification: cleanupUnbounded, Reason: "fixture review", OwnerIssue: "#1064"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "if true", "if false", 1)
	if err := os.WriteFile(filepath.Join(root, "owner_test.go"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("owning branch condition change must stale debt approval: %v", err)
	}
}

func TestCleanupRootGuardQualifiedEnclosingMethods(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
type first struct{}
type second struct{}
func (*first) wrap() { defer (&owner{}).Stop(context.Background()) }
func (*second) wrap() { defer (&owner{}).Stop(context.Background()) }
func TestDebt(t *testing.T) { (&first{}).wrap(); (&second{}).wrap() }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 2 {
		t.Fatalf("method debt sites: %+v, %v", report.Sites, err)
	}
	if report.Sites[0].Function == report.Sites[1].Function || report.Sites[0].Identity == report.Sites[1].Identity {
		t.Fatalf("same-named methods on different receivers need distinct enclosing identity: %+v", report.Sites)
	}
}

func TestCleanupRootGuardOrdinaryProductionLifecycleBoundary(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"manager.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func stopAll(o *owner) { defer o.Stop(context.Background()) }
`,
		"manager_test.go": `package fixture
import "testing"
func TestOrdinaryCall(t *testing.T) { _ = t; stopAll(&owner{}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil {
		t.Fatalf("ordinary production lifecycle call was admitted as cleanup: %+v, %v", report.Sites, err)
	}
	for _, site := range report.Sites {
		if site.Applicability == "cleanup-owned" || site.Applicability == "unresolved" {
			t.Fatalf("ordinary production implementation became cleanup root: %+v", site)
		}
	}
}

func TestCleanupRootGuardExactOrdinaryOwnershipDisposition(t *testing.T) {
	source := `package fixture
import ("context"; "testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
func TestTable(t *testing.T) { _ = t; _ = helper }
`
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": source})
	baselinePath := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("unreached helper must initially block: %+v", report.Sites)
	}
	var site cleanupSite
	for _, candidate := range report.Sites {
		if candidate.Function == "helper" && candidate.Classification == cleanupUnknown && candidate.Applicability == "unresolved" {
			site = candidate
		}
	}
	if site.Identity == "" {
		t.Fatalf("missing unresolved helper site: %+v", report.Sites)
	}
	parsed, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "owner_test.go"), nil, parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	dep, depErr := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"owner_test.go": parsed}, "owner_test.go", "TestTable")
	if depErr != nil {
		t.Fatal(depErr)
	}
	baseline := cleanupBaseline{Version: 1, Resolutions: []cleanupResolution{{Identity: site.Identity, SiteFingerprint: site.Fingerprint,
		Classification: cleanupUnknown, Applicability: "ordinary-only", Question: "is helper reached only by the ordinary test table",
		Reason: "fixture source review", Reviewer: "fixture-reviewer", OwnerIssue: "#1064",
		Dependencies: []cleanupDependency{{Path: "owner_test.go", Declaration: "TestTable", Fingerprint: dep}},
	}}}
	raw, marshalErr := json.Marshal(baseline)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile(baselinePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}})
	if err != nil {
		t.Fatalf("exact ordinary ownership disposition refused: %v", err)
	}
	found := false
	for _, candidate := range report.Sites {
		if candidate.Identity == site.Identity {
			found = candidate.Classification == cleanupUnknown && candidate.Applicability == "ordinary-only"
		}
	}
	if !found {
		t.Fatalf("manual disposition changed classification or lost site: %+v", report.Sites)
	}
	// The reviewed TestTable declaration and its dependency fingerprint stay
	// unchanged. A new file must introduce its own blocking ownership variant.
	callerPath := filepath.Join(root, "newcaller_test.go")
	if err := os.WriteFile(callerPath, []byte(`package fixture
import "testing"
func TestNewCleanupCaller(t *testing.T) { t.Cleanup(func(){helper(&owner{})}) }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "new unbounded-terminal-cleanup") {
		t.Fatalf("new cleanup caller reached same physical helper without debt refusal: %v", err)
	}
	if err := os.WriteFile(callerPath, []byte(`package fixture
import "testing"
var invoke func(*owner) = helper
func TestNewUnknownCaller(t *testing.T) { t.Cleanup(func(){invoke(&owner{})}) }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, baselinePath, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "new uncertain-owner-provenance") {
		t.Fatalf("new unresolved caller bypassed exact ordinary disposition: %v", err)
	}
}

func TestCleanupRootGuardQualifiedManualDependency(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import "context"
type first struct{ value int }
type second struct{}
func (*first) Stop(context.Context) error { return nil }
func (*second) Stop(context.Context) error { return nil }
`})
	path := filepath.Join(root, "owner_test.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{"owner_test.go": parsed}
	if _, err := cleanupSourceDependencyFingerprint(root, files, "owner_test.go", "Stop"); err == nil {
		t.Fatal("unqualified shared method was accepted")
	}
	first, err := cleanupSourceDependencyFingerprint(root, files, "owner_test.go", "first.Stop")
	if err != nil {
		t.Fatal(err)
	}
	second, err := cleanupSourceDependencyFingerprint(root, files, "owner_test.go", "second.Stop")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different receiver methods share evidence")
	}
	beforeType, err := cleanupSourceDependencyFingerprint(root, files, "owner_test.go", "first")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(`package fixture
import "context"
type first struct{ value int }
type second struct{}
func (*first) Stop(context.Context) error { return nil }
func (*second) Stop(context.Context) error { return nil }
`, "value int", "value string", 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err = parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	afterType, err := cleanupSourceDependencyFingerprint(root, map[string]*ast.File{"owner_test.go": parsed}, "owner_test.go", "first")
	if err != nil {
		t.Fatal(err)
	}
	if beforeType == afterType {
		t.Fatal("receiver type change retained manual evidence")
	}
}

func TestCleanupRootGuardTestingContextHasNoDeadline(t *testing.T) {
	if deadline, ok := t.Context().Deadline(); ok {
		t.Fatalf("top-level testing.T.Context gained deadline %v; re-review cleanup provenance", deadline)
	}
	t.Run("subtest", func(t *testing.T) {
		if deadline, ok := t.Context().Deadline(); ok {
			t.Fatalf("subtest testing.T.Context gained deadline %v; re-review cleanup provenance", deadline)
		}
	})
}

func TestCleanupRootGuardTypedTestingContextProvenance(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context";"testing";"time")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func TestUnbounded(t *testing.T) { defer (&owner{}).Stop(t.Context()) }
func TestFiniteChild(t *testing.T) { ctx,cancel:=context.WithTimeout(t.Context(),time.Second); defer cancel(); defer (&owner{}).Stop(ctx) }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("deadline-free testing context must refuse cleanup: %+v, %v", report.Sites, err)
	}
	classes := map[string]string{}
	for _, site := range report.Sites {
		classes[site.Function] = site.Classification
	}
	if classes["TestUnbounded"] != cleanupUnbounded || classes["TestFiniteChild"] != cleanupBounded {
		t.Fatalf("typed testing context provenance = %#v", classes)
	}
}

func TestCleanupRootGuardTypedManagerStopAllBoundary(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"service/manager.go": `package service
import "context"
type Manager struct{}
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func (*Manager) StopAll(ctx context.Context) error { return (&owner{}).Stop(ctx) }
type Other struct{}
func (*Other) StopAll(context.Context) error { return nil }
`,
		"service/manager_test.go": `package service
import ("context";"testing")
func TestBoundary(t *testing.T) { m:=&Manager{}; defer m.StopAll(context.Background()) }
func TestMethodValue(t *testing.T) { m:=&Manager{}; stop:=m.StopAll; defer stop(context.Background()) }
func TestMethodExpression(t *testing.T) { m:=&Manager{}; defer (*Manager).StopAll(m,context.Background()) }
func TestOther(t *testing.T) { o:=&Other{}; defer o.StopAll(context.Background()) }
`,
	})
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/c360studio/semstreams\n\ngo 1.26.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("typed StopAll cleanup must refuse: %+v, %v", report.Sites, err)
	}
	var direct, value, expression, internal, other int
	for _, site := range report.Sites {
		if site.Path == "service/manager.go" {
			internal++
		}
		if site.Function == "TestBoundary" && strings.HasSuffix(site.Target, ".StopAll") && site.Classification == cleanupUnbounded {
			direct++
		}
		if site.Function == "TestMethodValue" && strings.HasSuffix(site.Target, ".StopAll") && site.Classification == cleanupUnbounded {
			value++
		}
		if site.Function == "TestMethodExpression" && strings.HasSuffix(site.Target, ".StopAll") && site.Classification == cleanupUnbounded {
			expression++
		}
		if site.Function == "TestOther" {
			other++
		}
	}
	if direct != 1 || value != 1 || expression != 1 || internal != 0 || other != 0 {
		t.Fatalf("exact Manager.StopAll boundary counts direct=%d value=%d expression=%d internal=%d other=%d; sites=%+v", direct, value, expression, internal, other, report.Sites)
	}
}

func TestCleanupRootGuardReturnedLocalClosureExposesStop(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context"; "testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func setup() func() { o:=&owner{}; cleanup:=func(){_ = o.Stop(context.Background())}; return cleanup }
func TestFirst(t *testing.T) { _=t; cleanup:=setup(); defer cleanup() }
func TestSecond(t *testing.T) { _=t; cleanup:=setup(); defer cleanup() }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("returned closure hid lifecycle debt: %+v, %v", report.Sites, err)
	}
	var debt []cleanupSite
	for _, site := range report.Sites {
		if site.Classification == cleanupUnbounded {
			debt = append(debt, site)
		}
	}
	if len(debt) != 1 || debt[0].Function != "setup" || debt[0].Origin != "defer" {
		t.Fatalf("returned closure physical identity/ownership = %+v", debt)
	}
}

func TestCleanupRootGuardReturnedNamedNonLifecycleClosure(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import "testing"
func setup() (cleanup func()) { cleanup=func(){ _ = 1 }; return }
func TestOne(t *testing.T) { _=t; cleanup:=setup(); defer cleanup() }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil {
		t.Fatalf("named returned non-lifecycle closure was unknown: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardReplacedModuleManualDependencyRefused(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner_test.go": `package fixture
import ("context";"testing";_ "example.com/dep")
var stop func(context.Context) error
func TestDebt(t *testing.T) { _=t; defer stop(context.Background()) }
`,
		"dep/go.mod": "module example.com/dep\n\ngo 1.26.3\n",
		"dep/dep.go": "package dep\nconst Symbol = 1\n",
	})
	mod := "module example.com/cleanupfixture\n\ngo 1.26.3\n\nrequire example.com/dep v0.0.0\nreplace example.com/dep => ./dep\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "cleanup_baseline.json")
	report, err := analyzeCleanupRoots(t.Context(), root, path, []cleanupSelection{{Name: "default"}})
	if err == nil || len(report.Sites) != 1 {
		t.Fatalf("want unknown callback: %+v, %v", report.Sites, err)
	}
	site := report.Sites[0]
	baseline := cleanupBaseline{Version: 1, Resolutions: []cleanupResolution{{Identity: site.Identity, SiteFingerprint: site.Fingerprint,
		Classification: cleanupNonLifecycle, Question: "which replacement supplies Symbol", Reason: "fixture review", Reviewer: "fixture-reviewer", OwnerIssue: "#1064",
		Dependencies: []cleanupDependency{{Symbol: "Symbol", Module: "example.com/dep", Version: "v0.0.0", Fingerprint: cleanupHash("example.com/dep@v0.0.0|Symbol")}},
	}}}
	raw, marshalErr := json.Marshal(baseline)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeCleanupRoots(t.Context(), root, path, []cleanupSelection{{Name: "default"}}); err == nil || !strings.Contains(err.Error(), "stale manual cleanup resolution dependency") {
		t.Fatalf("replace-backed module evidence was accepted without target identity: %v", err)
	}
}

func TestCleanupRootGuardReturnedForeignMethodValue(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("net/http";"net/http/httptest";"testing")
func newServer() func() { server:=httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter,*http.Request){})); return server.Close }
func TestClose(t *testing.T) { _=t; closeServer:=newServer(); defer closeServer() }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err != nil {
		t.Fatalf("returned typed foreign method value unresolved: %+v, %v", report.Sites, err)
	}
	var count int
	for _, site := range report.Sites {
		if site.Function == "TestClose" && site.Classification == cleanupNonLifecycle && site.Origin == "defer" && site.Target == "net/http/httptest.Close" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("returned method value evidence missing: %+v", report.Sites)
	}
}

func TestCleanupRootGuardSameTypedHelperReceiverCollapsesCallers(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context";"testing")
type owner struct{ config int }
func (*owner) Stop(context.Context) error { return nil }
func makeOwner(config int) *owner { return &owner{config:config} }
func register(t *testing.T, o *owner) { t.Cleanup(func(){ _=o.Stop(context.Background()) }) }
func TestOne(t *testing.T) { register(t,makeOwner(1)) }
func TestTwo(t *testing.T) { register(t,makeOwner(2)) }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("missing unbounded helper refusal: %+v", report.Sites)
	}
	var debt []cleanupSite
	for _, site := range report.Sites {
		if site.Function == "register" && site.Classification == cleanupUnbounded {
			debt = append(debt, site)
		}
	}
	if len(debt) != 1 {
		t.Fatalf("same typed helper receiver yielded %d debt variants: %+v", len(debt), debt)
	}
}

func TestCleanupRootGuardUnresolvedParameterizedCleanupCallbackFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
`,
		"owner_test.go": `package fixture
import "testing"
var invoke func(*owner) = helper
func TestHidden(t *testing.T) { t.Cleanup(func(){invoke(&owner{})}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "uncertain-owner-provenance") {
		t.Fatalf("unresolved parameterized Cleanup callback hid Stop: %+v, %v", report.Sites, err)
	}
	found := false
	for _, site := range report.Sites {
		if site.Path == "owner_test.go" && site.Origin == "cleanup" && site.Classification == cleanupUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing callback edge at registration: %+v", report.Sites)
	}
}

func TestCleanupRootGuardReturnedContextlessReassignmentFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context";"net/http/httptest";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func closeHelper() func(){ return (&httptest.Server{}).Close }
func TestHidden(t *testing.T){ o:=&owner{}; f:=closeHelper(); f=func(){_ = o.Stop(context.Background())}; t.Cleanup(f) }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("reassigned returned close concealed Stop: %+v, %v", report.Sites, err)
	}
	found := false
	for _, site := range report.Sites {
		if site.Classification == cleanupUnbounded && site.Applicability == "cleanup-owned" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing cleanup-owned Stop after reassignment: %+v", report.Sites)
	}
}

func TestCleanupRootGuardUnresolvedSelectorCallbackFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
`,
		"owner_test.go": `package fixture
import "testing"
var holder = struct{invoke func(*owner)}{helper}
func TestHidden(t *testing.T) { t.Cleanup(func(){holder.invoke(&owner{})}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "uncertain-owner-provenance") {
		t.Fatalf("function-valued selector concealed Stop: %+v, %v", report.Sites, err)
	}
	found := false
	for _, site := range report.Sites {
		if site.Path == "owner_test.go" && site.Origin == "cleanup" && site.Classification == cleanupUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing unresolved callback selector at Cleanup: %+v", report.Sites)
	}
}

func TestCleanupRootGuardUnresolvedCallbackThroughResolvedWrapperFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
`,
		"owner_test.go": `package fixture
import "testing"
var holder = struct{invoke func(*owner)}{helper}
var invoke func(*owner) = helper
func wrapSelector(o *owner) { holder.invoke(o) }
func wrapIdent(o *owner) { invoke(o) }
func TestSelector(t *testing.T) { t.Cleanup(func(){wrapSelector(&owner{})}) }
func TestIdent(t *testing.T) { t.Cleanup(func(){wrapIdent(&owner{})}) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "uncertain-owner-provenance") {
		t.Fatalf("resolved wrapper pruned unresolved callback: %+v, %v", report.Sites, err)
	}
	found := map[string]bool{}
	for _, site := range report.Sites {
		if site.Origin == "cleanup" && site.Classification == cleanupUnknown {
			found[site.Function] = true
		}
	}
	if !found["wrapSelector"] || !found["wrapIdent"] {
		t.Fatalf("missing wrapper callback edges: %+v", report.Sites)
	}
}

func TestCleanupRootGuardParallelCallbackAssignmentFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{"owner_test.go": `package fixture
import ("context";"net/http/httptest";"testing")
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func closeHelper() func(){ return (&httptest.Server{}).Close }
func TestSwap(t *testing.T){ o:=&owner{}; safe:=closeHelper(); unsafe:=func(){_ = o.Stop(context.Background())}; unsafe,safe=safe,unsafe; t.Cleanup(safe) }
`})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil {
		t.Fatalf("parallel callback swap concealed lifecycle Stop: %+v", report.Sites)
	}
	var guarded bool
	for _, site := range report.Sites {
		if site.Applicability != "ordinary-only" && (site.Classification == cleanupUnbounded || site.Classification == cleanupUnknown) {
			guarded = true
		}
	}
	if !guarded {
		t.Fatalf("parallel callback swap produced no guarded site: %+v, %v", report.Sites, err)
	}
}

func TestCleanupRootGuardNamedWrapperCallbackFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
`,
		"owner_test.go": `package fixture
import "testing"
var holder = struct{invoke func(*owner)}{helper}
func wrap() { holder.invoke(&owner{}) }
func TestNamed(t *testing.T) { t.Cleanup(wrap) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "uncertain-owner-provenance") {
		t.Fatalf("named Cleanup wrapper pruned unresolved callback: %+v, %v", report.Sites, err)
	}
	for _, site := range report.Sites {
		if site.Function == "wrap" && site.Origin == "cleanup" && site.Classification == cleanupUnknown {
			return
		}
	}
	t.Fatalf("missing named wrapper cleanup-owned callback edge: %+v", report.Sites)
}

func TestCleanupRootGuardParenthesizedCallbackFailsClosed(t *testing.T) {
	root := cleanupFixtureModule(t, map[string]string{
		"owner.go": `package fixture
import "context"
type owner struct{}
func (*owner) Stop(context.Context) error { return nil }
func helper(o *owner) { _ = o.Stop(context.Background()) }
`,
		"owner_test.go": `package fixture
import "testing"
var holder = struct{invoke func(*owner)}{helper}
func TestParenthesized(t *testing.T) { t.Cleanup(func(){ (holder.invoke)(&owner{}) }) }
`,
	})
	report, err := analyzeCleanupRoots(t.Context(), root, filepath.Join(root, "cleanup_baseline.json"), []cleanupSelection{{Name: "default"}})
	if err == nil || !strings.Contains(err.Error(), "uncertain-owner-provenance") {
		t.Fatalf("parenthesized callback concealed lifecycle Stop: %+v, %v", report.Sites, err)
	}
	for _, site := range report.Sites {
		if site.Origin == "cleanup" && site.Classification == cleanupUnknown {
			return
		}
	}
	t.Fatalf("missing parenthesized callback cleanup-owned edge: %+v", report.Sites)
}

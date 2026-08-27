package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAdvertisedScenariosAreDispatchable proves ONE half of what an advertised
// scenario needs: every name printed in the --list menu or in the -scenario
// flag help has a case in createScenario's dispatch switch. Both surfaces are
// enumerated from their OWNING declarations — the live createScenario switch
// (via go/ast on main.go) and the live handleListCommand/-scenario flag help
// output (by calling the production functions) — never a hand-copied name
// list, so this guard cannot silently drift from what the CLI offers.
//
// It is NOT gh#1129 regression evidence, and the earlier version of this
// comment claiming it was is corrected here. On the tree that filed gh#1129
// (a28fceb3) core-federation was advertised at main.go:131 and :235 AND
// dispatched at :383 — so this assertion is GREEN on the exact unfixed
// defect, measured 2026-08-27 by running this test against that tree. The
// gh#1129 defect is an advertised, dispatchable scenario that nothing can
// stand up; TestAdvertisedScenariosHaveARunner below is its guard.
//
// Both extractors are scoped by structure (section boundaries, token
// separators), never by a fixed indentation depth or a comma-only split:
// review found this guard could be walked around by adding a menu entry at a
// different indent, or a flag-help name joined to its neighbor by " or "
// instead of ", or " — both previously matched nothing and were silently
// dropped instead of failing. See menuEntryPattern, tokenSplitPattern, the
// flag-help extractor's reject-unknown-token assertion, and the floor on the
// menu count, which exists so a parser regression that silently stops
// matching entries fails loudly instead of vacuously passing on one survivor.
//
// This only checks the advertised => dispatchable direction. It does not
// require the reverse: some dispatchable names (e.g. "ops", "crud-tools", and
// the bare "structural"/"statistical"/"semantic" tiered-variant aliases) are
// intentionally not advertised as standalone menu entries.
func TestAdvertisedScenariosAreDispatchable(t *testing.T) {
	dispatchable := dispatchableScenarioNames(t)
	require.NotEmpty(t, dispatchable, "createScenario's dispatch switch must be found and non-empty")

	menuNames := advertisedMenuScenarioNames(t)
	require.GreaterOrEqualf(t, len(menuNames), 10,
		"Individual Scenarios menu advertised only %d entries — a collapse below this floor means the line "+
			"parser silently stopped matching, not that scenarios were actually removed", len(menuNames))
	for _, name := range menuNames {
		require.Truef(t, dispatchable[name],
			"menu (--list) advertises scenario %q with no case in createScenario's dispatch switch", name)
	}
	for _, name := range advertisedFlagHelpScenarioNames(t) {
		require.Truef(t, dispatchable[name],
			"-scenario flag help advertises scenario %q with no case in createScenario's dispatch switch", name)
	}
}

// TestAdvertisedScenariosHaveARunner is the gh#1129 guard. core-federation had
// a menu entry AND a dispatch case — the check above passes on it — and still
// could not be stood up by anyone who selected it: no `task e2e:federation`,
// no compose file defining the `edge` service its config dialed, no distinct
// ports.
//
// A unit test cannot prove a scenario's Docker topology actually comes up;
// only running the tier can. What it CAN prove — and what gh#1129's first and
// cheapest leg was — is that some runner NAMES the scenario. Every advertised
// name must therefore be reachable one of exactly two ways, both derived from
// owning declarations rather than a hand-copied list:
//
//  1. a `./e2e --scenario <name>` invocation in Taskfile.yml or
//     taskfiles/**/*.yml names it directly, or
//  2. its dispatch case returns a constructor that runAllScenarios also
//     builds, so the `--scenario all` that `task e2e:core` runs covers it —
//     core-health and core-dataflow are reachable only this way.
//
// Stated so nobody reads this guard as more than it is: it does NOT prove the
// named task target has a working compose topology, distinct ports, or
// resolvable hostnames. Those were gh#1129's other three legs, and they fail
// only when the tier runs, which is the only place they can be observed.
func TestAdvertisedScenariosHaveARunner(t *testing.T) {
	namedByTask := scenarioNamesInvokedByTaskRunners(t)
	inAllBundle := scenarioNamesCoveredByAllBundle(t)

	advertised := append(advertisedMenuScenarioNames(t), advertisedFlagHelpScenarioNames(t)...)
	for _, name := range advertised {
		require.Truef(t, namedByTask[name] || inAllBundle[name],
			"scenario %q is advertised but nothing runs it: no `./e2e --scenario %s` in Taskfile.yml or "+
				"taskfiles/**/*.yml (those name %v), and createScenario's case for it does not return a "+
				"constructor that runAllScenarios builds for `--scenario all` (that covers %v). This is the "+
				"gh#1129 shape: an advertised scenario that fails for anyone who selects it.",
			name, name, slices.Sorted(maps.Keys(namedByTask)), slices.Sorted(maps.Keys(inAllBundle)))
	}
}

// TestOnlyExecutedTaskLinesCountAsRunners guards the executed-versus-documented
// distinction TestAdvertisedScenariosHaveARunner rests on. Without it that guard
// accepts any Taskfile line CONTAINING "./e2e", so a scenario nothing can stand
// up is laundered into "has a runner" by a help string or a commented-out
// example — the gh#1129 shape surviving the gh#1129 guard.
//
// Each line here is a shape this repository actually writes; the file:line
// citations are where. Names are deliberately ones no menu entry advertises, so
// a row proves the extraction rather than the repository's current contents.
func TestOnlyExecutedTaskLinesCountAsRunners(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		want  []string
		shape string
	}{
		{
			name:  "plain cmds invocation",
			line:  `      - cd cmd/e2e && ./e2e --scenario alpha-runs`,
			want:  []string{"alpha-runs"},
			shape: "taskfiles/e2e/core.yml:80",
		},
		{
			name:  "subshell capturing the exit code",
			line:  `          (cd cmd/e2e && ./e2e --scenario beta-runs) || rc=$?`,
			want:  []string{"beta-runs"},
			shape: "taskfiles/e2e/lifecycle.yml:26",
		},
		{
			name:  "env-prefixed invocation",
			line:  `      - cd cmd/e2e && SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT=10m ./e2e --scenario gamma-runs`,
			want:  []string{"gamma-runs"},
			shape: "taskfiles/e2e/semantic.yml:39",
		},
		{
			name:  "folded scalar continuation line",
			line:  `        cd cmd/e2e && ./e2e --scenario delta-runs --base-url http://localhost:38090`,
			want:  []string{"delta-runs"},
			shape: "taskfiles/e2e/slow-consumer.yml:13",
		},
		{
			name:  "echoed manual-run instructions are printed, not run",
			line:  `      - 'echo "  cd cmd/e2e && ./e2e --scenario epsilon-echoed --message-count 10000"'`,
			want:  nil,
			shape: "taskfiles/e2e/throughput.yml:156 — THE DEFEAT",
		},
		{
			name:  "printf'd instructions are printed, not run",
			line:  `      - printf '  cd cmd/e2e && ./e2e --scenario zeta-printed\n'`,
			want:  nil,
			shape: "the other shell spelling of the same laundering",
		},
		{
			name:  "commented-out invocation inside a cmds block",
			line:  `      # cd cmd/e2e && ./e2e --scenario eta-commented`,
			want:  nil,
			shape: "taskfiles/e2e/lifecycle.yml:16-23 — THE DEFEAT",
		},
		{
			name:  "top-of-file comment naming a scenario",
			line:  `# run with: cd cmd/e2e && ./e2e --scenario theta-commented`,
			want:  nil,
			shape: "taskfiles/e2e/research-graph.yml:5",
		},
		{
			name:  "a real invocation followed by a progress echo still counts",
			line:  `      - cd cmd/e2e && ./e2e --scenario iota-runs && echo "[DONE] ./e2e --scenario kappa-echoed"`,
			want:  []string{"iota-runs"},
			shape: "rejects the over-broad fix: dropping any line containing echo",
		},
		{
			name:  "template variable is not a literal scenario name",
			line:  `      - cd cmd/e2e && ./e2e --scenario tiered --variant {{.VARIANT}}`,
			want:  []string{"tiered"},
			shape: "Taskfile.yml:182",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Sorted(maps.Keys(scenarioNamesInTaskfileContent(tc.line)))
			require.Equalf(t, tc.want, got,
				"line shape from %s must contribute exactly %v to the runner set. A line only counts "+
					"when it is a command the task runner executes; a mention in a printed string or a "+
					"YAML comment is documentation, and counting it lets an advertised scenario nothing "+
					"can stand up pass TestAdvertisedScenariosHaveARunner.",
				tc.shape, tc.want)
		})
	}
}

// parseMainGo parses cmd/e2e/main.go, the owning declaration for the dispatch
// switch, the -scenario flag registration, and the `--scenario all` bundle.
func parseMainGo(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	require.NoError(t, err)
	return f
}

// findFuncDecl returns main.go's top-level declaration of the named function.
// A miss is fatal rather than an empty result: every set derived from an AST
// walk here must fail loudly when the shape it reads moves, since an empty
// set would make every assertion above pass vacuously.
func findFuncDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	require.FailNowf(t, "function not found",
		"main.go must declare func %s — this guard reads it as an owning declaration", name)
	return nil
}

// dispatchCaseClauses returns every case clause on createScenario's switch.
func dispatchCaseClauses(t *testing.T, file *ast.File) []*ast.CaseClause {
	t.Helper()
	fn := findFuncDecl(t, file, "createScenario")
	var clauses []*ast.CaseClause
	for _, stmt := range fn.Body.List {
		sw, ok := stmt.(*ast.SwitchStmt)
		if !ok {
			continue
		}
		for _, clauseStmt := range sw.Body.List {
			if clause, ok := clauseStmt.(*ast.CaseClause); ok {
				clauses = append(clauses, clause)
			}
		}
	}
	require.NotEmpty(t, clauses, "createScenario's switch statement must be found by AST walk")
	return clauses
}

// caseClauseNames returns the string literals a case clause matches on. The
// default clause has none and yields nothing.
func caseClauseNames(t *testing.T, clause *ast.CaseClause) []string {
	t.Helper()
	var names []string
	for _, expr := range clause.List {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		names = append(names, value)
	}
	return names
}

// dispatchableScenarioNames returns every case-clause string literal on
// createScenario's switch — the exact set of names --scenario can stand up.
// Enumerated by walking the real source at test time so it cannot go stale
// relative to the dispatcher it guards.
func dispatchableScenarioNames(t *testing.T) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	for _, clause := range dispatchCaseClauses(t, parseMainGo(t)) {
		for _, name := range caseClauseNames(t, clause) {
			names[name] = true
		}
	}
	return names
}

// calleeName renders a call target or type expression as its source spelling
// ("scenarios.NewCoreHealthScenario", "newThroughputScenario") for the
// identity comparisons the all-bundle check makes.
func calleeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			return pkg.Name + "." + e.Sel.Name
		}
		return e.Sel.Name
	default:
		return ""
	}
}

// allBundleConstructors returns the constructor names inside runAllScenarios'
// []scenarios.Scenario literal — exactly what `--scenario all` stands up, and
// therefore what `task e2e:core` covers without naming a scenario itself.
func allBundleConstructors(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()
	constructors := make(map[string]bool)
	ast.Inspect(findFuncDecl(t, file, "runAllScenarios"), func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		arr, ok := lit.Type.(*ast.ArrayType)
		if !ok || calleeName(arr.Elt) != "scenarios.Scenario" {
			return true
		}
		for _, elt := range lit.Elts {
			if call, ok := elt.(*ast.CallExpr); ok {
				constructors[calleeName(call.Fun)] = true
			}
		}
		return true
	})
	require.NotEmpty(t, constructors,
		"runAllScenarios must build a non-empty []scenarios.Scenario literal — this guard reads it as the "+
			"owning declaration of what `--scenario all` covers")
	return constructors
}

// scenarioNamesCoveredByAllBundle returns the createScenario case names whose
// returned constructor also appears in runAllScenarios' bundle. Both halves
// come from main.go's AST, so neither can drift from the source.
func scenarioNamesCoveredByAllBundle(t *testing.T) map[string]bool {
	t.Helper()
	file := parseMainGo(t)
	bundle := allBundleConstructors(t, file)

	covered := make(map[string]bool)
	for _, clause := range dispatchCaseClauses(t, file) {
		names := caseClauseNames(t, clause)
		for _, stmt := range clause.Body {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			call, ok := ret.Results[0].(*ast.CallExpr)
			if !ok || !bundle[calleeName(call.Fun)] {
				continue
			}
			for _, name := range names {
				covered[name] = true
			}
		}
	}
	return covered
}

// e2eBinaryInvocation marks a Taskfile command line that runs the e2e binary
// itself. Mock-server commands under docker/compose take a --scenario flag too
// (ops.yml, crud-tools.yml, deep-research.yml, research-graph.yml) but name
// the MOCK's fixture set, not an e2e scenario — counting those would let an
// unrunnable e2e scenario pass on a name collision, so only lines invoking
// ./e2e are read.
const e2eBinaryInvocation = "./e2e"

// taskScenarioFlagPattern captures the argument of a --scenario flag in either
// `--scenario name` or `--scenario=name` form. The captured charset is name
// characters plus Task's `{{.VAR}}` template punctuation and nothing else, so
// the shell wrapping several targets use — `(cd cmd/e2e && ./e2e --scenario
// lifecycle) || rc=$?` — does not glue its closing paren onto the name. A name
// spelled in some shape this charset misses drops out of the runner set, which
// makes the guard fail loudly on that scenario rather than pass on it.
var taskScenarioFlagPattern = regexp.MustCompile(`--scenario[=\s]+([A-Za-z0-9_.{}-]+)`)

// printingCommandWord matches the first `echo` or `printf` command word on a
// Taskfile line. Everything from that word to end of line is an argument the
// shell PRINTS, so a `./e2e --scenario X` inside it is documentation, not an
// invocation. The leading alternation makes it a command word rather than a
// substring, so a scenario NAME containing the letters (core-echoedonly) or a
// path ending in them never matches; the trailing one keeps `echoes` and
// `printfoo` out.
var printingCommandWord = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])(echo|printf)(\s|$)`)

// executedTaskCommand returns the part of a Taskfile line the task runner
// actually executes, or "" when the line executes nothing.
//
// Review of this guard's first version found it counted any line CONTAINING
// "./e2e", with no check that the line was a command — so a scenario advertised
// in the menu and dispatchable in createScenario, but named only by a help
// string or a commented-out example, satisfied TestAdvertisedScenariosHaveARunner
// while nobody could stand it up. That is the gh#1129 shape surviving the
// gh#1129 guard. Both laundering shapes are live idioms in this tree:
// taskfiles/e2e/throughput.yml prints its manual-run instructions with echo,
// and taskfiles/e2e/lifecycle.yml + research-graph.yml carry scenario names in
// YAML comments. Proved by mutation, 2026-08-27: a synthetic core-echoedonly
// referenced only by those two shapes passed both guards before this check and
// fails after it.
//
// Two exclusions, in order:
//
//  1. A line whose content is a YAML comment. Task never runs it — and these
//     appear INSIDE cmds: blocks (lifecycle.yml:16-23), not only at file top.
//  2. Everything from the first echo/printf command word onward. Truncating
//     rather than rejecting the whole line keeps a real invocation that is
//     merely FOLLOWED by a progress echo counted; only the printed text is
//     dropped.
//
// It reads one line at a time, as the caller does, which is what the folded
// (`>-`) and block (`|`) scalars this repo writes commands in require. It is
// therefore not YAML-structural: a `./e2e --scenario X` written into a `desc:`
// string would still count. TestOnlyExecutedTaskLinesCountAsRunners pins every
// shape above, including that boundary.
func executedTaskCommand(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	if loc := printingCommandWord.FindStringIndex(line); loc != nil {
		return line[:loc[0]]
	}
	return line
}

// scenarioNamesInTaskfileContent returns the scenario names one Taskfile's text
// passes to the e2e binary on lines the runner executes. Split out from the
// file walk so the executed-versus-documented distinction can be driven through
// this exact production path with synthetic content rather than re-implemented
// in the test.
func scenarioNamesInTaskfileContent(content string) map[string]bool {
	names := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		command := executedTaskCommand(line)
		if !strings.Contains(command, e2eBinaryInvocation) {
			continue
		}
		for _, match := range taskScenarioFlagPattern.FindAllStringSubmatch(command, -1) {
			if strings.Contains(match[1], "{{") {
				continue // a Task template variable, not a literal scenario name
			}
			names[match[1]] = true
		}
	}
	return names
}

// scenarioNamesInvokedByTaskRunners returns every scenario name a Taskfile
// command passes to the e2e binary. Task targets are the owning declaration of
// "what anyone can actually run": gh#1129's scenario appeared in no target at
// all, which is why nobody could stand it up.
func scenarioNamesInvokedByTaskRunners(t *testing.T) map[string]bool {
	t.Helper()
	files := []string{filepath.Join("..", "..", "Taskfile.yml")}
	err := filepath.WalkDir(filepath.Join("..", "..", "taskfiles"),
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && filepath.Ext(path) == ".yml" {
				files = append(files, path)
			}
			return nil
		})
	require.NoError(t, err)
	require.GreaterOrEqualf(t, len(files), 10,
		"found only %d Taskfile(s) to scan for --scenario invocations — a path or extension regression, not a "+
			"repository that lost its task targets", len(files))

	names := make(map[string]bool)
	for _, path := range files {
		content, readErr := os.ReadFile(path) //nolint:gosec // fixed in-repo Taskfile paths
		require.NoError(t, readErr)
		maps.Copy(names, scenarioNamesInTaskfileContent(string(content)))
	}
	require.GreaterOrEqualf(t, len(names), 8,
		"only %d scenario names are invoked by any task target (%v) — a collapse below this floor means the "+
			"invocation parser stopped matching, not that the tiers stopped running scenarios",
		len(names), slices.Sorted(maps.Keys(names)))
	return names
}

// captureListOutput runs handleListCommand(true) — the production --list
// implementation — and returns everything it printed to stdout. Shared by
// TestLessonsScenarioIsDispatchedAndListed (main_test.go) and the guards above
// so all of them drive the exact same production output through one capture
// path instead of copies of the os.Pipe dance.
func captureListOutput(t *testing.T) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	require.True(t, handleListCommand(true), "handleListCommand(true) must return true")
	require.NoError(t, writer.Close())
	os.Stdout = original

	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(out)
}

// scenarioSectionHeader is the exact top-level (zero-indent) line that opens
// the block of runnable "name - description" entries in --list output. Used
// only to find the section, never to enumerate names.
const scenarioSectionHeader = "Individual Scenarios:"

// menuEntryPattern matches a "name  - description" line at ANY indentation
// once inside the Individual Scenarios section — e.g. "core-health     -
// Component health checks" or "tiered --variant structural  - Rules-only,
// ZERO embeddings/clusters". It is deliberately not anchored to a fixed indent
// depth: a prior version required exactly four leading spaces, so a new
// sub-section printed at a different depth (2, 6, 22 spaces...) was invisible
// to it. A line with no whitespace-hyphen-whitespace separator — a sub-header
// ("  Core:") or a wrapped continuation of the previous description — never
// matches, at any indent. The capture is the whole name field, including any
// trailing "--variant X"; the caller takes its first token.
var menuEntryPattern = regexp.MustCompile(`^\s+(\S.*?)\s+-\s+\S`)

// advertisedMenuScenarioNames runs the production --list output and extracts
// the runnable scenario name from every entry line inside the "Individual
// Scenarios:" section. The section is scoped by its own start/end markers —
// the header line and the next zero-indent line after it — never by a fixed
// indent depth, so entries at any indentation are still found, and lines from
// the adjacent "task e2e:<tier>" or "Variant flag" sections (which also
// contain "name - description" shaped text) are never mistaken for --scenario
// names.
func advertisedMenuScenarioNames(t *testing.T) []string {
	t.Helper()
	out := captureListOutput(t)

	var names []string
	inSection := false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			// Zero-indent line: a top-level section boundary.
			inSection = strings.TrimSpace(line) == scenarioSectionHeader
			continue
		}
		if !inSection {
			continue
		}
		m := menuEntryPattern.FindStringSubmatch(line)
		if m == nil {
			continue // sub-header or continuation line, not an entry — any indent
		}
		fields := strings.Fields(m[1])
		require.NotEmpty(t, fields, "matched menu line %q must yield a name token", line)
		names = append(names, fields[0])
	}
	require.NotEmptyf(t, names, "--list output must advertise at least one scenario under %q", scenarioSectionHeader)
	return names
}

// scenarioNameToken matches a single scenario-name-shaped word: lowercase,
// hyphenated, never quoted.
var scenarioNameToken = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// flagHelpNonNames is the closed set of tokens the -scenario usage
// parenthetical may carry that are not scenario names: the English connectors
// its prose uses, and the quoted 'all' sentinel (a mode of runScenarios, not a
// createScenario case). Every OTHER token must be a scenario name — see
// advertisedFlagHelpScenarioNames.
var flagHelpNonNames = map[string]bool{"or": true, "and": true, "'all'": true}

// tokenSplitPattern splits the -scenario usage parenthetical on commas AND
// whitespace runs. A comma-only split previously let a name joined to its
// neighbor without a comma (e.g. "... lessons core-bogus or 'all'") glue into
// one non-matching string that was silently dropped — the flag-help mirror of
// the menu's indentation coupling.
var tokenSplitPattern = regexp.MustCompile(`[,\s]+`)

// advertisedFlagHelpScenarioNames parses the -scenario flag's usage string
// (the flag.StringVar call registering "scenario" in main.go) and returns the
// bare scenario names listed in its parenthetical.
//
// Every token that is not in the closed flagHelpNonNames set FAILS here rather
// than being dropped. The previous version appended tokens matching
// scenarioNameToken and ignored the rest, so a malformed advertised name —
// "core_dataflow" for "core-dataflow" — vanished silently and the dispatch
// check passed on it (proved by mutation, 2026-08-27). A name that cannot be
// dispatched is the defect this file exists to catch; it must not be able to
// hide by being unparseable.
func advertisedFlagHelpScenarioNames(t *testing.T) []string {
	t.Helper()
	usage := scenarioFlagUsage(t)

	open := strings.Index(usage, "(")
	closeIdx := strings.LastIndex(usage, ")")
	require.True(t, open >= 0 && closeIdx > open, "-scenario usage text must list names in parens: %q", usage)

	var names []string
	for _, part := range tokenSplitPattern.Split(usage[open+1:closeIdx], -1) {
		part = strings.TrimSpace(part)
		if part == "" || flagHelpNonNames[part] {
			continue
		}
		require.Truef(t, scenarioNameToken.MatchString(part),
			"-scenario usage advertises token %q, which is neither a scenario name (%s) nor one of the "+
				"known connector/sentinel tokens %v. Anything else in this list is a name a user will pass "+
				"to --scenario and cannot be dispatched; it fails here instead of being dropped.",
			part, scenarioNameToken, slices.Sorted(maps.Keys(flagHelpNonNames)))
		names = append(names, part)
	}
	require.NotEmpty(t, names, "-scenario usage text must list at least one bare scenario name")
	return names
}

// scenarioFlagUsage returns the usage string main.go registers for the
// -scenario flag, read from the flag.StringVar call itself.
func scenarioFlagUsage(t *testing.T) string {
	t.Helper()
	var usage string
	ast.Inspect(parseMainGo(t), func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "StringVar" || len(call.Args) < 4 {
			return true
		}
		nameLit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || nameLit.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(nameLit.Value)
		require.NoError(t, err)
		if name != "scenario" {
			return true
		}
		usageLit, ok := call.Args[3].(*ast.BasicLit)
		require.True(t, ok, "flag.StringVar(..., \"scenario\", ...) usage argument must be a string literal")
		usage, err = strconv.Unquote(usageLit.Value)
		require.NoError(t, err)
		return false
	})
	require.NotEmpty(t, usage, "-scenario flag registration must be found by AST walk")
	return usage
}

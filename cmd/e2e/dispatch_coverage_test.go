package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAdvertisedScenariosAreDispatchable is the guard for the core-federation
// class of defect (gh#1129): a menu entry or -scenario flag help entry naming
// a scenario with no case in createScenario's dispatch switch fails for
// anyone who selects it. Both surfaces are enumerated from their OWNING
// declarations — the live createScenario switch (via go/ast on main.go) and
// the live handleListCommand/-scenario flag help output (by calling the
// production functions) — never a hand-copied name list, so this guard
// cannot silently drift from what the CLI actually offers or dispatches.
//
// Both extractors are scoped by structure (section boundaries, token
// separators), never by a fixed indentation depth or a comma-only split:
// gh#1129 review found the guard could be walked around by adding a menu
// entry at a different indent, or a flag-help name joined to its neighbor
// by " or " instead of ", or " — both previously matched nothing and were
// silently dropped instead of failing. See menuEntryPattern and
// tokenSplitPattern below, and the floor assertion on the menu count,
// which exists so a parser regression that silently stops matching entries
// fails loudly instead of vacuously passing on zero or one survivor.
//
// This only checks the advertised ⇒ dispatchable direction (a name printed
// in the menu or flag help must have a switch case). It does not require the
// reverse: some dispatchable names (e.g. "ops", "crud-tools", and the bare
// "structural"/"statistical"/"semantic" tiered-variant aliases) are
// intentionally not advertised as standalone menu entries, and that is not
// the failure class gh#1129 found — a menu entry that fails when selected.
//
// A per-scenario compose/task existence check was considered and skipped:
// scenario names do not map 1:1 onto task or compose targets (core-health
// and core-dataflow both run under `task e2e:core`; the tiered variants
// share `task e2e:structural`/`statistical`/`semantic`), so any such mapping
// would itself be a hand-maintained list — the exact anti-pattern this guard
// exists to avoid.
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

// dispatchableScenarioNames parses cmd/e2e/main.go's createScenario function
// and returns every case-clause string literal on its switch — the exact set
// of names --scenario can stand up. Enumerated by walking the real source at
// test time so it cannot go stale relative to the dispatcher it guards.
func dispatchableScenarioNames(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)

	names := make(map[string]bool)
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "createScenario" {
			return true
		}
		for _, stmt := range fn.Body.List {
			sw, ok := stmt.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			found = true
			for _, clauseStmt := range sw.Body.List {
				clause, ok := clauseStmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range clause.List {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, unquoteErr := strconv.Unquote(lit.Value)
					require.NoError(t, unquoteErr)
					names[value] = true
				}
			}
		}
		return false
	})
	require.True(t, found, "createScenario's switch statement must be found by AST walk")
	return names
}

// captureListOutput runs handleListCommand(true) — the production --list
// implementation — and returns everything it printed to stdout. Shared by
// TestLessonsScenarioIsDispatchedAndListed (main_test.go) and the dispatch
// coverage guard below so both drive the exact same production output
// through one capture path instead of two copies of the os.Pipe dance.
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

// scenarioSectionHeader is the exact top-level (zero-indent) line that
// opens the block of runnable "name - description" entries in --list
// output. Used only to find the section, never to enumerate names.
const scenarioSectionHeader = "Individual Scenarios:"

// menuEntryPattern matches a "name  - description" line at ANY indentation
// once inside the Individual Scenarios section — e.g. "core-health     -
// Component health checks" or "tiered --variant structural  - Rules-only,
// ZERO embeddings/clusters". It is deliberately not anchored to a fixed
// indent depth: a prior version required exactly four leading spaces, so a
// new sub-section printed at a different depth (2, 6, 22 spaces...) was
// invisible to it. A line with no whitespace-hyphen-whitespace separator —
// a sub-header ("  Core:") or a wrapped continuation of the previous
// description — never matches, at any indent. The capture is the whole
// name field, including any trailing "--variant X"; the caller takes its
// first token.
var menuEntryPattern = regexp.MustCompile(`^\s+(\S.*?)\s+-\s+\S`)

// advertisedMenuScenarioNames runs the production --list output and
// extracts the runnable scenario name from every entry line inside the
// "Individual Scenarios:" section. The section is scoped by its own
// start/end markers — the header line and the next zero-indent line after
// it — never by a fixed indent depth, so entries at any indentation are
// still found, and lines from the adjacent "task e2e:<tier>" or "Variant
// flag" sections (which also contain "name - description" shaped text) are
// never mistaken for --scenario names.
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
// hyphenated, never quoted. It rejects the quoted "'all'" sentinel outright
// (it starts with a non-letter) without needing to special-case it.
var scenarioNameToken = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// flagHelpStopwords are English connectors that can appear in the
// -scenario usage prose ("... lessons, or 'all'"). They are plain lowercase
// words, so they would otherwise pass scenarioNameToken despite never being
// a scenario name.
var flagHelpStopwords = map[string]bool{"or": true, "and": true}

// tokenSplitPattern splits the -scenario usage parenthetical on commas AND
// whitespace runs. A comma-only split previously let a name joined to its
// neighbor without a comma (e.g. "... lessons core-bogus or 'all'") glue
// into one non-matching string that scenarioNameToken silently dropped —
// the flag-help mirror of the menu's indentation coupling.
var tokenSplitPattern = regexp.MustCompile(`[,\s]+`)

// advertisedFlagHelpScenarioNames parses the -scenario flag's usage string
// (the flag.StringVar call registering "scenario" in main.go) and returns
// the bare scenario names listed in its parenthetical, dropping connector
// words and the quoted "'all'" sentinel.
func advertisedFlagHelpScenarioNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)

	var usage string
	ast.Inspect(f, func(n ast.Node) bool {
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
		name, unquoteErr := strconv.Unquote(nameLit.Value)
		require.NoError(t, unquoteErr)
		if name != "scenario" {
			return true
		}
		usageLit, ok := call.Args[3].(*ast.BasicLit)
		require.True(t, ok, "flag.StringVar(..., \"scenario\", ...) usage argument must be a string literal")
		usage, unquoteErr = strconv.Unquote(usageLit.Value)
		require.NoError(t, unquoteErr)
		return false
	})
	require.NotEmpty(t, usage, "-scenario flag registration must be found by AST walk")

	open := strings.Index(usage, "(")
	closeIdx := strings.LastIndex(usage, ")")
	require.True(t, open >= 0 && closeIdx > open, "-scenario usage text must list names in parens: %q", usage)
	inner := usage[open+1 : closeIdx]

	var names []string
	for _, part := range tokenSplitPattern.Split(inner, -1) {
		part = strings.TrimSpace(part)
		if part == "" || flagHelpStopwords[part] {
			continue
		}
		if scenarioNameToken.MatchString(part) {
			names = append(names, part)
		}
	}
	require.NotEmpty(t, names, "-scenario usage text must list at least one bare scenario name")
	return names
}

// Package referenceconfigs_test verifies the reference rule configs
// in configs/rules/**/*.json don't reference $entity.triple.* /
// $related.triple.* predicates that the framework doesn't stamp.
//
// #149 follow-up to a class of failure caught in #147 review: my
// first cut at configs/rules/example-fan-out/02-stamp-completion-
// on-parent.json referenced $entity.triple.agent.task.subtopic — a
// predicate the framework does NOT stamp anywhere. The reference
// pack would have silently shipped broken (substitution layer logs a
// Warn but doesn't error; the pattern collapses to a dedup-of-one
// counter that never reaches the join condition).
//
// This test closes the documentation-vs-reality class structurally:
// every $entity.triple.<predicate> / $related.triple.<predicate> in
// shipped reference configs must either:
//   - have its <predicate> declared as a constant in vocabulary/
//     agentic/predicates.go (framework-stamped), OR
//   - appear in the rulesStampedPredicates allowlist (rule-stamped
//     within the same pack, with a justification on the entry).
//
// New constants on either side fail this test in the same PR they
// land, not three sessions later when a sister project tries the pack.
package referenceconfigs_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tripleRefRe matches both $entity.triple.* and $related.triple.*
// references in any string field of a config. Capture group 1 is
// the predicate path. Stops on whitespace, quote, brace, comma, or
// any of the supported `.length` / `.triples` suffixes so we capture
// the predicate name without the suffix (suffixes are handled by
// the substitution layer — `.length` from #149, `.triples` from
// ADR-048 PR 4).
var tripleRefRe = regexp.MustCompile(`\$(?:entity|related)\.triple\.([\w.]+?)(?:\.(?:length|triples)\b|[^\w.]|$)`)

// rulesStampedPredicates is the allowlist of predicates that rules
// stamp on entities themselves (via add_triple / update_triple etc.)
// rather than the framework stamping in graph_writer.go or
// agentic-tools. Adding here requires a one-line justification.
//
// When this list grows, prefer moving the predicate into
// vocabulary/agentic/predicates.go so it's grep-discoverable from
// the framework side.
var rulesStampedPredicates = map[string]string{
	"gather.completed_child": "example-fan-out/02 stamps this on the parent loop entity when each child investigator completes; the counter the join rule matches against",
	// Add justified entries here as new reference packs land. Empty
	// justification (or absent entry) causes the test to fail.
}

// loadFrameworkStampedPredicates returns the set of predicates the
// framework declares across every vocabulary/*/predicates.go file.
// Discovered by walking all subdirectories of vocabulary/ and
// scanning for `Const = "predicate.shape"` declarations. Approximate
// but covers the agentic, sosa, csapi, oasf, oms, swe, governance
// packages without forcing reference-config authors to pick one
// vocab per pack. Operator-side predicates (stamped by rules
// themselves, not framework code) still need an entry in the
// rulesStampedPredicates allowlist.
//
// Reviewer recommendation 3 on PR #150: original scoping to
// vocabulary/agentic/predicates.go only would force false-positive
// allowlist entries for any future pack using CS API or SOSA
// predicates as triple references. Widened to all sibling packages
// to keep the lint useful as packs scale.
func loadFrameworkStampedPredicates(t *testing.T) map[string]struct{} {
	t.Helper()
	root := mustFindRepoRoot(t)

	// Grep for string constant declarations: lines like
	//   `Foo = "predicate.name"`
	// in the const block. Captures the right-hand side value.
	constRe := regexp.MustCompile(`^\s*\w+\s*=\s*"([\w.\-/:]+)"\s*$`)
	out := make(map[string]struct{})

	// scanRoots names every directory that owns predicate constants
	// the framework stamps. vocabulary/ is the canonical home for
	// cross-domain vocab (agentic, csapi, sosa, oms, swe, governance,
	// etc.); agentic/research/ owns the research-graph chain's
	// orchestration predicates colocated with the payload types they
	// relate to. New roots get added here when a package legitimately
	// owns its own predicate namespace outside vocabulary/.
	scanRoots := []string{
		filepath.Join(root, "vocabulary"),
		filepath.Join(root, "agentic", "research"),
	}

	walk := func(scanRoot string) error {
		return filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "predicates.go" {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				m := constRe.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				val := m[1]
				// Only count predicate-shaped strings (dotted lowercase).
				// Skip IRIs (contain `/` or `:`) and entity-ID samples.
				if strings.ContainsAny(val, "/:") {
					continue
				}
				out[val] = struct{}{}
			}
			return nil
		})
	}

	for _, scanRoot := range scanRoots {
		if err := walk(scanRoot); err != nil {
			t.Fatalf("walking %s: %v", scanRoot, err)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no predicates discovered under %v — scanner walker likely broken", scanRoots)
	}
	return out
}

// TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates is the
// structural anti-foot for the documentation-vs-reality failure
// class. Walks configs/rules/**/*.json, extracts every
// $entity.triple.* / $related.triple.* reference, and asserts each
// predicate is either declared as a framework constant in
// vocabulary/agentic/predicates.go OR allowlisted in
// rulesStampedPredicates with a justification.
//
// To add a new reference pack that introduces a rule-stamped
// predicate, add the predicate to rulesStampedPredicates above with
// a one-line justification naming the rule that stamps it. To add a
// new framework-stamped predicate, declare it in vocabulary/agentic/
// predicates.go (which this test discovers automatically).
func TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates(t *testing.T) {
	root := mustFindRepoRoot(t)
	configsDir := filepath.Join(root, "configs", "rules")

	framework := loadFrameworkStampedPredicates(t)

	type ref struct {
		predicate string
		file      string
	}
	var refs []ref

	walkErr := filepath.WalkDir(configsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// Walk every string field in the JSON.
		var v any
		if jerr := json.Unmarshal(data, &v); jerr != nil {
			t.Logf("skipping unparseable JSON %s: %v", path, jerr)
			return nil
		}
		extractStrings(v, func(s string) {
			for _, m := range tripleRefRe.FindAllStringSubmatch(s, -1) {
				refs = append(refs, ref{predicate: m[1], file: path})
			}
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", configsDir, walkErr)
	}
	if len(refs) == 0 {
		t.Fatal("found no $entity.triple.* / $related.triple.* references in any reference config — scanner likely broken (every shipped pack should have at least one)")
	}

	// Stable order so failure output is reproducible.
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].predicate != refs[j].predicate {
			return refs[i].predicate < refs[j].predicate
		}
		return refs[i].file < refs[j].file
	})

	for _, r := range refs {
		_, framed := framework[r.predicate]
		justification, allowed := rulesStampedPredicates[r.predicate]
		if !framed && !allowed {
			t.Errorf("config %s references $entity.triple.%s but the predicate is neither declared in vocabulary/agentic/predicates.go nor in test.rulesStampedPredicates. Either:\n  (a) declare it in vocabulary/agentic/predicates.go if the framework should stamp it, OR\n  (b) add it to rulesStampedPredicates with a one-line justification naming the rule that stamps it",
				strings.TrimPrefix(r.file, root+"/"),
				r.predicate)
			continue
		}
		if allowed && justification == "" {
			t.Errorf("predicate %q is in rulesStampedPredicates but has no justification — every allowlist entry needs a one-line reason naming the rule that stamps it",
				r.predicate)
		}
	}
}

// mustFindRepoRoot walks up from the test's working dir until it
// finds a directory containing go.mod. Tests invoked via `go test
// ./...` run from the package's directory; we need the repo root to
// access configs/ and vocabulary/.
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

// extractStrings recursively walks a JSON-unmarshalled value and
// calls visit on every string it encounters. Lets the scanner find
// triple references regardless of which field carries them
// (subject / predicate / object / prompt / for_each / etc.).
func extractStrings(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		visit(x)
	case []any:
		for _, item := range x {
			extractStrings(item, visit)
		}
	case map[string]any:
		for _, val := range x {
			extractStrings(val, visit)
		}
	}
}

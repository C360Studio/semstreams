package contract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The NATS server minor the whole repo runs on. Bumping NATS means changing
// this line and nothing else in this file.
const expectedNATSMinor = "2.14"

// natsImageRef matches a pinned NATS server reference in any form the repo
// uses, which is the point: they are NOT all spelled the same.
//
//	image: nats:2.14-alpine          compose, CI workflows
//	natsVersion: "2.14-alpine"       Go test-client defaults, built by
//	                                 concatenation as "nats:" + version
//	"2.14.4-alpine@sha256:…"         bare quoted fragments assigned to
//	                                 arbitrarily-named constants
//	                                 (…NATSServer…, WithNATSVersion args)
//
// The fragment forms are why this guard exists: a concatenated value has no
// `nats:`-prefixed literal to grep for. The fourth form was ADDED 2026-08-02
// after an audit found the previous patterns matched zero digest-pinned refs —
// the doc comment claimed coverage the regexes did not have, and two evidence
// gates in processor/graph-index sat on the OLD server pin, invisible to this
// guard, while their docs claimed the new one.
var (
	natsImageRef    = regexp.MustCompile(`nats:(\d+\.\d+)(?:\.\d+)?-alpine`)
	natsVersionVar  = regexp.MustCompile(`natsVersion\s*[:=]\s*"(\d+\.\d+)(?:\.\d+)?-alpine"`)
	natsFragmentRef = regexp.MustCompile(`"(\d+\.\d+)(?:\.\d+)?-alpine(?:@sha256:[0-9a-f]{64})?"`)
	floatingRef     = regexp.MustCompile(`nats:(latest|main|edge)\b`)
)

// scanRoots are the trees that describe what we RUN. Deliberately excluded:
// docs/adr and docs/operations/evidence record what WAS measured on older
// servers and must keep their historical versions, and openspec/changes/archive
// is closed history.
var scanRoots = []string{
	".github/workflows",
	"docker/compose",
	"taskfiles",
	"natsclient",
	"processor",
	"pkg",
	"test",
}

// TestNATSVersionIsConverged asserts every NATS server reference in the running
// configuration resolves to ONE minor version, and that none of them float.
//
// It exists because of a concrete miss. gh#790 converged the repo from three
// regimes (2.10-alpine, 2.12-alpine, and an unpinned nats:latest in CI) by
// grepping for the literal string `nats:2.12-alpine` — which silently skipped
// `natsclient/test_client.go`, where the image is built as
// `"nats:" + cfg.natsVersion` from a version FRAGMENT. The result shipped: CI
// and compose ran 2.14 while every integration test using NewTestClient still
// ran 2.12, in a change whose entire claim was "one version everywhere".
//
// A string search finds references spelled the way you guessed. This walks the
// trees that own them and accepts every spelling, so the next bump cannot half
// land.
func TestNATSVersionIsConverged(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	type ref struct{ file, version string }
	var refs []ref
	var floating []string

	for _, root := range scanRoots {
		walkRoot := filepath.Join(repoRoot, root)
		if _, err := os.Stat(walkRoot); err != nil {
			t.Fatalf("scan root %q missing — this guard has stopped guarding: %v", root, err)
		}
		err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch filepath.Ext(path) {
			case ".go", ".yml", ".yaml":
			default:
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, _ := filepath.Rel(repoRoot, path)
			// This file's own prose quotes the historical versions
			// (`nats:2.12-alpine`, `nats:latest`) to explain what the drift
			// was. Scanning itself would report those as live drift forever.
			// Keep the exclusion narrow — one file, by name — so it cannot
			// quietly grow into a way of hiding real references.
			if rel == filepath.Join("test", "contract", "nats_version_contract_test.go") {
				return nil
			}
			text := string(body)

			for _, m := range natsImageRef.FindAllStringSubmatch(text, -1) {
				refs = append(refs, ref{rel, m[1]})
			}
			for _, m := range natsVersionVar.FindAllStringSubmatch(text, -1) {
				refs = append(refs, ref{rel, m[1]})
			}
			// Bare quoted fragments (Go only — YAML image refs are unquoted
			// and already covered above; quoting one would double-count it,
			// which is harmless).
			if filepath.Ext(path) == ".go" {
				for _, m := range natsFragmentRef.FindAllStringSubmatch(text, -1) {
					refs = append(refs, ref{rel, m[1]})
				}
			}
			if floatingRef.MatchString(text) {
				floating = append(floating, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %q: %v", root, err)
		}
	}

	// A guard that finds nothing is not a guard. The repo has many NATS
	// references; a zero here means the patterns drifted from how they are
	// spelled, not that the repo went clean.
	if len(refs) < 10 {
		t.Fatalf("found only %d NATS references across %v — the patterns have drifted "+
			"from the source and this guard is checking almost nothing", len(refs), scanRoots)
	}

	var wrong []string
	for _, r := range refs {
		if r.version != expectedNATSMinor {
			wrong = append(wrong, r.file+" -> "+r.version)
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		t.Errorf("NATS version drift: expected every running reference to be %s, found %d that are not:\n  %s\n"+
			"Bumping NATS means updating expectedNATSMinor in this file AND every reference above. "+
			"Note they are not all spelled `nats:<v>` — the Go test client builds its image from a "+
			"version fragment.",
			expectedNATSMinor, len(wrong), strings.Join(wrong, "\n  "))
	}

	sort.Strings(floating)
	if len(floating) > 0 {
		t.Errorf("floating NATS reference (nats:latest and friends) in %d file(s):\n  %s\n"+
			"An unpinned server means the gate can break with no code change, and tests a "+
			"different substrate from every tier and developer.",
			len(floating), strings.Join(floating, "\n  "))
	}
}

// repoRootDir walks up from the test's working directory to the module root.
func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from the test directory")
		}
		dir = parent
	}
}

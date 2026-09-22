package contract

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file is the single home for "which binary does an E2E tier boot, under
// which gate". It replaces four per-hook source-text pins (the process-barrier,
// slow-consumer and milestone-probe build contracts, and the ops compose
// contract), each of which spelled one row of the same interpreted fact.
//
// The owner ruling on #1249 (2026-09-22, amending Q5) put the fact in one
// table, in the payload-registry capability spec, and required it to be READ
// from the artifacts that boot each tier rather than predicted: `build.target`
// in the tier's compose service, and the Go package plus `-tags=` of that
// target in docker/Dockerfile. The spec table is the source of truth here — the
// test parses it and drives every assertion from its rows, so there is no
// second copy of the table to drift.

const tierRepoRoot = "../.."

// tierTableHeader is the first cell pair of the spec table. It is matched
// literally so a renamed column is a loud failure rather than a silent
// zero-row parse.
const tierTableHeader = "| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate |"

var codeSpan = regexp.MustCompile("`([^`]+)`")

// tierRow is one row of the spec table: one tier, one compose service, one
// Dockerfile target, one binary, and the gates that keep the tier's hooks out
// of every other tier and out of every shipped artifact.
type tierRow struct {
	tier        string
	composeFile string
	service     string
	target      string
	binary      string
	tags        []string
	envGates    []string
}

func (r tierRow) String() string { return fmt.Sprintf("%s (%s %s)", r.tier, r.composeFile, r.service) }

// tierTable parses the table out of the payload-registry spec. Two homes are
// possible: an in-flight change's delta carries the target state until
// `openspec archive` syncs it into the live capability spec, after which the
// delta is gone and the live spec answers.
//
// Exactly one of them may carry the table. Picking the first match would be
// fail-open, and openspec's MODIFIED rule makes the ambiguous case likely
// rather than exotic: the next change touching this requirement must restate
// the whole block, table included, so two deltas would both carry it and a
// first-match resolver would silently govern by alphabetical change id — a
// corrupted table in the change under test would go unread. When more than one
// carries it, the author decides which governs; this refuses and names them.
func tierTable(t *testing.T) (string, []tierRow) {
	t.Helper()

	// A single `*` cannot reach archived changes: those live at
	// openspec/changes/archive/<date>-<id>/specs/, one level deeper.
	candidates, err := filepath.Glob(filepath.Join(tierRepoRoot, "openspec/changes/*/specs/payload-registry/spec.md"))
	if err != nil {
		t.Fatalf("glob change deltas: %v", err)
	}
	sort.Strings(candidates)
	candidates = append(candidates, filepath.Join(tierRepoRoot, "openspec/specs/payload-registry/spec.md"))

	var carriers []string
	bodies := map[string]string{}
	for _, path := range candidates {
		body, readErr := os.ReadFile(path) //nolint:gosec // repository-relative spec path
		if readErr != nil || !strings.Contains(string(body), tierTableHeader) {
			continue
		}
		carriers = append(carriers, path)
		bodies[path] = string(body)
	}

	switch len(carriers) {
	case 0:
		t.Fatalf("no payload-registry spec carries the tier table header %q", tierTableHeader)
	case 1:
	default:
		t.Fatalf("%d payload-registry specs carry the tier table, so which one governs is ambiguous: %s",
			len(carriers), strings.Join(carriers, ", "))
	}
	return carriers[0], parseTierRows(t, carriers[0], bodies[carriers[0]])
}

func parseTierRows(t *testing.T, path, body string) []tierRow {
	t.Helper()

	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, tierTableHeader) {
			start = i + 2 // skip the header and the |---| separator
			break
		}
	}
	if start < 0 || start >= len(lines) {
		t.Fatalf("%s: tier table header found but no rows follow it", path)
	}

	var rows []tierRow
	for _, line := range lines[start:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			t.Fatalf("%s: tier row has %d cells, want at least 4: %s", path, len(cells), line)
		}

		service := codeSpans(cells[1])
		if len(service) != 2 {
			t.Fatalf("%s: compose cell must hold exactly `<file>` `<service>`, got %v in %s", path, service, line)
		}
		target := codeSpans(cells[2])
		if len(target) != 2 {
			t.Fatalf("%s: target cell must hold exactly `<target>` `<binary>`, got %v in %s", path, target, line)
		}

		row := tierRow{
			tier:        strings.TrimSpace(cells[0]),
			composeFile: service[0],
			service:     service[1],
			target:      target[0],
			binary:      target[1],
		}
		// A gate token this test cannot classify is a defect in the row, not a
		// gate to ignore: an unread gate is the fail-open shape.
		for _, gate := range codeSpans(cells[3]) {
			switch {
			case strings.HasPrefix(gate, "-tags="):
				row.tags = append(row.tags, strings.Split(strings.TrimPrefix(gate, "-tags="), ",")...)
			case strings.HasPrefix(gate, "SEMSTREAMS_E2E_"):
				row.envGates = append(row.envGates, gate)
			default:
				t.Fatalf("%s: unclassified gate token %q in row %s", path, gate, row.tier)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatalf("%s: tier table parsed to zero rows", path)
	}
	return rows
}

func codeSpans(cell string) []string {
	var out []string
	for _, match := range codeSpan.FindAllStringSubmatch(cell, -1) {
		out = append(out, match[1])
	}
	return out
}

// composeService is the slice of a compose service this contract reads.
type composeService struct {
	Image string `yaml:"image"`
	Build *struct {
		Context    string `yaml:"context"`
		Dockerfile string `yaml:"dockerfile"`
		Target     string `yaml:"target"`
	} `yaml:"build"`
	Environment composeEnv `yaml:"environment"`
}

// composeEnv accepts both compose spellings of `environment:` — a list of
// KEY=VALUE strings and a mapping — so a future switch to the mapping form
// cannot silently empty the env-gate assertion.
type composeEnv []string

func (e *composeEnv) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var entries []string
		if err := value.Decode(&entries); err != nil {
			return err
		}
		*e = entries
		return nil
	case yaml.MappingNode:
		var entries map[string]string
		if err := value.Decode(&entries); err != nil {
			return err
		}
		for key, val := range entries {
			*e = append(*e, key+"="+val)
		}
		return nil
	default:
		return fmt.Errorf("environment: unsupported YAML kind %v", value.Kind)
	}
}

func (e composeEnv) names(prefix string) []string {
	var out []string
	for _, entry := range e {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// composeServices reads every service in docker/compose/ that is built from
// docker/Dockerfile, keyed "<file>/<service>".
func composeServices(t *testing.T) map[string]composeService {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(tierRepoRoot, "docker/compose/*.yml"))
	if err != nil {
		t.Fatalf("glob compose files: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("docker/compose holds no compose files")
	}

	out := make(map[string]composeService)
	for _, path := range paths {
		body, readErr := os.ReadFile(path) //nolint:gosec // repository-relative compose path
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		var file struct {
			Services map[string]composeService `yaml:"services"`
		}
		if err := yaml.Unmarshal(body, &file); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		for name, service := range file.Services {
			if service.Build == nil || service.Build.Dockerfile != "docker/Dockerfile" {
				continue
			}
			out[filepath.Base(path)+"/"+name] = service
		}
	}
	return out
}

// dockerBuild is one `go build` in the Dockerfile: the package it compiles and
// the tags it compiles it with.
type dockerBuild struct {
	pkg  string
	tags []string
}

// dockerfileTargets resolves a Dockerfile target name to the binary its image
// runs, by following the stage that copies a binary to /app/semstreams back to
// the `go build` that produced it, and through `FROM <stage>` inheritance.
type dockerfileTargets struct {
	artifacts map[string]dockerBuild // output path -> build
	installs  map[string]string      // stage -> output path copied to /app/semstreams
	bases     map[string]string      // stage -> base stage
}

func readDockerfileTargets(t *testing.T) dockerfileTargets {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(tierRepoRoot, "docker/Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}

	targets := dockerfileTargets{
		artifacts: make(map[string]dockerBuild),
		installs:  make(map[string]string),
		bases:     make(map[string]string),
	}
	stage := ""
	for _, line := range joinContinuations(string(body)) {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 4 && strings.EqualFold(fields[0], "FROM") && strings.EqualFold(fields[2], "AS"):
			stage = fields[3]
			targets.bases[stage] = fields[1]
		case strings.Contains(line, "go build"):
			build, out := parseGoBuild(t, fields)
			targets.artifacts[out] = build
		case len(fields) >= 3 && strings.EqualFold(fields[0], "COPY") && fields[len(fields)-1] == "/app/semstreams":
			targets.installs[stage] = fields[len(fields)-2]
		}
	}
	if len(targets.artifacts) == 0 || len(targets.installs) == 0 {
		t.Fatalf("Dockerfile parsed to %d builds and %d installs", len(targets.artifacts), len(targets.installs))
	}
	return targets
}

// joinContinuations folds shell line continuations so one RUN or COPY is one
// logical line.
func joinContinuations(body string) []string {
	var out []string
	var current strings.Builder
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, " \t")
		if strings.HasSuffix(line, "\\") {
			current.WriteString(strings.TrimSuffix(line, "\\"))
			current.WriteString(" ")
			continue
		}
		current.WriteString(line)
		out = append(out, current.String())
		current.Reset()
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

func parseGoBuild(t *testing.T, fields []string) (dockerBuild, string) {
	t.Helper()

	var build dockerBuild
	out := ""
	for i, field := range fields {
		switch {
		case strings.HasPrefix(field, "-tags="):
			build.tags = append(build.tags, strings.Split(strings.TrimPrefix(field, "-tags="), ",")...)
		case field == "-o" && i+1 < len(fields):
			out = fields[i+1]
		case strings.HasPrefix(field, "./cmd/"):
			if build.pkg != "" {
				t.Fatalf("Dockerfile go build names two packages: %s and %s", build.pkg, field)
			}
			build.pkg = strings.TrimPrefix(field, "./")
		}
	}
	if build.pkg == "" || out == "" {
		t.Fatalf("Dockerfile go build has package %q and output %q", build.pkg, out)
	}
	return build, out
}

// resolve returns the build behind a Dockerfile target, following FROM
// inheritance until a stage installs a binary.
func (d dockerfileTargets) resolve(t *testing.T, target string) dockerBuild {
	t.Helper()

	for seen := map[string]bool{}; target != ""; target = d.bases[target] {
		if seen[target] {
			t.Fatalf("Dockerfile target %q inherits in a cycle", target)
		}
		seen[target] = true
		if artifact, ok := d.installs[target]; ok {
			build, known := d.artifacts[artifact]
			if !known {
				t.Fatalf("Dockerfile target %q installs %q, which no stage builds", target, artifact)
			}
			return build
		}
	}
	t.Fatalf("Dockerfile target does not install a binary")
	return dockerBuild{}
}

// TestE2ETierTableMatchesComposeAndDockerfile is the one pin for the whole
// tier -> target -> binary -> gate fact. Every row of the spec table is checked
// against the compose service that boots the tier and the Dockerfile stage that
// builds its image, in both directions: no compose service built from
// docker/Dockerfile may be missing from the table, and no row may name a
// service that does not exist.
func TestE2ETierTableMatchesComposeAndDockerfile(t *testing.T) {
	specPath, rows := tierTable(t)
	t.Logf("tier table read from %s: %d rows", specPath, len(rows))

	services := composeServices(t)
	dockerfile := readDockerfileTargets(t)

	imagesByTarget := map[string]string{}
	claimed := map[string]bool{}

	for _, row := range rows {
		key := row.composeFile + "/" + row.service
		service, ok := services[key]
		if !ok {
			t.Errorf("%s: no compose service %q in docker/compose/%s builds docker/Dockerfile", row, row.service, row.composeFile)
			continue
		}
		claimed[key] = true

		if service.Build.Target != row.target {
			t.Errorf("%s: compose target = %q, spec table says %q", row, service.Build.Target, row.target)
			continue
		}

		build := dockerfile.resolve(t, row.target)
		if build.pkg != row.binary {
			t.Errorf("%s: Dockerfile target %q builds %q, spec table says %q", row, row.target, build.pkg, row.binary)
		}
		if got, want := strings.Join(sorted(build.tags), ","), strings.Join(sorted(row.tags), ","); got != want {
			t.Errorf("%s: Dockerfile target %q builds with tags [%s], spec table says [%s]", row, row.target, got, want)
		}

		// Both directions of the env gate. The milestone probe crashes and
		// quarantines on purpose, so arming it in another tier would read as a
		// flake, and disarming it in its own tier loses the proof silently.
		if got, want := strings.Join(service.Environment.names("SEMSTREAMS_E2E_"), ","), strings.Join(sorted(row.envGates), ","); got != want {
			t.Errorf("%s: compose sets [%s], spec table says [%s]", row, got, want)
		}

		// Per-target image tags: Docker caches by image name, so two targets
		// sharing one tag means whoever built last wins regardless of
		// `target:` (the beta.90 pre-tag leak, docker/compose/e2e.yml:45-52).
		if service.Image == "" {
			t.Errorf("%s: compose service has no per-target image tag", row)
			continue
		}
		if previous, seen := imagesByTarget[service.Image]; seen && previous != row.target {
			t.Errorf("image tag %q is shared by targets %q and %q", service.Image, previous, row.target)
		}
		imagesByTarget[service.Image] = row.target
	}

	for key := range services {
		if !claimed[key] {
			t.Errorf("compose service %s builds docker/Dockerfile but no tier table row names it", key)
		}
	}
}

// TestProductionRootReachesNoE2EHarnessWithoutABuildTag is the compile-time
// half of the rule: an E2E-only hook lands in the binary its tier boots, behind
// that tier's build tag, so the shipped production image links no harness at
// all. It sweeps the whole production root rather than the three hook files
// known today.
func TestProductionRootReachesNoE2EHarnessWithoutABuildTag(t *testing.T) {
	dockerfile := readDockerfileTargets(t)

	production := dockerfile.resolve(t, "production")
	if len(production.tags) != 0 {
		t.Fatalf("the shipped production target builds with tags %v", production.tags)
	}

	// The tags this package is legitimately overlaid with are the ones the
	// Dockerfile itself compiles it with — read, not listed here.
	overlay := map[string]bool{}
	for _, build := range dockerfile.artifacts {
		if build.pkg != production.pkg {
			continue
		}
		for _, tag := range build.tags {
			overlay[tag] = true
		}
	}
	if len(overlay) == 0 {
		t.Fatalf("no Dockerfile target overlays %s with a build tag", production.pkg)
	}

	entries, err := os.ReadDir(filepath.Join(tierRepoRoot, production.pkg))
	if err != nil {
		t.Fatalf("read production root: %v", err)
	}

	scanned, guarded := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++

		path := filepath.Join(tierRepoRoot, production.pkg, name)
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		if !importsE2EHarness(file.Imports) {
			continue
		}
		guarded++

		if reachableWithoutOverlayTags(t, path, overlay) {
			t.Errorf("%s/%s imports an E2E harness and builds without any of the overlay tags %v",
				production.pkg, name, sortedKeys(overlay))
		}
	}
	if scanned == 0 {
		t.Fatalf("no non-test Go files found in %s", production.pkg)
	}
	if guarded == 0 {
		t.Fatalf("no file in %s imports an E2E harness; if the last hook was removed, this guard's expectation goes with it", production.pkg)
	}
	t.Logf("%s: %d non-test files scanned, %d behind an overlay tag", production.pkg, scanned, guarded)
}

func importsE2EHarness(imports []*ast.ImportSpec) bool {
	for _, spec := range imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if strings.Contains(path, "/test/e2e/") || strings.Contains(path, "/internal/e2e") {
			return true
		}
	}
	return false
}

// reachableWithoutOverlayTags reports whether the file still builds when every
// E2E overlay tag is off — which is exactly "the ordinary build links this".
func reachableWithoutOverlayTags(t *testing.T, path string, overlay map[string]bool) bool {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // repository-relative source path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "package ") {
			break
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, parseErr := constraint.Parse(line)
		if parseErr != nil {
			t.Fatalf("parse build constraint in %s: %v", path, parseErr)
		}
		return expr.Eval(func(tag string) bool { return !overlay[tag] })
	}
	return true // no constraint at all: always built
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

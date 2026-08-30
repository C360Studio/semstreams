package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot is this package's path back to the repository root.
const repoRoot = "../../.."

// composeFile is the compose document whose profiles the three tier tasks run.
const composeFile = "docker/compose/tiered.yml"

// TestTierAuthorityMatchesShippedConfigs re-derives the tier authority table
// from the two artifacts that actually decide it — the compose profile's
// --config argument, and that config's platform.org / platform.id — and
// requires the table to equal what it finds.
//
// It pins the STEM, not the authority: since ADR-104 the running deployment
// mints platform.id plus an entropy suffix, and fixtures read that pair from
// semstreams_config/platform_identity (EffectiveAuthority). This test is what
// keeps the stem honest, so that observation's cross-check — "the stack I am
// driving is the configuration I named" — means something. If an operator
// renames a tier's platform.id, this unit test fails in a second instead of the
// tier failing with "entity not found" after ninety.
func TestTierAuthorityMatchesShippedConfigs(t *testing.T) {
	profileConfig := composeProfileConfigs(t)

	for variant, want := range tierAuthorityStem {
		t.Run(variant, func(t *testing.T) {
			configPath, ok := profileConfig[variant]
			if !ok {
				t.Fatalf("compose profile %q declares no --config argument in %s", variant, composeFile)
			}
			org, id := platformIdentity(t, filepath.Join(repoRoot, configPath))
			got := org + "." + id
			if got != want {
				t.Errorf("tier %q boots %s with platform %q, but the table says %q",
					variant, configPath, got, want)
			}
		})
	}
}

// TestCoreAuthorityMatchesShippedConfig is TestTierAuthorityMatchesShippedConfigs
// for the core stack, whose compose document declares no profiles: it reads the
// --config argument out of docker/compose/e2e.yml directly and requires
// CoreAuthorityStem to equal that config's platform.org.platform.id.
func TestCoreAuthorityMatchesShippedConfig(t *testing.T) {
	const coreComposeFile = "docker/compose/e2e.yml"
	data, err := os.ReadFile(filepath.Join(repoRoot, coreComposeFile))
	if err != nil {
		t.Fatalf("read %s: %v", coreComposeFile, err)
	}
	match := configArgument.FindStringSubmatch(string(data))
	if match == nil {
		t.Fatalf("no --config argument found in %s; the regex or the compose shape changed", coreComposeFile)
	}
	org, id := platformIdentity(t, filepath.Join(repoRoot, match[1]))
	if got := org + "." + id; got != CoreAuthorityStem {
		t.Errorf("core boots %s with platform %q, but CoreAuthorityStem says %q", match[1], got, CoreAuthorityStem)
	}
}

// TestTierAuthorityRejectsUnknownVariant pins the fail-closed behavior: an
// unregistered variant must panic rather than yield a plausible prefix.
func TestTierAuthorityRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TierAuthority returned for an unregistered variant instead of panicking")
		}
	}()
	_ = TierAuthorityStem("no-such-variant")
}

// TestTierStemEntityIDComposesUnderTheTierStem pins the composition helper. It
// composes under the DECLARED stem and is for shape assertions only; an ID the
// running stack would recognise carries the minted suffix.
func TestTierStemEntityIDComposesUnderTheTierStem(t *testing.T) {
	got := TierStemEntityID(VariantStructural, "sensor.environmental.temperature.temp-sensor-001")
	want := "c360.semstreams-e2e-structural.sensor.environmental.temperature.temp-sensor-001"
	if got != want {
		t.Errorf("TierStemEntityID = %q, want %q", got, want)
	}
	if parts := strings.Split(got, "."); len(parts) != 6 {
		t.Errorf("TierStemEntityID produced %d positions, want the canonical 6: %q", len(parts), got)
	}
}

// configArgument matches the `--config /app/configs/<name>.json` a profile's
// command passes, in either the exec-form list or the shell string form.
var configArgument = regexp.MustCompile(`/app/(configs/[A-Za-z0-9._-]+\.json)`)

// composeProfileConfigs reads the compose document and returns, per profile,
// the repo-relative config path that profile's service boots with.
func composeProfileConfigs(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, composeFile))
	if err != nil {
		t.Fatalf("read %s: %v", composeFile, err)
	}
	var doc struct {
		Services map[string]struct {
			Profiles []string `yaml:"profiles"`
			Command  []string `yaml:"command"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", composeFile, err)
	}
	found := map[string]string{}
	for name, service := range doc.Services {
		match := configArgument.FindStringSubmatch(strings.Join(service.Command, " "))
		if match == nil {
			continue
		}
		for _, profile := range service.Profiles {
			if existing, clash := found[profile]; clash && existing != match[1] {
				t.Fatalf("profile %q boots two configs (%s via %s, and %s)", profile, existing, name, match[1])
			}
			found[profile] = match[1]
		}
	}
	if len(found) == 0 {
		t.Fatalf("no profile in %s declares a --config argument; the regex or the compose shape changed", composeFile)
	}
	return found
}

// platformIdentity reads platform.org / platform.id out of a shipped config.
func platformIdentity(t *testing.T, path string) (string, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		Platform struct {
			Org string `json:"org"`
			ID  string `json:"id"`
		} `json:"platform"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if doc.Platform.Org == "" || doc.Platform.ID == "" {
		t.Fatalf("%s declares platform.org=%q platform.id=%q; both are required to mint",
			path, doc.Platform.Org, doc.Platform.ID)
	}
	return doc.Platform.Org, doc.Platform.ID
}

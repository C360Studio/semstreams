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
// It exists because the alternative is a fixture predicting a value the
// deployment owns. Every hardcoded entity ID in the tiered scenarios is built
// on TierAuthority; if an operator renames a tier's platform.id, this unit
// test fails in a second instead of the tier failing with "entity not found"
// after ninety.
func TestTierAuthorityMatchesShippedConfigs(t *testing.T) {
	profileConfig := composeProfileConfigs(t)

	for variant, want := range tierAuthority {
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

// TestTierAuthorityRejectsUnknownVariant pins the fail-closed behavior: an
// unregistered variant must panic rather than yield a plausible prefix.
func TestTierAuthorityRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TierAuthority returned for an unregistered variant instead of panicking")
		}
	}()
	_ = TierAuthority("no-such-variant")
}

// TestTierEntityIDComposesUnderTheTierAuthority pins the composition helper.
func TestTierEntityIDComposesUnderTheTierAuthority(t *testing.T) {
	got := TierEntityID(VariantStructural, "sensor.environmental.temperature.temp-sensor-001")
	want := "c360.semstreams-e2e-structural.sensor.environmental.temperature.temp-sensor-001"
	if got != want {
		t.Errorf("TierEntityID = %q, want %q", got, want)
	}
	if parts := strings.Split(got, "."); len(parts) != 6 {
		t.Errorf("TierEntityID produced %d positions, want the canonical 6: %q", len(parts), got)
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

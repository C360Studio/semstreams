package e2eboot

import (
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
)

var (
	testCLI = boot.CLI{
		ConfigPath: "configs/e2e.json", LogLevel: "warn", LogFormat: "json",
		Debug: true, DebugPort: 6061, ShutdownTimeout: 7 * time.Second, HealthPort: 9091, Validate: true,
	}
	testBuild = boot.BuildInfo{Version: "v0.0.0-test", GitCommit: "abc123", BuildTime: "2026-09-26"}
)

// env is a lookup over a fixed map, standing in for os.LookupEnv.
func env(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// extensionLengths reads every slice field of Options by reflection, so an
// extension added later is covered without editing the tests below.
func extensionLengths(opts boot.Options) map[string]int {
	lengths := map[string]int{}
	value := reflect.ValueOf(opts)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() == reflect.Slice {
			lengths[value.Type().Field(i).Name] = value.Field(i).Len()
		}
	}
	return lengths
}

// TestE2EBootWithNoOptionsIsTheProductionOptions is invariant I1: with no
// SEMSTREAMS_E2E_* variable enabled, the E2E binary's boot options are the
// production options — every extension empty in both, every scalar equal.
func TestE2EBootWithNoOptionsIsTheProductionOptions(t *testing.T) {
	unset := map[string]string{}
	allEmpty := map[string]string{}
	for _, name := range names() {
		allEmpty[name] = ""
	}
	for name, values := range map[string]map[string]string{
		"nothing set": unset,
		// NAME= is the same as unset: only a nonempty value enables.
		"every variable set to the empty string": allEmpty,
		// Runner-side variables share the prefix and must not change the boot.
		"only unknown prefixed variables set": {
			"SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT": "90s",
			"SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT": "30s",
			"SEMSTREAMS_E2E_NOT_AN_OPTION_AT_ALL": "1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			e2e := FromEnv(testCLI, testBuild, env(values))
			production := boot.Production(testCLI, testBuild)

			e2eValue, productionValue := reflect.ValueOf(e2e), reflect.ValueOf(production)
			scalars, extensions := 0, 0
			for i := 0; i < e2eValue.NumField(); i++ {
				field := e2eValue.Type().Field(i).Name
				if e2eValue.Field(i).Kind() == reflect.Slice {
					extensions++
					if n := productionValue.Field(i).Len(); n != 0 {
						t.Errorf("production %s has %d extensions, want none", field, n)
					}
					if n := e2eValue.Field(i).Len(); n != 0 {
						t.Errorf("E2E %s has %d extensions with no option enabled, want none", field, n)
					}
					continue
				}
				scalars++
				if !reflect.DeepEqual(e2eValue.Field(i).Interface(), productionValue.Field(i).Interface()) {
					t.Errorf("%s: E2E %+v, production %+v", field,
						e2eValue.Field(i).Interface(), productionValue.Field(i).Interface())
				}
			}
			if scalars == 0 || extensions == 0 {
				t.Fatalf("reflection saw %d scalar and %d extension fields; Options changed shape", scalars, extensions)
			}
			if !reflect.DeepEqual(production.CLI, testCLI) || production.Build != testBuild {
				t.Fatalf("production options dropped the command line or build: %+v", production)
			}
		})
	}
}

// TestE2EBootOptionAppendsExactlyItsExtensions is invariant I2: each variable
// grows exactly the extension slices design § 2.2 lists for it, by exactly the
// listed count, and nothing else.
func TestE2EBootOptionAppendsExactlyItsExtensions(t *testing.T) {
	// design.md § 2.2, one row per variable.
	declared := map[string]map[string]int{
		"SEMSTREAMS_E2E_EXAMPLES":        {"Components": 2, "Payloads": 3},
		"SEMSTREAMS_E2E_MISSION":         {"Components": 1, "Payloads": 1, "Workflows": 1},
		"SEMSTREAMS_E2E_LIFECYCLE_SEED":  {"PostStart": 1},
		"SEMSTREAMS_E2E_LESSON_CURATION": {"Responders": 1},
		"SEMSTREAMS_E2E_PROCESS_BARRIER": {"Tools": 1, "ConfigPatches": 1},
		"SEMSTREAMS_E2E_MILESTONE_PROBE": {"MilestoneHooks": 1},
		"SEMSTREAMS_E2E_SLOW_CONSUMER":   {"AfterConnect": 1},
	}
	if len(declared) != len(names()) {
		t.Fatalf("FromEnv reads %d variables %v, design § 2.2 declares %d", len(names()), names(), len(declared))
	}

	for name, growth := range declared {
		t.Run(name, func(t *testing.T) {
			for _, value := range []string{"1", "gcs.lifecycle.mission.m001"} {
				got := extensionLengths(FromEnv(testCLI, testBuild, env(map[string]string{name: value})))
				for field, length := range got {
					if length != growth[field] {
						t.Errorf("%s=%s: %s grew by %d, want %d", name, value, field, length, growth[field])
					}
				}
				for field := range growth {
					if _, ok := got[field]; !ok {
						t.Errorf("%s declares growth of %s, which Options does not have", name, field)
					}
				}
			}

			got := extensionLengths(FromEnv(testCLI, testBuild, env(map[string]string{name: ""})))
			for field, length := range got {
				if length != 0 {
					t.Errorf("%s= (empty): %s grew by %d, want nothing enabled", name, field, length)
				}
			}
		})
	}
}

// TestMilestoneProbeOptionUsesTheProbesOwnVariable keeps the table's literal
// and the harness's EnvVar one spelling.
func TestMilestoneProbeOptionUsesTheProbesOwnVariable(t *testing.T) {
	opts := FromEnv(testCLI, testBuild, env(map[string]string{milestoneprobe.EnvVar: "1"}))
	if len(opts.MilestoneHooks) != 1 {
		t.Fatalf("%s=1 enabled %d milestone hooks, want 1", milestoneprobe.EnvVar, len(opts.MilestoneHooks))
	}
}

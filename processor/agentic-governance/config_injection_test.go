package agenticgovernance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestInjectionClassifierConfig_JSONRoundTrip pins the operator-
// configurable surface contract per the
// feedback_polymorphic_config_needs_json_roundtrip_test discipline.
// Every field reachable from operator config must round-trip
// through Marshal/Unmarshal without loss. Classifier lives on
// FilterConfig as a peer to InjectionConfig, not nested inside it.
func TestInjectionClassifierConfig_JSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   FilterConfig
	}{
		{
			name: "classifier block omitted",
			in: FilterConfig{
				Name:    "injection_classifier",
				Enabled: false,
			},
		},
		{
			name: "shadow-mode populated",
			in: FilterConfig{
				Name:    "injection_classifier",
				Enabled: true,
				ClassifierConfig: &InjectionClassifierConfig{
					Threshold:  0.7,
					ShadowMode: true,
					CorpusSources: []InjectionCorpusSource{
						{Domain: "internal-seed", Version: "v0", Path: "testdata/internal_seed_v0.jsonl"},
					},
				},
			},
		},
		{
			name: "enforce-mode multi-source",
			in: FilterConfig{
				Name:    "injection_classifier",
				Enabled: true,
				ClassifierConfig: &InjectionClassifierConfig{
					Threshold:  0.75,
					ShadowMode: false,
					CorpusSources: []InjectionCorpusSource{
						{Domain: "internal-seed", Version: "v0", Path: "a.jsonl"},
						{Domain: "deepset", Version: "v1", Path: "b.jsonl"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(&tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got FilterConfig
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tc.in, got) {
				t.Errorf("round-trip mismatch\nwant: %+v\ngot:  %+v", tc.in, got)
			}
		})
	}
}

// TestInjectionClassifierConfig_Validate enforces the boot-time
// guards: threshold range, corpus required, blank fields rejected.
// The outer FilterConfig.Enabled gates filter activation; this
// inner Validate runs unconditionally once the operator has chosen
// to supply a ClassifierConfig.
func TestInjectionClassifierConfig_Validate(t *testing.T) {
	cases := []struct {
		name   string
		cfg    InjectionClassifierConfig
		errSub string
	}{
		{
			name:   "negative threshold",
			cfg:    InjectionClassifierConfig{Threshold: -0.1},
			errSub: "threshold must be between",
		},
		{
			name:   "threshold over one",
			cfg:    InjectionClassifierConfig{Threshold: 1.5},
			errSub: "threshold must be between",
		},
		{
			name:   "no corpus sources",
			cfg:    InjectionClassifierConfig{Threshold: 0.7},
			errSub: "corpus_sources is required",
		},
		{
			name: "blank domain in source",
			cfg: InjectionClassifierConfig{
				Threshold:     0.7,
				CorpusSources: []InjectionCorpusSource{{Path: "x.jsonl"}},
			},
			errSub: "domain empty",
		},
		{
			name: "blank path in source",
			cfg: InjectionClassifierConfig{
				Threshold:     0.7,
				CorpusSources: []InjectionCorpusSource{{Domain: "x"}},
			},
			errSub: "path empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errSub)
			}
		})
	}
}

// TestFilterConfig_ClassifierRequiresConfig confirms the
// injection_classifier filter case rejects a missing
// ClassifierConfig at boot rather than constructing a useless
// filter. Operator error must surface at config load.
func TestFilterConfig_ClassifierRequiresConfig(t *testing.T) {
	cfg := FilterConfig{
		Name:    "injection_classifier",
		Enabled: true,
		// ClassifierConfig intentionally nil.
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected error for missing classifier_config")
	}
	if !strings.Contains(err.Error(), "classifier_config is required") {
		t.Errorf("error %q does not mention required classifier_config", err.Error())
	}
}

// TestFilterConfig_ClassifierNestedValidate propagates inner errors
// (threshold out of range) through FilterConfig.Validate. Easy
// regression target if a future refactor stops calling
// ClassifierConfig.Validate.
func TestFilterConfig_ClassifierNestedValidate(t *testing.T) {
	cfg := FilterConfig{
		Name:    "injection_classifier",
		Enabled: true,
		ClassifierConfig: &InjectionClassifierConfig{
			Threshold:     2.0,
			CorpusSources: []InjectionCorpusSource{{Domain: "x", Path: "y"}},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected nested validation error")
	}
	if !strings.Contains(err.Error(), "threshold must be between") {
		t.Errorf("error %q does not contain threshold message", err.Error())
	}
}

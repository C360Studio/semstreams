package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	e2econfig "github.com/c360studio/semstreams/test/e2e/config"
)

const (
	// platformIdentityBucket is the shared configuration bucket.
	platformIdentityBucket = "semstreams_config"
	// platformIdentityKey is where a deployment durably records the authority
	// it mints under (ADR-104).
	platformIdentityKey = "platform_identity"
)

// mintedSuffix matches the entropy suffix the framework mints onto platform.id:
// a separator and exactly six lowercase hex bytes.
var mintedSuffix = regexp.MustCompile(`^-[0-9a-f]{6}$`)

// MintedAuthorityScenario proves the running stack minted, recorded, and is
// using a per-deployment authority (ADR-104) — the value every other fixture
// now READS instead of predicting.
type MintedAuthorityScenario struct {
	natsURL       string
	declaredStem  string
	nats          *client.NATSValidationClient
	requireSuffix bool
}

// NewMintedAuthorityScenario builds the core tier's validate-minted-authority
// stage. declaredStem is the org.platform the stack's shipped configuration
// declares; the recorded identity must have that stem and an entropy suffix.
func NewMintedAuthorityScenario(natsURL, declaredStem string) *MintedAuthorityScenario {
	return &MintedAuthorityScenario{natsURL: natsURL, declaredStem: declaredStem, requireSuffix: true}
}

// Name returns the scenario identifier.
func (s *MintedAuthorityScenario) Name() string { return "core-minted-authority" }

// Description summarizes what the stage proves.
func (s *MintedAuthorityScenario) Description() string {
	return "The deployment minted an entropy suffix onto platform.id, recorded it as {org, stem, id}, and mints under it"
}

// Setup opens the scenario-owned NATS validation connection.
func (s *MintedAuthorityScenario) Setup(ctx context.Context) error {
	nats, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		return err
	}
	s.nats = nats
	return nil
}

// Execute reads the identity record and asserts its shape, its stem, and that
// EffectiveAuthority resolves through it.
func (s *MintedAuthorityScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.Name(), StartTime: time.Now(),
		Metrics: make(map[string]any), Details: make(map[string]any),
	}
	fail := func(err error) (*Result, error) {
		result.Error = err.Error()
		result.Errors = []string{err.Error()}
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	raw, err := s.nats.GetKV(ctx, platformIdentityBucket, platformIdentityKey)
	if err != nil {
		return fail(fmt.Errorf("read %s/%s: %w", platformIdentityBucket, platformIdentityKey, err))
	}

	// Exactly three fields — the record's shape is a cross-repo contract.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fail(fmt.Errorf("parse the identity record: %w", err))
	}
	if len(fields) != 3 {
		return fail(fmt.Errorf("identity record carries %d fields, want exactly org/stem/id: %s", len(fields), raw))
	}
	for _, want := range []string{"org", "stem", "id"} {
		if _, ok := fields[want]; !ok {
			return fail(fmt.Errorf("identity record is missing %q: %s", want, raw))
		}
	}

	var record struct {
		Org  string `json:"org"`
		Stem string `json:"stem"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return fail(fmt.Errorf("decode the identity record: %w", err))
	}
	if s.requireSuffix {
		if len(record.ID) <= len(record.Stem) || record.ID[:len(record.Stem)] != record.Stem {
			return fail(fmt.Errorf("recorded id %q is not the stem %q plus a suffix", record.ID, record.Stem))
		}
		if suffix := record.ID[len(record.Stem):]; !mintedSuffix.MatchString(suffix) {
			return fail(fmt.Errorf("recorded id %q carries suffix %q, want %q", record.ID, suffix, mintedSuffix))
		}
	}

	// The same read every adopter makes, including its stem cross-check.
	authority, err := e2econfig.EffectiveAuthority(ctx, s.nats, s.declaredStem)
	if err != nil {
		return fail(err)
	}
	if authority != record.Org+"."+record.ID {
		return fail(fmt.Errorf("EffectiveAuthority returned %q, record says %q", authority, record.Org+"."+record.ID))
	}

	result.Details["declared_stem"] = s.declaredStem
	result.Details["effective_authority"] = authority
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// Teardown closes the scenario-owned NATS connection.
func (s *MintedAuthorityScenario) Teardown(ctx context.Context) error {
	if s.nats == nil {
		return nil
	}
	return s.nats.Close(ctx)
}

// PreIdentityBucketScenario drives the two halves of the pre-identity-bucket
// refusal stage: `seed` writes a configuration bucket of the shape that existed
// before identity minting, and `assert` proves the refused boot created no
// identity record. The boot itself is the taskfile's job, because only it can
// observe the container's exit code and logs.
type PreIdentityBucketScenario struct {
	natsURL string
	mode    string
	stem    string
	nats    *client.NATSValidationClient
}

// NewPreIdentityBucketScenario builds one half of the stage. mode is "seed" or
// "assert"; declaredStem is the org.platform of the configuration the app boots.
func NewPreIdentityBucketScenario(natsURL, mode, declaredStem string) *PreIdentityBucketScenario {
	return &PreIdentityBucketScenario{natsURL: natsURL, mode: mode, stem: declaredStem}
}

// Name returns the scenario identifier, distinguished by mode.
func (s *PreIdentityBucketScenario) Name() string { return "core-pre-identity-bucket-" + s.mode }

// Description summarizes the half being run.
func (s *PreIdentityBucketScenario) Description() string {
	if s.mode == "seed" {
		return "Writes a configuration bucket that predates identity minting: platform and version, no platform_identity"
	}
	return "Proves a refused pre-identity boot minted nothing and created no platform_identity record"
}

// Setup opens the scenario-owned NATS validation connection.
func (s *PreIdentityBucketScenario) Setup(ctx context.Context) error {
	nats, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		return err
	}
	s.nats = nats
	return nil
}

// Execute performs the seed or the assertion.
func (s *PreIdentityBucketScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.Name(), StartTime: time.Now(),
		Metrics: make(map[string]any), Details: make(map[string]any),
	}
	finish := func(err error) (*Result, error) {
		if err != nil {
			result.Error = err.Error()
			result.Errors = []string{err.Error()}
		} else {
			result.Success = true
		}
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	org, stem, ok := cutAuthority(s.stem)
	if !ok {
		return finish(fmt.Errorf("declared authority %q is not org.platform", s.stem))
	}

	switch s.mode {
	case "seed":
		platform, err := json.Marshal(map[string]string{"org": org, "id": stem, "type": "test"})
		if err != nil {
			return finish(err)
		}
		if err := s.nats.PutKV(ctx, platformIdentityBucket, "platform", platform); err != nil {
			return finish(fmt.Errorf("seed the platform key: %w", err))
		}
		version, err := json.Marshal("1.0.0")
		if err != nil {
			return finish(err)
		}
		if err := s.nats.PutKV(ctx, platformIdentityBucket, "version", version); err != nil {
			return finish(fmt.Errorf("seed the version key: %w", err))
		}
		if _, err := s.nats.GetKV(ctx, platformIdentityBucket, platformIdentityKey); err == nil {
			return finish(errors.New("the seeded bucket already holds a platform_identity record; it does not predate identity minting"))
		}
		result.Details["seeded"] = []string{"platform", "version"}
		return finish(nil)

	case "assert":
		if _, err := s.nats.GetKV(ctx, platformIdentityBucket, platformIdentityKey); err == nil {
			return finish(errors.New("a refused pre-identity boot created a platform_identity record; it must mint nothing and create nothing"))
		}
		result.Details["platform_identity_absent"] = true
		return finish(nil)

	default:
		return finish(fmt.Errorf("unknown mode %q, want seed or assert", s.mode))
	}
}

// Teardown closes the scenario-owned NATS connection.
func (s *PreIdentityBucketScenario) Teardown(ctx context.Context) error {
	if s.nats == nil {
		return nil
	}
	return s.nats.Close(ctx)
}

// cutAuthority splits an org.platform pair.
func cutAuthority(authority string) (string, string, bool) {
	for i := 0; i < len(authority); i++ {
		if authority[i] == '.' {
			return authority[:i], authority[i+1:], authority[:i] != "" && authority[i+1:] != ""
		}
	}
	return "", "", false
}

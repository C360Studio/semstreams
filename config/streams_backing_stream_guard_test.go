package config

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// guardTestManager builds a StreamsManager over a Client that has NO JetStream
// context. Any provisioning call that actually reaches NATS therefore fails with
// Client.JetStream's "JetStream not initialized" — which makes that string the
// negative control for every test below: if it appears, the name guard did NOT
// fire and the provisioner was on its way to CreateStream/UpdateStream.
func guardTestManager() *StreamsManager {
	return NewStreamsManager(&natsclient.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// jetStreamReached is the marker Client.JetStream emits when a provisioning
// call got past the name guard and tried to talk to NATS.
const jetStreamReached = "JetStream not initialized"

// The clauses a refusal diagnostic must carry so the operator has somewhere to
// go next. These are matched exactly rather than by a loose word like "catalog"
// or "content", both of which occur incidentally in the surrounding prose and so
// would pass against a diagnostic that had lost its actionable half.
//
// The KV list deliberately requires BOTH halves. A message naming only the
// framework catalog seam dead-ends the operator of a PRODUCT bucket
// (graph.EnsureCatalogBucket rejects a name outside the catalog), which is
// exactly the straddle the prefix-not-membership rule creates.
var (
	kvOwnerClauses = []string{
		"bucket descriptor catalog's acquisition seam",
		"outside that catalog is provisioned by the component that owns it",
	}
	objOwnerClauses = []string{
		"content-store constructor",
	}
)

func guardTestConfig() *Config {
	return &Config{
		Version:    "1.0.0",
		Platform:   PlatformConfig{Org: "acme", ID: "test"},
		Components: ComponentConfigs{},
		Streams:    StreamConfigs{},
	}
}

// portComponent builds an enabled component whose single JetStream output port
// carries the given explicit stream_name (or, when empty, the given subject for
// DeriveStreamName to work on).
func portComponent(t *testing.T, streamName, subject string) types.ComponentConfig {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"ports": map[string]any{
			"outputs": []map[string]any{{
				"name":        "out",
				"type":        "jetstream",
				"subject":     subject,
				"stream_name": streamName,
			}},
		},
	})
	require.NoError(t, err)
	return types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "guard-test",
		Enabled: true,
		Config:  raw,
	}
}

// TestEnsureStreams_RefusesKVAndObjectStoreBackingStreams is the POSITIVE guard
// test for the stream-provisioning prohibition: the provisioner governs ORDINARY
// streams only, and a declaration naming a KV (`KV_`) or ObjectStore (`OBJ_`)
// backing stream must fail closed, naming the resource and the owner that
// legitimately provisions it.
//
// The hazard being guarded is NOT "these names skip a bounds requirement" — it
// is the provisioner WRITING retention/config onto a graph or content backing
// stream. createStream stamps the declared MaxAge/MaxBytes/Discard onto whatever
// it is handed, and the bounds contract REQUIRES an ordinary stream to declare
// them, so a single operator typo (`KV_ENTITY_STATES` in cfg.Streams) would be
// reachability-blind age eviction stamped onto authoritative graph state — and
// the operator would have been told to supply it.
//
// Both operator-authored entry points are covered: the arbitrary-key map at
// cfg.Streams, and the port-derived stream name (explicit stream_name AND the
// subject-derived fallback, since DeriveStreamName uppercases the first subject
// token and "kv_entity_states.stored" therefore yields "KV_ENTITY_STATES").
func TestEnsureStreams_RefusesKVAndObjectStoreBackingStreams(t *testing.T) {
	kvBackingStream := natsclient.KVStreamPrefix + graph.BucketEntityStates
	const objBackingStream = "OBJ_MESSAGES" // ObjectStore default bucket "MESSAGES"

	tests := []struct {
		name       string
		mutate     func(*Config)
		wantNamed  string   // the refused resource must be named in the diagnostic
		wantOwner  []string // the seam(s) that legitimately provision it, named precisely
		wantSource string   // where the declaration came from
	}{
		{
			name: "operator stream map declares a KV backing stream",
			mutate: func(c *Config) {
				c.Streams[kvBackingStream] = StreamConfig{Subjects: []string{"kv_entity_states.>"}}
			},
			wantNamed:  kvBackingStream,
			wantOwner:  kvOwnerClauses,
			wantSource: "config.streams",
		},
		{
			name: "operator stream map declares an ObjectStore backing stream",
			mutate: func(c *Config) {
				c.Streams[objBackingStream] = StreamConfig{Subjects: []string{"obj_messages.>"}}
			},
			wantNamed:  objBackingStream,
			wantOwner:  objOwnerClauses,
			wantSource: "config.streams",
		},
		{
			name: "component port declares a KV backing stream as stream_name",
			mutate: func(c *Config) {
				c.Components["kvport"] = portComponent(t, kvBackingStream, "anything.out")
			},
			wantNamed:  kvBackingStream,
			wantOwner:  kvOwnerClauses,
			wantSource: "kvport",
		},
		{
			name: "component port subject derives a KV backing stream name",
			mutate: func(c *Config) {
				c.Components["derivedkv"] = portComponent(t, "", "kv_entity_states.stored")
			},
			wantNamed:  kvBackingStream,
			wantOwner:  kvOwnerClauses,
			wantSource: "derivedkv",
		},
		{
			name: "component port declares an ObjectStore backing stream as stream_name",
			mutate: func(c *Config) {
				c.Components["objport"] = portComponent(t, objBackingStream, "anything.out")
			},
			wantNamed:  objBackingStream,
			wantOwner:  objOwnerClauses,
			wantSource: "objport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := guardTestConfig()
			tt.mutate(cfg)

			err := guardTestManager().EnsureStreams(context.Background(), cfg)

			require.Error(t, err, "provisioning a backing stream must fail closed")
			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable,
				"refusal must be classifiable, not just any error")
			assert.True(t, errs.IsFatal(err), "a backing-stream declaration is unrecoverable, not retryable")
			assert.Contains(t, err.Error(), tt.wantNamed, "diagnostic must name the refused resource")
			for _, clause := range tt.wantOwner {
				assert.Contains(t, err.Error(), clause,
					"diagnostic must tell the operator who legitimately provisions this resource")
			}
			assert.Contains(t, err.Error(), tt.wantSource, "diagnostic must name the declaration source")
			assert.NotContains(t, err.Error(), jetStreamReached,
				"refusal must happen at declaration validation, before any JetStream call")
		})
	}
}

// TestCreateStream_RefusesBackingStreamBeforeAnyJetStreamCall covers the second
// half of the guard: createStream itself refuses, so no future caller path
// (a new derivation rule, a direct call, a test helper) can reach CreateStream
// or UpdateStream with a backing-stream name by bypassing EnsureStreams's
// declaration validation. The ordinary-name case is the positive control: it
// must get PAST the guard and fail on the absent JetStream context instead.
func TestCreateStream_RefusesBackingStreamBeforeAnyJetStreamCall(t *testing.T) {
	tests := []struct {
		name          string
		stream        string
		wantRefused   bool
		wantSubstring string
	}{
		{
			name:          "KV backing stream refused",
			stream:        natsclient.KVStreamPrefix + graph.BucketEntityStates,
			wantRefused:   true,
			wantSubstring: natsclient.KVStreamPrefix + graph.BucketEntityStates,
		},
		{
			name:          "ObjectStore backing stream refused",
			stream:        "OBJ_MESSAGES",
			wantRefused:   true,
			wantSubstring: "OBJ_MESSAGES",
		},
		{
			name:          "ordinary stream reaches the JetStream call",
			stream:        "AGENT",
			wantRefused:   false,
			wantSubstring: jetStreamReached,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fully bounded so the ordinary-name positive control fails on the
			// absent JetStream context rather than on the bounds contract — the
			// assertion here is about the PREFIX guard.
			err := guardTestManager().createStream(context.Background(),
				boundedDeclaration(tt.stream, "whatever.>"))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSubstring)
			if tt.wantRefused {
				require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
				assert.NotContains(t, err.Error(), jetStreamReached,
					"refusal must precede any JetStream access")
			} else {
				assert.NotErrorIs(t, err, ErrBackingStreamNotProvisionable,
					"an ordinary stream name must not be refused by the guard")
			}
		})
	}
}

// TestStreamProvisioner_RefusalIsByPrefixNotCatalogMembership pins the reason
// the guard is a PREFIX rule rather than a descriptor-catalog membership test.
// Every downstream safety net has a hole for a non-catalog bucket: the KV
// retention reconciler runs only for catalog descriptors (and even then clears
// only MaxAge/MaxBytes, never a discard policy), and a product or sister-repo
// bucket has neither an acquisition seam nor a pre-start backstop to repair a
// stamp the provisioner applied. So a `KV_*` name the framework has never heard
// of must be refused on exactly the same rule.
func TestStreamProvisioner_RefusalIsByPrefixNotCatalogMembership(t *testing.T) {
	// A product bucket name, deliberately outside the framework descriptor
	// catalog. The two assertions below are the straddle: this one is NOT in
	// the catalog, ENTITY_STATES IS — and both are refused identically.
	const productBucket = "SEMSOURCE_DOCUMENTS"
	require.Empty(t, graph.OwnerOf(productBucket),
		"test premise: %q must be outside the framework descriptor catalog", productBucket)
	require.NotEmpty(t, graph.OwnerOf(graph.BucketEntityStates),
		"test premise: the catalog must be populated, or the straddle is vacuous")

	productBackingStream := natsclient.KVStreamPrefix + productBucket

	t.Run("declaration validation refuses a non-catalog KV backing stream", func(t *testing.T) {
		cfg := guardTestConfig()
		cfg.Streams[productBackingStream] = StreamConfig{Subjects: []string{"kv_semsource_documents.>"}}

		err := guardTestManager().EnsureStreams(context.Background(), cfg)

		require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
		assert.Contains(t, err.Error(), productBackingStream)
		assert.NotContains(t, err.Error(), jetStreamReached)
	})

	t.Run("createStream refuses a non-catalog KV backing stream", func(t *testing.T) {
		err := guardTestManager().createStream(context.Background(),
			boundedDeclaration(productBackingStream, "kv_semsource_documents.>"))

		require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
		assert.Contains(t, err.Error(), productBackingStream)
		assert.NotContains(t, err.Error(), jetStreamReached)
	})

	// Refusing correctly is only half the job: the operator of a NON-catalog
	// bucket must not be sent somewhere that will reject them. A diagnostic that
	// names only graph.EnsureCatalogBucket is a dead end for this exact case —
	// that function errors with "not in the framework KV catalog" for a product
	// bucket — so the message must also say a bucket outside the catalog is
	// provisioned by the component that owns it.
	t.Run("the non-catalog diagnostic does not dead-end the operator", func(t *testing.T) {
		err := guardTestManager().createStream(context.Background(),
			boundedDeclaration(productBackingStream, "kv_semsource_documents.>"))

		require.Error(t, err)
		for _, clause := range kvOwnerClauses {
			assert.Contains(t, err.Error(), clause,
				"a product-bucket declarant must be told who provisions THEIR bucket, "+
					"not only the framework catalog seam that would reject them")
		}
	})
}

// TestConfigValidate_RefusesBackingStreamDeclaration closes the `--validate`
// hole. cmd/semstreams runs Config.Validate, prints "✓ Configuration is valid"
// and exits BEFORE streams are ensured — so a config carrying a backing-stream
// declaration would be greenlit by the very tool an operator uses to check it,
// then hard-fail at boot. The guard is pure and I/O-free so it can run here.
func TestConfigValidate_RefusesBackingStreamDeclaration(t *testing.T) {
	kvBackingStream := natsclient.KVStreamPrefix + graph.BucketEntityStates

	t.Run("valid config with no stream declarations passes", func(t *testing.T) {
		require.NoError(t, guardTestConfig().Validate(), "control: the base config must be valid")
	})

	t.Run("ordinary stream declaration still passes", func(t *testing.T) {
		cfg := guardTestConfig()
		// Bounded, because an ordinary stream now also owes the bounds contract;
		// the assertion here is that the PREFIX guard does not refuse it.
		cfg.Streams["AGENT"] = boundedStreamConfig("agent.>")
		require.NoError(t, cfg.Validate(), "an ordinary stream must not be refused")
	})

	for _, name := range []string{kvBackingStream, "OBJ_MESSAGES", "KV_SEMSOURCE_DOCUMENTS"} {
		t.Run("validate refuses "+name, func(t *testing.T) {
			cfg := guardTestConfig()
			cfg.Streams[name] = StreamConfig{Subjects: []string{"whatever.>"}}

			err := cfg.Validate()

			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable,
				"--validate must fail on a config that would hard-fail boot")
			assert.Contains(t, err.Error(), name, "diagnostic must name the refused resource")
			assert.Contains(t, err.Error(), "config.streams", "diagnostic must name the declaration source")
		})
	}

	t.Run("diagnostic is deterministic across several offenders", func(t *testing.T) {
		// Map iteration order is random; sorted keys make the reported offender
		// stable so an operator fixing them one at a time sees consistent output.
		first := ""
		for range 20 {
			cfg := guardTestConfig()
			cfg.Streams["KV_AAA"] = StreamConfig{Subjects: []string{"a.>"}}
			cfg.Streams["KV_ZZZ"] = StreamConfig{Subjects: []string{"z.>"}}
			cfg.Streams["OBJ_MMM"] = StreamConfig{Subjects: []string{"m.>"}}

			err := cfg.Validate()
			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
			if first == "" {
				first = err.Error()
				continue
			}
			require.Equal(t, first, err.Error(), "offender reported must not depend on map order")
		}
		assert.Contains(t, first, "KV_AAA", "the sorted-first offender is the one reported")
	})
}

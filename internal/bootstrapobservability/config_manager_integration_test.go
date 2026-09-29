//go:build integration

package bootstrapobservability

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestStartValidatedConfigManagerPropagatesForeignPlatformIdentityMismatch(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Since #1188 a foreign deployment names its own bucket, so a foreign
	// identity reaches this pair's bucket only through an alias or by hand:
	// seed it directly into the bucket the local configuration names.
	bucketName, err := config.BucketName("local", "candidate")
	require.NoError(t, err)
	bucket, err := testClient.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: bucketName, History: 5})
	require.NoError(t, err)
	_, err = bucket.Create(ctx, "platform_identity", []byte(`{"org":"foreign","stem":"existing","id":"existing-0a1b2c"}`))
	require.NoError(t, err)

	local := &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "local", ID: "candidate", Type: "test"},
		Services: make(types.ServiceConfigs),
	}
	manager, effective, err := StartValidatedConfigManager(ctx, local, testClient.Client, logger)
	require.Nil(t, manager)
	require.Nil(t, effective)
	require.ErrorContains(t, err, "start config manager: config bucket platform identity mismatch")
	require.ErrorContains(t, err, `declares org="local" platform.id="candidate"`)
	// The mismatch is decided against the durable platform_identity record, not
	// against the mutable `platform` config key.
	require.ErrorContains(t, err, `records org="foreign" stem="existing"`)
}

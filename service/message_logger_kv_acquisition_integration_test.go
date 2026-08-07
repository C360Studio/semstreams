//go:build integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegration_MessageLoggerReadsExistingKVBucketsWithoutCreating(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	frameworkKV, err := graph.EnsureCatalogBucket(ctx, client, graph.BucketGraphStatus)
	require.NoError(t, err)

	const productBucket = "MESSAGE_LOGGER_PRODUCT_FIXTURE"
	productKV, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:  productBucket,
		History: 1,
	})
	require.NoError(t, err)

	fixtures := []struct {
		name   string
		bucket string
		kv     jetstream.KeyValue
		key    string
		value  []byte
	}{
		{
			name:   "framework bucket",
			bucket: graph.BucketGraphStatus,
			kv:     frameworkKV,
			key:    "message_logger.framework",
			value:  []byte(`{"source":"framework"}`),
		},
		{
			name:   "product bucket",
			bucket: productBucket,
			kv:     productKV,
			key:    "message_logger.product",
			value:  []byte(`{"source":"product"}`),
		},
	}

	ml, err := NewMessageLogger(nil, client)
	require.NoError(t, err)

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			_, err := fixture.kv.Put(ctx, fixture.key, fixture.value)
			require.NoError(t, err)

			req := httptest.NewRequestWithContext(
				ctx,
				http.MethodGet,
				"/message-logger/kv/"+fixture.bucket+"?pattern="+fixture.key,
				nil,
			)
			rec := httptest.NewRecorder()
			ml.handleKVQuery(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var result struct {
				Bucket  string `json:"bucket"`
				Pattern string `json:"pattern"`
				Count   int    `json:"count"`
				Entries []struct {
					Key   string         `json:"key"`
					Value map[string]any `json:"value"`
				} `json:"entries"`
			}
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
			require.Equal(t, fixture.bucket, result.Bucket)
			require.Equal(t, fixture.key, result.Pattern)
			require.Equal(t, 1, result.Count)
			require.Len(t, result.Entries, 1)
			require.Equal(t, fixture.key, result.Entries[0].Key)
			var expectedValue map[string]any
			require.NoError(t, json.Unmarshal(fixture.value, &expectedValue))
			require.Equal(t, expectedValue, result.Entries[0].Value)

			watchCtx, cancelWatch := context.WithCancel(ctx)
			defer cancelWatch()
			watchKV, err := ml.getKVBucketForWatch(watchCtx, fixture.bucket)
			require.NoError(t, err)
			events, watcher, err := ml.createKVWatcher(watchCtx, watchKV, fixture.bucket, fixture.key)
			require.NoError(t, err)
			defer func() { _ = watcher.Stop() }()

			var sawValue, sawInitialSync bool
			for !sawValue || !sawInitialSync {
				select {
				case event, ok := <-events:
					require.True(t, ok, "watch closed before initial replay completed")
					switch event.Operation {
					case "initial_sync_complete":
						sawInitialSync = true
					default:
						if event.Key == fixture.key {
							require.Equal(t, fixture.bucket, event.Bucket)
							require.JSONEq(t, string(fixture.value), string(event.Value))
							sawValue = true
						}
					}
				case <-ctx.Done():
					t.Fatalf("watch did not replay existing value and sync marker: %v", ctx.Err())
				}
			}
		})
	}

	const missingBucket = "MESSAGE_LOGGER_MISSING_FIXTURE"
	_, err = client.GetKeyValueBucket(ctx, missingBucket)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: missing-bucket fixture must not exist")

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"/message-logger/kv/"+missingBucket,
		nil,
	)
	rec := httptest.NewRecorder()
	ml.handleKVQuery(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	_, err = ml.getKVBucketForWatch(ctx, missingBucket)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound)

	_, err = client.GetKeyValueBucket(ctx, missingBucket)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"query and watch lookup must not create persistent state")
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// recordingKVProvider records every bucket lookup and every bucket creation
// attempt made by the KV query endpoint. Creation attempts are the bug under
// test (gh#611): a read-shaped GET must never create persistent NATS state.
type recordingKVProvider struct {
	getCalls    []string
	createCalls []string
	getErr      error
}

func (p *recordingKVProvider) GetKeyValueBucket(_ context.Context, name string) (jetstream.KeyValue, error) {
	p.getCalls = append(p.getCalls, name)
	return nil, p.getErr
}

// CreateKeyValueBucket is a regression tripwire. The fixed kvBucketProvider
// has no create method, so this is unreachable today; it exists so that if
// creation is ever re-added to the provider interface, this stub satisfies the
// wider interface and the createCalls assertions fail loudly instead of the
// endpoint silently regaining the ability to create buckets.
func (p *recordingKVProvider) CreateKeyValueBucket(
	_ context.Context,
	cfg jetstream.KeyValueConfig,
) (jetstream.KeyValue, error) {
	p.createCalls = append(p.createCalls, cfg.Bucket)
	return nil, errors.New("stub provider: bucket creation attempted")
}

func newKVQueryTestLogger() *MessageLogger {
	return &MessageLogger{logger: slog.Default()}
}

// TestHandleKVQuery_MissingBucketReturns404WithoutCreating pins gh#611.
//
// Before the fix, queryKVBucket called CreateKeyValueBucket with a 7-day TTL,
// so a GET for a bucket that does not exist auto-created a real, TTL'd bucket
// and returned 200. That made the handler's own 404 branch dead code and let a
// read-only debugging query impose lifecycle retention on the live graph.
func TestHandleKVQuery_MissingBucketReturns404WithoutCreating(t *testing.T) {
	ml := newKVQueryTestLogger()
	provider := &recordingKVProvider{getErr: jetstream.ErrBucketNotFound}

	req := httptest.NewRequest(http.MethodGet, "/message-logger/kv/SPATIAL_INDEX", nil)
	rec := httptest.NewRecorder()

	ml.handleKVQueryWith(rec, req, provider)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (404 branch must be reachable); body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}

	if len(provider.createCalls) != 0 {
		t.Errorf("endpoint created bucket(s) %v; a read-shaped GET must never create NATS state",
			provider.createCalls)
	}

	if len(provider.getCalls) != 1 || provider.getCalls[0] != "SPATIAL_INDEX" {
		t.Errorf("bucket lookups = %v, want exactly [SPATIAL_INDEX]", provider.getCalls)
	}
}

// TestRecordMatchesStatus_OptInFailedFilter pins the opt-in status filter (#613): an
// empty filter matches everything (off by default, so the endpoint is unchanged for
// existing callers), a "failed" filter keeps failed EMBEDDING_INDEX records and skips
// generated ones, and a non-object value never matches.
func TestRecordMatchesStatus_OptInFailedFilter(t *testing.T) {
	failed := map[string]any{"entity_id": "e1", "status": "failed", "reason": "connection_refused", "error_msg": "dial: connection refused"}
	generated := map[string]any{"entity_id": "e2", "status": "generated"}

	if !recordMatchesStatus(failed, "") || !recordMatchesStatus(generated, "") {
		t.Fatal("an empty filter must match every record (filter off by default)")
	}
	if !recordMatchesStatus(failed, "failed") {
		t.Error("a failed record must pass status=failed")
	}
	if recordMatchesStatus(generated, "failed") {
		t.Error("a generated record must not pass status=failed")
	}
	if recordMatchesStatus("raw string value", "failed") {
		t.Error("a non-object value must never match a status filter")
	}
}

// TestKVQueryEntry_FailedRecordRoundTrips is the operator-surface discipline for the
// debug enumerate (#613): a failed EMBEDDING_INDEX record surfaced through the endpoint's
// entry shape round-trips through the production JSON codec with its operator-reachable
// fields intact — status, reason, and the raw error_msg — so the per-entity forensic view
// carries what an operator needs, and still passes the status=failed filter.
func TestKVQueryEntry_FailedRecordRoundTrips(t *testing.T) {
	entry := map[string]any{
		"key": "acme.ops.a.b.c.1",
		"value": map[string]any{
			"entity_id": "acme.ops.a.b.c.1",
			"status":    "failed",
			"reason":    "connection_refused",
			"error_msg": "generation failed: dial tcp: connection refused",
		},
		"revision": uint64(7),
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	val, ok := back["value"].(map[string]any)
	if !ok {
		t.Fatal("value must round-trip as an object")
	}
	if val["status"] != "failed" || val["reason"] != "connection_refused" {
		t.Fatalf("operator-reachable fields lost in round-trip: %v", val)
	}
	if val["error_msg"] == "" {
		t.Error("the raw error_msg must survive for forensics")
	}
	if !recordMatchesStatus(val, "failed") {
		t.Error("the round-tripped value must still pass the status=failed filter")
	}
}

// TestHandleKVQuery_LookupFailureReturns500 proves the not-found classification
// is a classification, not a catch-all: a genuine backend failure must not be
// reported to the operator as "bucket not found".
func TestHandleKVQuery_LookupFailureReturns500(t *testing.T) {
	ml := newKVQueryTestLogger()
	provider := &recordingKVProvider{getErr: errors.New("connection refused")}

	req := httptest.NewRequest(http.MethodGet, "/message-logger/kv/ENTITY_STATES", nil)
	rec := httptest.NewRecorder()

	ml.handleKVQueryWith(rec, req, provider)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body: %s",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	if len(provider.createCalls) != 0 {
		t.Errorf("endpoint created bucket(s) %v on a transport failure", provider.createCalls)
	}
}

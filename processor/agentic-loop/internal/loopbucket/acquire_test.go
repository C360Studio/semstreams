package loopbucket

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type authorityStatus struct {
	jetstream.KeyValueStatus
	history int64
	ttl     time.Duration
	info    *jetstream.StreamInfo
}

func (s authorityStatus) History() int64                    { return s.history }
func (s authorityStatus) TTL() time.Duration                { return s.ttl }
func (s authorityStatus) StreamInfo() *jetstream.StreamInfo { return s.info }

type authorityBucket struct {
	jetstream.KeyValue
	status   jetstream.KeyValueStatus
	err      error
	contexts []context.Context
}

func (b *authorityBucket) Status(ctx context.Context) (jetstream.KeyValueStatus, error) {
	b.contexts = append(b.contexts, ctx)
	return b.status, b.err
}

type authorityLookup struct {
	bucket jetstream.KeyValue
	err    error
}
type authorityManager struct {
	jetstream.KeyValueManager
	gets     []authorityLookup
	create   authorityLookup
	getCalls int
	creates  []jetstream.KeyValueConfig
	contexts []context.Context
}

func (m *authorityManager) KeyValue(ctx context.Context, _ string) (jetstream.KeyValue, error) {
	m.contexts = append(m.contexts, ctx)
	m.getCalls++
	if m.getCalls > len(m.gets) {
		return nil, errors.New("unexpected extra lookup")
	}
	r := m.gets[m.getCalls-1]
	return r.bucket, r.err
}
func (m *authorityManager) CreateKeyValue(ctx context.Context, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	m.contexts = append(m.contexts, ctx)
	m.creates = append(m.creates, cfg)
	return m.create.bucket, m.create.err
}

// spec: agentic-loop / Loop-state authority is acquired and observed before loop work
func TestAcquireOwnerTypedPathsAndObservedPolicy(t *testing.T) {
	cause := errors.New("I/O failed")
	for _, row := range []struct {
		name, path            string
		history               int64
		ttl                   time.Duration
		maxBytes              int64
		incomplete            string
		wantErr               bool
		wantGets, wantCreates int
		cause                 error
	}{
		{"existing match", "existing", 10, 24 * time.Hour, 0, "", false, 1, 0, nil},
		{"negative bytes nonbinding", "existing", 10, 24 * time.Hour, -1, "", false, 1, 0, nil},
		{"fresh create", "fresh", 10, 24 * time.Hour, 0, "", false, 1, 1, nil},
		{"typed exists race", "race", 10, 24 * time.Hour, 0, "", false, 2, 1, nil},
		{"lookup failure", "lookup-fail", 10, 24 * time.Hour, 0, "", true, 1, 0, cause},
		{"create failure", "create-fail", 10, 24 * time.Hour, 0, "", true, 1, 1, cause},
		{"race lookup failure", "race-fail", 10, 24 * time.Hour, 0, "", true, 2, 1, cause},
		{"status failure", "status-fail", 10, 24 * time.Hour, 0, "", true, 1, 0, cause},
		{"history drift", "existing", 1, 24 * time.Hour, 0, "", true, 1, 0, nil},
		{"ttl drift", "existing", 10, time.Hour, 0, "", true, 1, 0, nil},
		{"binding bytes", "existing", 10, 24 * time.Hour, 1, "", true, 1, 0, nil},
		{"race winner drift", "race", 1, 24 * time.Hour, 0, "", true, 2, 1, nil},
		{"nil status", "existing", 10, 24 * time.Hour, 0, "status", true, 1, 0, nil},
		{"no backing information", "existing", 10, 24 * time.Hour, 0, "info", true, 1, 0, nil},
		{"no backing capability", "existing", 10, 24 * time.Hour, 0, "capability", true, 1, 0, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			ctx := t.Context()
			status := authorityStatus{history: row.history, ttl: row.ttl, info: &jetstream.StreamInfo{Config: jetstream.StreamConfig{MaxAge: row.ttl, MaxBytes: row.maxBytes}}}
			b := &authorityBucket{status: status}
			switch row.incomplete {
			case "status":
				b.status = nil
			case "info":
				status.info = nil
				b.status = status
			case "capability":
				b.status = struct{ jetstream.KeyValueStatus }{}
			}
			m := &authorityManager{gets: []authorityLookup{{bucket: b}}, create: authorityLookup{bucket: b}}
			switch row.path {
			case "fresh", "create-fail", "race", "race-fail":
				m.gets = []authorityLookup{{err: fmt.Errorf("wrapped absence: %w", jetstream.ErrBucketNotFound)}}
				if row.path == "create-fail" {
					m.create = authorityLookup{err: cause}
				}
				if row.path == "race" || row.path == "race-fail" {
					m.create = authorityLookup{err: fmt.Errorf("wrapped race: %w", jetstream.ErrBucketExists)}
					m.gets = append(m.gets, authorityLookup{bucket: b})
				}
				if row.path == "race-fail" {
					m.gets[1] = authorityLookup{err: cause}
				}
			case "lookup-fail":
				m.gets = []authorityLookup{{err: cause}}
			case "status-fail":
				b.err = cause
			}
			got, err := AcquireOwner(ctx, m, "AUTHORITY")
			if row.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
				if row.cause != nil {
					require.ErrorIs(t, err, row.cause)
				}
			} else {
				require.NoError(t, err)
				require.Same(t, b, got)
			}
			require.Equal(t, row.wantGets, m.getCalls)
			require.Len(t, m.creates, row.wantCreates)
			for _, cfg := range m.creates {
				require.Equal(t, "AUTHORITY", cfg.Bucket)
				require.EqualValues(t, 10, cfg.History)
				require.Equal(t, 24*time.Hour, cfg.TTL)
				require.LessOrEqual(t, cfg.MaxBytes, int64(0))
			}
			for _, observed := range append(m.contexts, b.contexts...) {
				require.Same(t, ctx, observed)
			}
		})
	}
}

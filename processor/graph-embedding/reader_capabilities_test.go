package graphembedding

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

type minimalEntityStatesReader struct{}

func (minimalEntityStatesReader) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, nil
}

func (minimalEntityStatesReader) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}

func (minimalEntityStatesReader) Status(context.Context) (jetstream.KeyValueStatus, error) {
	return nil, nil
}

var (
	_ entityStatesReader = minimalEntityStatesReader{}
	_ interface {
		Get(context.Context, string) (jetstream.KeyValueEntry, error)
		WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
		Status(context.Context) (jetstream.KeyValueStatus, error)
	} = (entityStatesReader)(nil)
)

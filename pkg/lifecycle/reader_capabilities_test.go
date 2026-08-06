package lifecycle

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

type minimalEntityStatesReader struct{}

func (minimalEntityStatesReader) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, nil
}

func (minimalEntityStatesReader) Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}

func (minimalEntityStatesReader) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}

var (
	_ entityStatesReader = minimalEntityStatesReader{}
	_ interface {
		ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error)
		Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
		WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	} = (entityStatesReader)(nil)
)

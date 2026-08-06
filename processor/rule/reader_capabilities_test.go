package rule

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

type minimalEntityStatesWatcher struct{}

func (minimalEntityStatesWatcher) Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}

var (
	_ entityStatesWatcher = minimalEntityStatesWatcher{}
	_ interface {
		Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	} = (entityStatesWatcher)(nil)
)

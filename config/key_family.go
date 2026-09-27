package config

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// ManagerOption configures a Manager at construction.
type ManagerOption func(*Manager)

// WithKeyFamily registers a key family whose owner is not the Manager.
//
// The Manager is the configuration bucket's only acquirer, and the bucket's
// name is derived from the deployment's declared authority pair, which only
// the Manager holds. A second writer therefore cannot find the bucket on its
// own. It registers its key family here and reaches the bucket through the
// family instead. Owner ruling on #1188, 2026-09-27 (Q1 (d)).
func WithKeyFamily(family *KeyFamily) ManagerOption {
	return func(cm *Manager) {
		cm.families = append(cm.families, family)
	}
}

// KeyFamilyOperation says what happened to one family entry.
type KeyFamilyOperation uint8

const (
	// KeyFamilyPut is a created or updated entry.
	KeyFamilyPut KeyFamilyOperation = iota + 1
	// KeyFamilyDelete is a deleted or purged entry.
	KeyFamilyDelete
)

// KeyFamilyEntry is one entry of a key family, delivered to its handler.
type KeyFamilyEntry struct {
	// Name is the member name: the key with "<prefix>." removed.
	Name string
	// Value is the entry's value. It is empty for a delete.
	Value []byte
	// Operation says whether the entry was put or deleted.
	Operation KeyFamilyOperation
	// Initial is true for an entry that was present when the watch opened.
	Initial bool
}

// errFamilyNotRegistered is returned by a family used before any Manager's
// Start bound it.
var errFamilyNotRegistered = errors.New(
	"key family has no configuration bucket yet: it is bound by the Start of the config manager it was registered with")

// KeyFamily is the family of keys "<prefix>.<name>" in the configuration
// bucket, together with the handler that receives the family's entries.
//
// Its reads and writes are scoped to the family's "<prefix>." keys, and
// NewConfigManager refuses a family whose prefix is one of the Manager's own
// key prefixes, so a holder cannot reach the identity record or any other key
// the Manager owns, and never learns the bucket's name. They return the not-acquired error until a Manager that
// registered the family has started successfully.
type KeyFamily struct {
	prefix string
	handle func(KeyFamilyEntry)

	mu    sync.RWMutex
	store *natsclient.KVStore
}

// NewKeyFamily builds a family for the keys "<prefix>.<name>".
//
// prefix and every member name are each one NATS KV literal token
// (natsclient.ValidateKVLiteralToken): the family's watch is "<prefix>.*",
// which matches exactly one token after the prefix, so a member name holding a
// "." would be stored but never delivered.
//
// handle runs on the Manager's watch goroutine, in delivery order, and MUST
// NOT block: the Manager's Stop waits for that goroutine. A handler that has
// real work to do should signal a goroutine it owns.
func NewKeyFamily(prefix string, handle func(KeyFamilyEntry)) (*KeyFamily, error) {
	if err := natsclient.ValidateKVLiteralToken(prefix); err != nil {
		return nil, errs.WrapInvalid(err, "KeyFamily", "NewKeyFamily",
			fmt.Sprintf("key family prefix %q must be one KV literal token", prefix))
	}
	if handle == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "KeyFamily", "NewKeyFamily",
			"key family handler cannot be nil")
	}
	return &KeyFamily{prefix: prefix, handle: handle}, nil
}

// pattern is the watch pattern for the family's single-token members.
func (f *KeyFamily) pattern() string { return f.prefix + ".*" }

// key validates one member name and returns its complete key. A name is a
// member only if it is one KV literal token, the set pattern() watches.
func (f *KeyFamily) key(name string) (string, error) {
	if err := natsclient.ValidateKVLiteralToken(name); err != nil {
		return "", errs.WrapInvalid(err, "KeyFamily", "key",
			fmt.Sprintf("member name %q must be one KV literal token", name))
	}
	key := f.prefix + "." + name
	if err := natsclient.ValidateKVLiteralKey(key); err != nil {
		return "", errs.WrapInvalid(err, "KeyFamily", "key", fmt.Sprintf("member key %q is not a KV literal key", key))
	}
	return key, nil
}

// requireContext refuses a nil context at an exported boundary, before any
// store access: KVStore derives its operation timeout from the context and
// panics on nil.
func requireContext(ctx context.Context, method string) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "KeyFamily", method, "context cannot be nil")
	}
	return nil
}

// member validates the context and the name, then returns the bound store and
// the member's key. Validation precedes the binding check so an invalid call
// is refused the same way before and after Start.
func (f *KeyFamily) member(ctx context.Context, method, name string) (*natsclient.KVStore, string, error) {
	if err := requireContext(ctx, method); err != nil {
		return nil, "", err
	}
	key, err := f.key(name)
	if err != nil {
		return nil, "", err
	}
	store, err := f.bound()
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

func (f *KeyFamily) bind(store *natsclient.KVStore) {
	f.mu.Lock()
	f.store = store
	f.mu.Unlock()
}

func (f *KeyFamily) bound() (*natsclient.KVStore, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.store == nil {
		return nil, errFamilyNotRegistered
	}
	return f.store, nil
}

// Get returns the value of one member, or natsclient.ErrKVKeyNotFound.
func (f *KeyFamily) Get(ctx context.Context, name string) ([]byte, error) {
	store, key, err := f.member(ctx, "Get", name)
	if err != nil {
		return nil, err
	}
	entry, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return entry.Value, nil
}

// Put writes one member unconditionally.
func (f *KeyFamily) Put(ctx context.Context, name string, value []byte) error {
	store, key, err := f.member(ctx, "Put", name)
	if err != nil {
		return err
	}
	_, err = store.Put(ctx, key, value)
	return err
}

// Create writes one member only if it is absent, or returns
// natsclient.ErrKVKeyExists.
func (f *KeyFamily) Create(ctx context.Context, name string, value []byte) error {
	store, key, err := f.member(ctx, "Create", name)
	if err != nil {
		return err
	}
	_, err = store.Create(ctx, key, value)
	return err
}

// Delete removes one member.
func (f *KeyFamily) Delete(ctx context.Context, name string) error {
	store, key, err := f.member(ctx, "Delete", name)
	if err != nil {
		return err
	}
	return store.Delete(ctx, key)
}

// Names lists the family's current member names. A key under "<prefix>."
// with more than one token after it is not a member, because the family's
// watch never delivers it, so Names omits it too.
func (f *KeyFamily) Names(ctx context.Context) ([]string, error) {
	if err := requireContext(ctx, "Names"); err != nil {
		return nil, err
	}
	store, err := f.bound()
	if err != nil {
		return nil, err
	}
	keys, err := store.Keys(ctx)
	if err != nil {
		return nil, err
	}
	prefix := f.prefix + "."
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		if name, ok := strings.CutPrefix(key, prefix); ok && natsclient.ValidateKVLiteralToken(name) == nil {
			names = append(names, name)
		}
	}
	return names, nil
}

// processFamily delivers one family's watch to its handler until shutdown.
// Entries before the watch's nil marker are the snapshot present when it
// opened.
func (cm *Manager) processFamily(ctx context.Context, family *KeyFamily, watcher jetstream.KeyWatcher) {
	defer cm.wg.Done()
	prefix := family.prefix + "."
	initial := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-cm.shutdownCh:
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if entry == nil {
				initial = false
				continue
			}
			if cm.stopped.Load() {
				return
			}
			delivered := KeyFamilyEntry{
				Name:      strings.TrimPrefix(entry.Key(), prefix),
				Operation: KeyFamilyPut,
				Initial:   initial,
			}
			switch entry.Operation() {
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				delivered.Operation = KeyFamilyDelete
			default:
				delivered.Value = entry.Value()
			}
			family.handle(delivered)
		}
	}
}

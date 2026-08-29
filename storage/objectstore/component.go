// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// objectstoreSchema defines the configuration schema for ObjectStore component
// Generated from Config struct tags using reflection
var objectstoreSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component wraps ObjectStore as a component with NATS ports
//
// Composition-Friendly Design:
//   - Generic NATS port handling (no semantic requirements)
//   - Publishes simple storage events (not semantic messages)
//   - Allows SemStreams to wrap/extend for semantic behavior
type Component struct {
	// Component metadata
	instanceName string
	enabled      bool

	// Mutex to protect concurrent access to state fields
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	started        bool

	// core dependencies
	store           *Store
	decoder         *message.Decoder
	natsClient      *natsclient.Client
	metricsRegistry *metric.MetricsRegistry
	config          Config
	logger          *slog.Logger
	inputPorts      []component.Port
	outputPorts     []component.Port
	portsByName     map[string]component.Port
	portKinds       map[string]component.PortKind
	portSubjects    map[string]string
	inputBindings   []objectStoreInputBinding

	// Active write-input bindings. The component owns every exact native handle
	// and keeps callback authority and the Store live until those handles close.
	writeSubs      []objectStoreCoreSubscription
	writeConsumers []objectStoreConsumerBinding

	newStore           func(context.Context) (*Store, error)
	closeStore         func(*Store) error
	subscribeCore      func(context.Context, string, func(context.Context, *nats.Msg)) (objectStoreCoreSubscription, error)
	waitForStreamInput func(context.Context, string) error
	consumeStream      func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitConsumerClosed func(context.Context, <-chan struct{}) error

	// Metrics tracking
	messagesReceived uint64
	messagesStored   uint64
	lastActivity     atomic.Value // stores time.Time
}

type objectStoreConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

type objectStoreCoreSubscription interface {
	Drain(context.Context) error
}

type objectStoreInputBinding struct {
	portName     string
	kind         component.PortKind
	subject      string
	streamName   string
	consumerName string
}

// Event represents a simple storage event published by ObjectStore
// core design: Just indicates what happened, no semantic payload
type Event struct {
	Type      string         `json:"type"` // "stored", "retrieved"
	Key       string         `json:"key"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Ensure Component implements required interfaces
var _ component.Discoverable = (*Component)(nil)
var _ component.LifecycleComponent = (*Component)(nil)
var _ component.StoreProvider = (*Component)(nil)

// ProvidedStores exposes this component's live store to the ComponentManager for
// registration in the shared StoreRegistry (ADR-063). Keyed by the store's
// stamped StorageInstance (store.InstanceName()) — the same value it writes into
// every StorageReference, so consumers resolving a ref land on this store.
// Returns nil before Start (no store yet).
func (c *Component) ProvidedStores() map[string]storage.StreamableStore {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return nil
	}
	return map[string]storage.StreamableStore{c.store.InstanceName(): c.store}
}

// Initialize sets up the component (no I/O operations)
func (c *Component) Initialize() error {
	// No initialization needed - all setup happens in Start
	return nil
}

// constructedInstanceName is the instance name NewComponent stamps into the
// store-provide port and every StorageReference it produces. The factory
// signature carries no instance name, so this literal stands in for it; the
// declarer below mirrors it so admission parity holds. Whether the real
// instance name should reach the constructor (changing the store-provide
// resource identity at runtime) is an owner question recorded as a `[~]` in
// openspec/changes/composition-validation-substrate/tasks.md (3.1) and
// inventory §12.2 — neither side is codified here.
const constructedInstanceName = "objectstore"

// DeclarePorts is the component.PortDeclarer for objectstore: the configured
// NATS/JetStream ports plus the derived store-provide output, exactly as
// NewComponent will report them. The instanceName parameter is deliberately
// not used — see constructedInstanceName.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	cfg, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	inputPorts, outputPorts, _, _, _, err := resolveObjectStorePorts(cfg, constructedInstanceName)
	if err != nil {
		return component.PortConfig{}, errs.WrapInvalid(err, "Component", "NewComponent", "resolve ports")
	}
	return component.PortConfigFrom(inputPorts, outputPorts), nil
}

// resolveConfig overlays the user configuration on the defaults. It is the one
// derivation DeclarePorts and NewComponent share; resolveObjectStorePorts is
// the one port derivation.
func resolveConfig(rawConfig json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	if len(rawConfig) > 0 {
		var userConfig Config
		if err := json.Unmarshal(rawConfig, &userConfig); err != nil {
			return Config{}, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}

		// Apply user overrides
		if userConfig.Ports != nil {
			cfg.Ports = userConfig.Ports
		}
		if userConfig.BucketName != "" {
			cfg.BucketName = userConfig.BucketName
		}
		if userConfig.DataCache.Enabled || userConfig.DataCache.MaxSize > 0 {
			cfg.DataCache = userConfig.DataCache
		}
		// Copy over pluggable generators
		if userConfig.KeyGenerator != nil {
			cfg.KeyGenerator = userConfig.KeyGenerator
		}
		if userConfig.MetadataExtractor != nil {
			cfg.MetadataExtractor = userConfig.MetadataExtractor
		}
	}
	return cfg, nil
}

// NewComponent creates a new ObjectStore component factory
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	cfg, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	instanceName := constructedInstanceName
	inputPorts, outputPorts, portsByName, portKinds, portSubjects, err := resolveObjectStorePorts(cfg, instanceName)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve ports")
	}
	inputBindings, err := planObjectStoreInputBindings(instanceName, inputPorts, portKinds, portSubjects)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "plan input bindings")
	}

	return &Component{
		instanceName:    instanceName,
		enabled:         true,
		config:          cfg,
		decoder:         message.NewDecoder(deps.PayloadRegistry),
		natsClient:      deps.NATSClient,
		metricsRegistry: deps.MetricsRegistry,
		logger:          deps.GetLogger(),
		inputPorts:      inputPorts,
		outputPorts:     outputPorts,
		portsByName:     portsByName,
		portKinds:       portKinds,
		portSubjects:    portSubjects,
		inputBindings:   inputBindings,
	}, nil
}

const objectStoreConsumerHashDomain = "objectstore-js-consumer-v1"

func planObjectStoreInputBindings(
	instanceName string,
	inputs []component.Port,
	kinds map[string]component.PortKind,
	subjects map[string]string,
) ([]objectStoreInputBinding, error) {
	plans := make([]objectStoreInputBinding, len(inputs))
	plansByPort := make(map[string]objectStoreInputBinding, len(inputs))
	duplicatePorts := make(map[string][]string)
	collisionGroups := make(map[string][]int)

	for i, input := range inputs {
		plan := objectStoreInputBinding{
			portName: input.Name,
			kind:     kinds[input.Name],
			subject:  subjects[input.Name],
		}
		switch plan.kind {
		case component.PortKindNATS:
			duplicatePorts["nats\x00"+plan.subject] = append(
				duplicatePorts["nats\x00"+plan.subject], plan.portName)
		case component.PortKindJetStream:
			facts, err := input.Facts()
			if err != nil {
				return nil, fmt.Errorf("project input port %q facts: %w", plan.portName, err)
			}
			stream, ok := facts.Stream()
			if !ok {
				return nil, fmt.Errorf("input port %q has no JetStream declaration", plan.portName)
			}
			plan.streamName = stream.Name()
			duplicateKey := "jetstream\x00" + plan.streamName + "\x00" + plan.subject
			duplicatePorts[duplicateKey] = append(duplicatePorts[duplicateKey], plan.portName)
			plan.consumerName = legacyObjectStoreConsumerName(instanceName, plan.subject)
			collisionKey := plan.streamName + "\x00" + plan.consumerName
			collisionGroups[collisionKey] = append(collisionGroups[collisionKey], i)
		default:
			return nil, fmt.Errorf("input port %q has unsupported kind %q", plan.portName, plan.kind)
		}
		plans[i] = plan
		plansByPort[plan.portName] = plan
	}

	duplicateKeys := make([]string, 0)
	for key, ports := range duplicatePorts {
		if len(ports) > 1 {
			duplicateKeys = append(duplicateKeys, key)
		}
	}
	sort.Strings(duplicateKeys)
	if len(duplicateKeys) > 0 {
		ports := duplicatePorts[duplicateKeys[0]]
		sort.Strings(ports)
		plan := plansByPort[ports[0]]
		if plan.kind == component.PortKindJetStream {
			return nil, fmt.Errorf(
				"duplicate ObjectStore JetStream binding stream=%q subject=%q declared by ports %q",
				plan.streamName, plan.subject, ports)
		}
		return nil, fmt.Errorf(
			"duplicate ObjectStore NATS binding subject=%q declared by ports %q", plan.subject, ports)
	}

	for _, indexes := range collisionGroups {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes {
			plan := &plans[index]
			canonical := objectStoreConsumerHashDomain + "\x00" + instanceName + "\x00" +
				plan.streamName + "\x00" + plan.subject
			digest := sha256.Sum256([]byte(canonical))
			plan.consumerName = "objectstore-h-" + fmt.Sprintf("%x", digest[:])
		}
	}

	consumerOwners := make(map[string]string)
	for _, plan := range plans {
		if plan.kind != component.PortKindJetStream {
			continue
		}
		if err := validateObjectStoreConsumerName(plan.consumerName); err != nil {
			return nil, fmt.Errorf("input port %q planned invalid consumer name %q: %w",
				plan.portName, plan.consumerName, err)
		}
		key := plan.streamName + "\x00" + plan.consumerName
		if owner, duplicate := consumerOwners[key]; duplicate {
			ports := []string{owner, plan.portName}
			sort.Strings(ports)
			return nil, fmt.Errorf(
				"ObjectStore JetStream consumer identity collision stream=%q consumer=%q ports=%q",
				plan.streamName, plan.consumerName, ports)
		}
		consumerOwners[key] = plan.portName
	}
	return plans, nil
}

func legacyObjectStoreConsumerName(instanceName, subject string) string {
	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	return fmt.Sprintf("objectstore-%s-%s", instanceName, sanitizedSubject)
}

func validateObjectStoreConsumerName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 255 {
		return fmt.Errorf("name is %d bytes, maximum is 255", len(name))
	}
	if strings.ContainsAny(name, ">*. /\\\t\r\n") {
		return errors.New("name contains a character forbidden by NATS")
	}
	return nil
}

func resolveObjectStorePorts(cfg Config, instanceName string) ([]component.Port, []component.Port, map[string]component.Port, map[string]component.PortKind, map[string]string, error) {
	if cfg.Ports == nil {
		return nil, nil, nil, nil, nil, errors.New("ports configuration is required")
	}
	byName := make(map[string]component.Port, len(cfg.Ports.Inputs)+len(cfg.Ports.Outputs)+1)
	kinds := make(map[string]component.PortKind, len(byName))
	subjects := make(map[string]string, len(byName))
	resolve := func(definitions []component.PortDefinition, direction component.Direction, allowed map[component.PortKind]bool) ([]component.Port, error) {
		ports := make([]component.Port, len(definitions))
		for index, definition := range definitions {
			if direction == component.DirectionInput && definition.Name == "api" {
				return nil, errors.New("ObjectStore input \"api\" was removed; use the registered Store")
			}
			port, err := definition.Resolve(direction)
			if err != nil {
				return nil, err
			}
			if _, duplicate := byName[port.Name]; duplicate {
				return nil, fmt.Errorf("duplicate port name %q", port.Name)
			}
			facts, err := port.Facts()
			if err != nil {
				return nil, err
			}
			if !allowed[facts.Kind()] {
				return nil, fmt.Errorf("port %q kind %q is not supported", port.Name, facts.Kind())
			}
			portSubjects := facts.NATSSubjects()
			if len(portSubjects) != 1 {
				return nil, fmt.Errorf("port %q declares %d subjects, want one", port.Name, len(portSubjects))
			}
			ports[index] = port
			byName[port.Name] = port
			kinds[port.Name] = facts.Kind()
			subjects[port.Name] = portSubjects[0]
		}
		return ports, nil
	}
	inputs, err := resolve(cfg.Ports.Inputs, component.DirectionInput, map[component.PortKind]bool{
		component.PortKindNATS: true, component.PortKindJetStream: true,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	outputs, err := resolve(cfg.Ports.Outputs, component.DirectionOutput, map[component.PortKind]bool{
		component.PortKindNATS: true, component.PortKindJetStream: true,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	provider, err := (component.PortDefinition{
		Name: "store-provide", Description: "Owns the store instance addressable as StorageInstance=" + instanceName,
		Config: component.StoreProvidePort{Instance: instanceName},
	}).Resolve(component.DirectionOutput)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	outputs = append(outputs, provider)
	return inputs, outputs, byName, kinds, subjects, nil
}

// startStoreError classifies a store-constructor error for Component.Start,
// PRESERVING its class. The D2 retention guard (reconcileNoLifecycleRetention,
// #600/#616) returns a FATAL ErrGraphBucketRetention when a content store's backing
// stream keeps lifecycle eviction it cannot strip — that must fail Start CLOSED. Every
// other constructor error stays transient (retryable). errs.IsFatal inspects the
// OUTERMOST classification, so an unconditional WrapTransient here would silently
// downgrade the fatal (the #632 defect); the IsFatal branch is load-bearing. Returns
// nil for a nil error. Extracted so Start's fail-closed decision is directly testable.
func startStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errs.IsFatal(err) {
		return errs.WrapFatal(err, "Component", "Start", "create object store")
	}
	return errs.WrapTransient(err, "Component", "Start", "create object store")
}

// Start initializes the ObjectStore and sets up NATS handlers
func (c *Component) Start(ctx context.Context) (startErr error) {
	// Validate before inspecting lifecycle state.
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}
	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "already started")
	}
	if c.natsClient == nil {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "NATS client is required")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	c.lifecycleUsed, c.cleanupPending = true, true
	c.cancel = cancel
	c.startDone = startDone
	c.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanup)
			startErr = errors.Join(startErr, rollbackErr)
			c.lifecycleMu.Lock()
			if rollbackErr == nil {
				c.cleanupPending = false
				c.terminal = true
				c.clearLifecycleHandles()
			}
			close(startDone)
			c.startDone = nil
			c.lifecycleMu.Unlock()
			return
		}
		c.lifecycleMu.Lock()
		c.cleanupPending = false
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}()

	c.logger.Debug("Creating ObjectStore", "name", c.instanceName, "bucket", c.config.BucketName)

	// gh#400: thread the component instance name into the store so its
	// StoreContent path stamps the SAME StorageInstance as this component's
	// StoredMessage emit path (which already uses c.instanceName). Without this
	// the store would fall back to the bucket name and the two write paths would
	// disagree, leaving a resolver unable to resolve one of them.
	// Thread this component's logger so the D2 retention-reconcile guard (#600)
	// attributes its boot WARN to this store rather than the process default.
	c.config.InstanceName, c.config.Logger = c.instanceName, c.logger

	// Create the underlying ObjectStore with metrics support
	createStore := func(ctx context.Context) (*Store, error) {
		return NewStoreWithConfigAndMetrics(ctx, c.natsClient, c.config, c.metricsRegistry)
	}
	if c.newStore != nil {
		createStore = c.newStore
	}
	store, err := createStore(runCtx)
	if store != nil {
		store.SetDecoder(c.decoder)
		c.mu.Lock()
		c.store = store
		c.mu.Unlock()
	}
	if err != nil {
		c.logger.Error(
			"Failed to create ObjectStore",
			"name",
			c.instanceName,
			"bucket",
			c.config.BucketName,
			"error",
			err,
		)
		return startStoreError(err)
	}

	c.logger.Debug("ObjectStore created successfully", "name", c.instanceName, "bucket", c.config.BucketName)

	// Every declared ordinary input is a write lane. Port names are local graph
	// labels, not operation selectors; interpreting only the literal name
	// "write" left valid renamed inputs (including protocol-flow's store_in)
	// configured and healthy-looking but inert (#848).
	for _, input := range c.inputBindings {
		writeSubject := input.subject
		c.logger.Debug("Subscribing to write subject",
			"name", c.instanceName,
			"port", input.portName,
			"subject", writeSubject)

		if input.kind == component.PortKindJetStream {
			binding, setupErr := c.setupJetStreamConsumer(runCtx, input)
			if setupErr != nil {
				startErr := errs.WrapTransient(setupErr, "Component", "Start",
					fmt.Sprintf("setup JetStream consumer for input %s", input.portName))
				return startErr
			}
			c.writeConsumers = append(c.writeConsumers, binding)
			continue
		}

		subscribe := func(ctx context.Context, subject string, handler func(context.Context, *nats.Msg)) (objectStoreCoreSubscription, error) {
			return c.natsClient.Subscribe(ctx, subject, handler)
		}
		if c.subscribeCore != nil {
			subscribe = c.subscribeCore
		}
		writeSub, subscribeErr := subscribe(runCtx, writeSubject, c.handleWriteRequest)
		if subscribeErr != nil {
			c.logger.Error(
				"Failed to subscribe to write subject",
				"name", c.instanceName,
				"port", input.portName,
				"subject", writeSubject,
				"error", subscribeErr,
			)
			startErr := errs.WrapTransient(subscribeErr, "Component", "Start",
				fmt.Sprintf("subscribe to input %s subject %s", input.portName, writeSubject))
			return startErr
		}
		c.writeSubs = append(c.writeSubs, writeSub)
	}

	// NOTE: Stream creation is handled centrally by config.StreamsManager
	// which derives streams from component port definitions at startup.
	// Components no longer need to create their own streams.

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	committed = true
	c.lastActivity.Store(time.Now())
	c.logger.Debug("ObjectStore component fully started", "name", c.instanceName)

	return nil
}

// Stop cleanly shuts down the component
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed = true
			c.terminal = true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.terminal {
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.startDone != nil {
			startDone := c.startDone
			c.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "Component", "Stop", "concurrent Stop is unsupported")
		}
		retryable := c.cleanupPending
		c.stopping = true
		c.lifecycleMu.Unlock()
		stopErr := c.cleanup(ctx)
		c.lifecycleMu.Lock()
		c.stopping = false
		if retryable && stopErr != nil {
			c.lifecycleMu.Unlock()
			return stopErr
		}
		c.cleanupPending = false
		c.terminal = true
		c.clearLifecycleHandles()
		c.lifecycleMu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanup(ctx context.Context) error {
	var cleanupErr error
	for index := range c.writeConsumers {
		binding := &c.writeConsumers[index]
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
	}
	for _, sub := range c.writeSubs {
		cleanupErr = errors.Join(cleanupErr, sub.Drain(ctx))
	}
	for index := range c.writeConsumers {
		closed := c.writeConsumers[index].handle.Closed()
		if c.waitConsumerClosed != nil {
			cleanupErr = errors.Join(cleanupErr, c.waitConsumerClosed(ctx, closed))
			continue
		}
		select {
		case <-closed:
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
		}
	}
	if c.cancel != nil {
		c.cancel()
	}
	cleanupErr = errors.Join(cleanupErr, ctx.Err())

	c.mu.Lock()
	store := c.store
	c.mu.Unlock()
	if cleanupErr == nil && store != nil {
		closeStore := func(store *Store) error { return store.Close() }
		if c.closeStore != nil {
			closeStore = c.closeStore
		}
		cleanupErr = errors.Join(cleanupErr, closeStore(store))
	}
	if cleanupErr != nil {
		return errs.WrapTransient(cleanupErr, "Component", "Stop", "stop write inputs and close Store")
	}
	return nil
}

func (c *Component) clearLifecycleHandles() {
	c.cancel = nil
	c.writeSubs = nil
	c.writeConsumers = nil
	c.mu.Lock()
	c.store = nil
	c.started = false
	c.mu.Unlock()
}

// IsStarted returns whether the component is running
func (c *Component) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// handleWriteRequest handles async write operations via core NATS
// Stores message and emits StoredMessage with StorageRef for downstream processors.
// Core NATS has no ack/redelivery semantics, so a failed write can only be
// surfaced — logged once here (the shared processWriteMessage returns errors
// rather than logging, per the return-vs-log convention).
func (c *Component) handleWriteRequest(ctx context.Context, msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	if err := c.processWriteMessage(ctx, msg.Data); err != nil {
		c.logger.Error("Failed to process write message",
			slog.String("subject", msg.Subject),
			slog.String("error", err.Error()))
	}
}

// emitStoredMessage emits a StoredMessage with StorageRef for downstream
// semantic processing, from the decode result processWriteMessage already
// holds (a single decode feeds both the storage key and this emit, so the
// two can never disagree about what the message is — #741).
//
// Returns nil for by-design skips (no "stored" port configured, message not a
// BaseMessage, payload not Graphable) — those messages never owe downstream a
// StorageReference. Returns a classified error when a REQUIRED publication
// fails (marshal or publish): the "stored" port is configured and the payload
// qualifies, so downstream losing the reference is the same loss shape as a
// failed store — the caller must not ack the delivery (#727). Errors are
// returned, not logged here; the transport caller logs once.
func (c *Component) emitStoredMessage(
	ctx context.Context, baseMsg *message.BaseMessage, decodeErr error, data []byte, storageKey string,
) error {
	if !c.hasPort("stored") {
		return nil // No stored output port configured
	}

	if decodeErr != nil {
		c.logger.Debug("Message not a BaseMessage, skipping StoredMessage emit",
			slog.String("error", decodeErr.Error()))
		return nil
	}

	// Extract Graphable payload
	payload := baseMsg.Payload()
	graphable, ok := payload.(graph.Graphable)
	if !ok {
		c.logger.Debug("Payload not Graphable, skipping StoredMessage emit",
			slog.String("payload_type", fmt.Sprintf("%T", payload)))
		return nil
	}

	// Create StorageReference
	storageRef := &message.StorageReference{
		StorageInstance: c.instanceName,
		Key:             storageKey,
		ContentType:     "application/json",
		Size:            int64(len(data)),
	}

	if err := c.publishStoredMessage(ctx, graphable, storageRef, baseMsg.Type().Key()); err != nil {
		return err
	}

	c.logger.Debug("Emitted StoredMessage",
		slog.String("entity_id", graphable.EntityID()),
		slog.String("storage_key", storageKey))
	return nil
}

// emitStoredMessageFromContentStorable emits a StoredMessage for ContentStorable payloads
// This is used when we've already stored via StoreContent and have a proper StorageRef.
//
// Same contract as emitStoredMessage: nil for by-design skips (no "stored"
// port, ContentStorable not Graphable), a classified error when the required
// publication fails so the caller can refuse the ack (#727).
func (c *Component) emitStoredMessageFromContentStorable(
	ctx context.Context,
	baseMsg *message.BaseMessage,
	cs message.ContentStorable,
	storageRef *message.StorageReference,
) error {
	if !c.hasPort("stored") {
		return nil
	}

	// ContentStorable must also be Graphable for downstream processing
	graphable, ok := cs.(graph.Graphable)
	if !ok {
		c.logger.Debug("ContentStorable not Graphable, skipping StoredMessage emit",
			slog.String("entity_id", cs.EntityID()))
		return nil
	}

	if err := c.publishStoredMessage(ctx, graphable, storageRef, baseMsg.Type().Key()); err != nil {
		return err
	}

	c.logger.Debug("Emitted StoredMessage for ContentStorable",
		slog.String("entity_id", cs.EntityID()),
		slog.String("storage_key", storageRef.Key))
	return nil
}

// publishStoredMessage wraps a Graphable + StorageReference in a StoredMessage
// envelope and publishes it on the "stored" port. Shared tail of the two emit
// paths. Marshal failure is Invalid (retrying the same message cannot fix it);
// publish failure is Transient (broker or stream unavailability).
func (c *Component) publishStoredMessage(
	ctx context.Context,
	graphable graph.Graphable,
	storageRef *message.StorageReference,
	originalType string,
) error {
	storedMsg := NewStoredMessage(graphable, storageRef, originalType)

	// Wrap in BaseMessage for transport
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(),
		storedMsg,
		c.instanceName, // source
	)

	msgData, err := wrappedMsg.MarshalJSON()
	if err != nil {
		return errs.WrapInvalid(err, "Component", "publishStoredMessage", "marshal StoredMessage")
	}

	storedSubject := c.getPortSubject("stored")

	// Use JetStream publishing when port type is "jetstream" for durability
	if c.isJetStreamPort("stored") {
		if err := c.natsClient.PublishToStream(ctx, storedSubject, msgData); err != nil {
			return errs.WrapTransient(err, "Component", "publishStoredMessage",
				fmt.Sprintf("publish StoredMessage to JetStream subject %s", storedSubject))
		}
		return nil
	}
	// Fallback to core NATS for non-JetStream ports
	if err := c.natsClient.Publish(ctx, storedSubject, msgData); err != nil {
		return errs.WrapTransient(err, "Component", "publishStoredMessage",
			fmt.Sprintf("publish StoredMessage to subject %s", storedSubject))
	}
	return nil
}

// publishEvent publishes a simple storage event to the events subject
func (c *Component) publishEvent(event Event) {
	if !c.hasPort("events") {
		return // No events port configured
	}

	eventSubject := c.getPortSubject("events")
	data, err := json.Marshal(event)
	if err != nil {
		c.logger.Error("Failed to marshal event",
			slog.String("error", err.Error()))
		return
	}

	if err := c.natsClient.GetConnection().Publish(eventSubject, data); err != nil {
		c.logger.Error("Failed to publish event",
			slog.String("subject", eventSubject),
			slog.String("error", err.Error()))
		return
	}
}

// hasPort checks if a port with the given name is configured
func (c *Component) hasPort(name string) bool {
	_, found := c.portsByName[name]
	return found
}

// isJetStreamPort checks if an output port is configured for JetStream
func (c *Component) isJetStreamPort(portName string) bool {
	return c.portKinds[portName] == component.PortKindJetStream
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(
	ctx context.Context,
	binding objectStoreInputBinding,
) (objectStoreConsumerBinding, error) {
	port, found := c.portsByName[binding.portName]
	if !found || port.Direction != component.DirectionInput {
		return objectStoreConsumerBinding{}, errs.WrapInvalid(
			errs.ErrInvalidConfig, "Component", "setupJetStreamConsumer",
			fmt.Sprintf("port %s not found", binding.portName))
	}
	streamName := binding.streamName
	subject := binding.subject

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return objectStoreConsumerBinding{}, errs.WrapTransient(
			err, "Component", "setupJetStreamConsumer", fmt.Sprintf("stream %s not available", streamName))
	}

	consumerName := binding.consumerName

	c.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration).
	// objectstore is idempotent (content-addressed storage overwrites), so it
	// defaults to "all": it MUST catch up on messages published before its
	// consumer bound. A JSON config that omits deliver_policy would otherwise
	// fall to the framework "new" default and silently drop the first document
	// (the startup first-message race). An explicit deliver_policy still wins.
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return objectStoreConsumerBinding{}, errs.WrapInvalid(
			consumerErr, "ObjectStoreComponent", "setupJetStreamConsumer", "resolve consumer config")
	}
	facts, factsErr := port.Facts()
	if factsErr != nil {
		return objectStoreConsumerBinding{}, errs.WrapInvalid(
			factsErr, "ObjectStoreComponent", "setupJetStreamConsumer", "project input port facts")
	}
	stream, ok := facts.Stream()
	if !ok {
		return objectStoreConsumerBinding{}, errs.WrapInvalid(
			errs.ErrInvalidConfig, "ObjectStoreComponent", "setupJetStreamConsumer", "missing stream facts")
	}
	if stream.DeliverPolicy() == "" {
		consumerCfg.DeliverPolicy = "all"
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending,
		AutoCreate:    false,
	}

	consume := c.natsClient.ConsumeStreamWithConfig
	if c.consumeStream != nil {
		consume = c.consumeStream
	}
	handle, consumeErr := consume(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleJetStreamWriteRequest(msgCtx, msg)
	})
	if consumeErr != nil {
		return objectStoreConsumerBinding{}, errs.WrapTransient(
			consumeErr, "Component", "setupJetStreamConsumer", fmt.Sprintf("consumer setup failed for stream %s", streamName))
	}

	return objectStoreConsumerBinding{handle: handle}, nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	if c.waitForStreamInput != nil {
		return c.waitForStreamInput(ctx, streamName)
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Component", "waitForStream", "get JetStream context")
	}

	// Retry with backoff
	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			c.logger.Debug("Stream available", "stream", streamName)
			return nil
		}

		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}

	return errs.WrapTransient(errs.ErrStorageUnavailable, "Component", "waitForStream", fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
}

// Nak delays mirror the documented heartbeat-settlement precedent: 30s breathing room
// before retrying failed work, 5s for graceful shutdown/cancellation. They are
// deliberately NOT operator-configurable (#727).
const (
	transientNakDelay = 30 * time.Second
	shutdownNakDelay  = 5 * time.Second
)

// writeDisposition is the ack decision for one JetStream write delivery.
type writeDisposition int

const (
	dispositionAck          writeDisposition = iota // positive ack: work fully committed
	dispositionNakShutdown                          // NakWithDelay(shutdownNakDelay): cancelled mid-flight
	dispositionTerm                                 // Term: structurally invalid, retry can never succeed
	dispositionNakTransient                         // NakWithDelay(transientNakDelay): transient or unclassified
)

// classifyWriteDisposition maps a processWriteMessage outcome onto the ack
// decision table:
//
//	nil error                  -> Ack
//	ctx cancelled + any error  -> NakWithDelay(5s)  (graceful shutdown)
//	errs.IsInvalid             -> Term              (poison message, do not retry)
//	anything else              -> NakWithDelay(30s) (transient/unclassified: retry)
//
// The delays and the Term/NAK split follow the heartbeat-settlement precedent,
// with one DELIBERATE divergence: heartbeat lets cancellation own the outcome
// even when work returned nil (NAK -> redeliver -> duplicate), because its
// select can race a success report against ctx.Done(). Here a nil error means
// the store commit and any required emit fully completed, so nil Acks even
// under a cancelled context — NAKing completed work would guarantee a
// duplicate object on redelivery for zero durability gain.
//
// For NON-nil errors, cancellation is checked BEFORE the invalid class
// (heartbeat precedent: cancellation owns the delivery outcome;
// select-race-on-pre-cancelled-ctx discipline): an error observed under a
// cancelled context may be the cancellation itself surfacing through the
// store, and terminating it would permanently drop a retryable message during
// shutdown. Unclassified errors deliberately fall to the transient NAK —
// retrying is the safe default; terminating loses data.
func classifyWriteDisposition(ctxErr, procErr error) writeDisposition {
	if procErr == nil {
		return dispositionAck
	}
	if ctxErr != nil {
		return dispositionNakShutdown
	}
	if errs.IsInvalid(procErr) {
		return dispositionTerm
	}
	return dispositionNakTransient
}

// handleJetStreamWriteRequest handles JetStream messages for write operations.
//
// Delivery contract: at-least-once. The delivery is positively acked ONLY after
// the store commit AND any required StorageReference publication both succeed;
// failures NAK (transient) or Term (structurally invalid) per
// classifyWriteDisposition. Redelivery after a partial failure re-runs the
// store. On the raw lane, DefaultKeyGenerator keys every write with a
// per-write UUID nonce (#741), so a redelivered raw message ALWAYS stores a
// NEW object rather than overwriting — a visible duplicate; duplicates beat
// loss (#727). (Before #741 the seconds-granularity key suffix made
// same-second redelivery an idempotent overwrite only by the same collision
// that silently LOST distinct same-second messages.) On the ContentStorable
// lane, Store.generateContentKey still keys with UnixNano: a redelivery
// landing on the same clock reading overwrites rather than duplicates — but
// that key embeds the entity ID, so the casualty is the SAME entity's
// just-written content with identical bytes (an idempotent overwrite), never
// a distinct message.
//
// Retry is BOUNDED, not indefinite: the write consumer's canonical port
// declaration defaults MaxDeliver to 3, which caps delivery
// attempts — roughly 60s of backend-outage tolerance at the 30s transient NAK
// delay — after which the message parks un-acked awaiting operator action;
// parked-message visibility is tracked as the MaxDeliver parking follow-up.
func (c *Component) handleJetStreamWriteRequest(ctx context.Context, msg jetstream.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	c.settleJetStreamWrite(ctx, msg, c.processWriteMessage(ctx, msg.Data()))
}

// settleJetStreamWrite applies the ack decision for one delivery and logs the
// processing error exactly once (processWriteMessage returns errors rather
// than logging, per the return-vs-log convention). Split from
// handleJetStreamWriteRequest so the decision table is drivable with a fake
// jetstream.Msg in unit tests.
func (c *Component) settleJetStreamWrite(ctx context.Context, msg jetstream.Msg, procErr error) {
	switch classifyWriteDisposition(ctx.Err(), procErr) {
	case dispositionAck:
		if err := msg.Ack(); err != nil {
			c.logger.Error("Failed to ack JetStream message",
				slog.String("error", err.Error()))
		}
	case dispositionNakShutdown:
		// Same NAK verb for both cancellation causes, different visibility:
		// DeadlineExceeded here is the per-message processing deadline
		// (natsclient's messageHandlerContext default, 30s) expiring over a
		// hung backend — a real failure class that can silently park the
		// message once MaxDeliver exhausts, so it must be loud. Canceled is
		// genuine shutdown/restart noise and stays Debug.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.logger.Error("Write processing deadline exceeded; NAK for redelivery",
				slog.String("subject", msg.Subject()),
				slog.String("error", procErr.Error()))
		} else {
			c.logger.Debug("Write processing cancelled; NAK for redelivery",
				slog.String("subject", msg.Subject()),
				slog.String("error", procErr.Error()))
		}
		_ = msg.NakWithDelay(shutdownNakDelay)
	case dispositionTerm:
		c.logger.Error("Terminating JetStream write delivery: structurally invalid message",
			slog.String("subject", msg.Subject()),
			slog.String("error", procErr.Error()))
		if err := msg.Term(); err != nil {
			c.logger.Error("Failed to terminate JetStream message",
				slog.String("error", err.Error()))
		}
	case dispositionNakTransient:
		c.logger.Error("Failed to process JetStream write; NAK for redelivery",
			slog.String("subject", msg.Subject()),
			slog.String("error", procErr.Error()))
		_ = msg.NakWithDelay(transientNakDelay)
	}
}

// processWriteMessage contains the shared logic for processing write messages.
// Used by both core NATS and JetStream handlers. Returns the store or
// required-emit error (already classified and attributed by the store/emit
// layer) so each transport caller can apply its own delivery semantics — the
// JetStream handler's ack decision, the core NATS handler's log-and-drop.
// Errors are returned, not logged here (return-vs-log convention).
func (c *Component) processWriteMessage(ctx context.Context, data []byte) error {
	// Try to parse as BaseMessage to check for ContentStorable payload
	baseMsg, decodeErr := c.decoder.Decode(data)
	if decodeErr == nil {
		// Successfully parsed - check if payload is ContentStorable
		if cs, ok := baseMsg.Payload().(message.ContentStorable); ok {
			// Use StoreContent for proper key generation and StoredContent envelope
			storageRef, err := c.store.StoreContent(ctx, cs)
			if err != nil {
				return err
			}

			atomic.AddUint64(&c.messagesStored, 1)

			// Publish storage event (best-effort monitoring signal, never gates the ack)
			c.publishEvent(Event{
				Type:      "stored",
				Key:       storageRef.Key,
				Timestamp: time.Now(),
			})

			// Emit StoredMessage with proper StorageRef — a REQUIRED publication
			// when the "stored" port is configured; its failure gates the ack.
			return c.emitStoredMessageFromContentStorable(ctx, baseMsg, cs, storageRef)
		}
	}

	// Fallback: store raw bytes for non-ContentStorable messages. The stored
	// payload stays the ORIGINAL wire bytes (re-marshaling the decoded
	// envelope would base64-encode []byte fields and corrupt the JSON), but
	// the KEY derives from the decoded envelope when decode succeeded:
	// keying from the opaque bytes sent every decodable-but-not-
	// ContentStorable message — the PRIMARY lane for JSONMap-style outputs —
	// into message/.../unknown_<ts>, where two distinct messages in the same
	// instant collided and ObjectStore Put silently replaced the first
	// (#741). Only true undecodable bytes take the unknown key family, which
	// DefaultKeyGenerator disambiguates with a per-write UUID nonce (a clock
	// is never a nonce — the wall clock feeds only the date partitions).
	var keySource any = data
	if decodeErr == nil {
		keySource = baseMsg
	}
	key, err := c.store.storeWithKeySource(ctx, keySource, data)
	if err != nil {
		return err
	}

	atomic.AddUint64(&c.messagesStored, 1)

	// Publish simple storage event (best-effort monitoring signal, never gates the ack)
	c.publishEvent(Event{
		Type:      "stored",
		Key:       key,
		Timestamp: time.Now(),
	})

	// Emit StoredMessage if we have a "stored" output port — required
	// publication when configured; decode/non-Graphable skips return nil.
	return c.emitStoredMessage(ctx, baseMsg, decodeErr, data, key)
}

// getPortSubject gets the canonical subject for a configured named port.
func (c *Component) getPortSubject(portName string) string {
	return c.portSubjects[portName]
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.instanceName,
		Type:        "storage",
		Description: "NATS ObjectStore component for immutable message storage",
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports for this component
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputPorts...)
}

// OutputPorts returns the output ports for this component
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputPorts...)
}

// ConfigSchema returns the configuration schema for this component
// References the package-level objectstoreSchema variable for efficient retrieval
func (c *Component) ConfigSchema() component.ConfigSchema {
	return objectstoreSchema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	var lastAct time.Time
	if v := c.lastActivity.Load(); v != nil {
		lastAct = v.(time.Time)
	}

	return component.HealthStatus{
		Healthy:    started,
		LastCheck:  time.Now(),
		ErrorCount: 0, // Would need error tracking
		LastError:  "",
		Uptime:     time.Since(lastAct),
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	var lastAct time.Time
	if v := c.lastActivity.Load(); v != nil {
		lastAct = v.(time.Time)
	}

	// Simple metrics - would need rate calculation in production
	return component.FlowMetrics{
		MessagesPerSecond: 0, // Would need rate calculation
		BytesPerSecond:    0, // Would need byte tracking
		ErrorRate:         0, // Would need error tracking
		LastActivity:      lastAct,
	}
}

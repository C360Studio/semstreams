package component

// PortFacts is an immutable semantic projection of one resolved Port.
type PortFacts struct {
	kind              PortKind
	resourceID        string
	exclusive         bool
	interfaceContract *InterfaceContract
	interaction       InteractionPattern
	connectionIDs     []string
	natsSubjects      []string
	stream            *StreamFacts
	network           *NetworkFacts
	storeReadBucket   string
	kvReadBucket      string
}

// StreamFacts is the immutable JetStream-specific portion of PortFacts.
type StreamFacts struct {
	streamName        string
	subjects          []string
	storage           string
	retentionPolicy   string
	retentionDays     int
	maxSizeGB         int
	replicas          int
	consumerName      string
	deliverPolicy     string
	ackPolicy         string
	maxDeliver        int
	ackWait           string
	heartbeatInterval string
	maxAckPending     int
}

// NetworkFacts is the immutable listener binding portion of PortFacts.
type NetworkFacts struct {
	protocol string
	host     string
	port     int
}

// Kind returns the canonical port kind.
func (f PortFacts) Kind() PortKind { return f.kind }

// ResourceID returns the canonical resource identity.
func (f PortFacts) ResourceID() string { return f.resourceID }

// IsExclusive reports whether two ports may claim the same resource.
func (f PortFacts) IsExclusive() bool { return f.exclusive }

// Interface returns a defensive copy of the semantic interface contract.
func (f PortFacts) Interface() (InterfaceContract, bool) {
	if f.interfaceContract == nil {
		return InterfaceContract{}, false
	}
	return *cloneInterfaceContract(f.interfaceContract), true
}

// InteractionPattern returns the canonical communication behavior.
func (f PortFacts) InteractionPattern() InteractionPattern { return f.interaction }

// ConnectionIDs returns defensive copies of the identifiers used for flow matching.
func (f PortFacts) ConnectionIDs() []string { return append([]string(nil), f.connectionIDs...) }

// NATSSubjects returns defensive copies of declared NATS subject families.
func (f PortFacts) NATSSubjects() []string { return append([]string(nil), f.natsSubjects...) }

// Stream returns the immutable JetStream projection when the port declares one.
func (f PortFacts) Stream() (StreamFacts, bool) {
	if f.stream == nil {
		return StreamFacts{}, false
	}
	stream := *f.stream
	stream.subjects = append([]string(nil), f.stream.subjects...)
	return stream, true
}

// Network returns the immutable network binding when the port declares one.
func (f PortFacts) Network() (NetworkFacts, bool) {
	if f.network == nil {
		return NetworkFacts{}, false
	}
	return *f.network, true
}

// StoreReadBucket returns the configured content-store bucket for a store-read port.
func (f PortFacts) StoreReadBucket() (string, bool) {
	if f.kind != PortKindStoreRead || f.storeReadBucket == "" {
		return "", false
	}
	return f.storeReadBucket, true
}

// KVReadBucket returns the declared KV bucket for a kv-read port, so a
// component OBSERVES the bucket it was bound to instead of predicting the
// name with a constant. Reports false for every other port kind: the second
// return is the whole answer to "is this a KV read port", and a caller must
// never treat an empty bucket as a default.
//
// This is the projection route for the bucket; concrete port config types are
// only interpreted by the canonical projection owners (this file and
// port_codec.go).
func (f PortFacts) KVReadBucket() (string, bool) {
	if f.kind != PortKindKVRead || f.kvReadBucket == "" {
		return "", false
	}
	return f.kvReadBucket, true
}

// Name returns the declared stream name.
func (f StreamFacts) Name() string { return f.streamName }

// Subjects returns a defensive copy of the declared subjects.
func (f StreamFacts) Subjects() []string { return append([]string(nil), f.subjects...) }

// Storage returns the declared JetStream storage mode.
func (f StreamFacts) Storage() string { return f.storage }

// RetentionPolicy returns the declared JetStream retention policy.
func (f StreamFacts) RetentionPolicy() string { return f.retentionPolicy }

// RetentionDays returns the declared stream retention duration in days.
func (f StreamFacts) RetentionDays() int { return f.retentionDays }

// MaxSizeGB returns the declared stream size limit in GiB.
func (f StreamFacts) MaxSizeGB() int { return f.maxSizeGB }

// Replicas returns the declared stream replica count.
func (f StreamFacts) Replicas() int { return f.replicas }

// ConsumerName returns the declared durable consumer name.
func (f StreamFacts) ConsumerName() string { return f.consumerName }

// DeliverPolicy returns the declared consumer delivery policy.
func (f StreamFacts) DeliverPolicy() string { return f.deliverPolicy }

// AckPolicy returns the declared consumer acknowledgement policy.
func (f StreamFacts) AckPolicy() string { return f.ackPolicy }

// MaxDeliver returns the declared maximum delivery attempts.
func (f StreamFacts) MaxDeliver() int { return f.maxDeliver }

// AckWait returns the declared acknowledgement timeout.
func (f StreamFacts) AckWait() string { return f.ackWait }

// HeartbeatInterval returns the declared consumer heartbeat interval.
func (f StreamFacts) HeartbeatInterval() string { return f.heartbeatInterval }

// MaxAckPending returns the declared unacknowledged-message ceiling.
func (f StreamFacts) MaxAckPending() int { return f.maxAckPending }

// Protocol returns the network protocol.
func (f NetworkFacts) Protocol() string { return f.protocol }

// Host returns the configured bind host.
func (f NetworkFacts) Host() string { return f.host }

// Port returns the configured bind port.
func (f NetworkFacts) Port() int { return f.port }

func basePortFacts(config Portable, interaction InteractionPattern, connectionIDs ...string) PortFacts {
	return PortFacts{
		kind:          config.Kind(),
		resourceID:    config.ResourceID(),
		exclusive:     config.IsExclusive(),
		interaction:   interaction,
		connectionIDs: append([]string(nil), connectionIDs...),
	}
}

func timerPortFacts(config Portable) PortFacts {
	port := config.(TimerPort)
	facts := basePortFacts(port, PatternTimer, port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func networkPortFacts(config Portable) PortFacts {
	port := config.(NetworkPort)
	facts := basePortFacts(port, PatternNetwork, port.ResourceID())
	facts.network = &NetworkFacts{protocol: port.Protocol, host: port.Host, port: port.Port}
	return facts
}

func filePortFacts(config Portable) PortFacts {
	port := config.(FilePort)
	return basePortFacts(port, PatternNetwork, port.Path)
}

func httpClientPortFacts(config Portable) PortFacts {
	port := config.(HTTPClientPort)
	facts := basePortFacts(port, PatternHTTPClient, port.URLPattern)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func natsPortFacts(config Portable) PortFacts {
	port := config.(NATSPort)
	facts := basePortFacts(port, PatternStream, port.Subject)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = []string{port.Subject}
	return facts
}

func natsRequestPortFacts(config Portable) PortFacts {
	port := config.(NATSRequestPort)
	facts := basePortFacts(port, PatternRequest, port.Subject)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = []string{port.Subject}
	return facts
}

func jetStreamPortFacts(config Portable) PortFacts {
	port := config.(JetStreamPort)
	connections := make([]string, 0, len(port.Subjects)+1)
	if port.StreamName != "" {
		connections = append(connections, port.StreamName)
	}
	for _, subject := range port.Subjects {
		if subject != "" {
			connections = append(connections, subject)
		}
	}
	facts := basePortFacts(port, PatternStream, connections...)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = append([]string(nil), port.Subjects...)
	facts.stream = &StreamFacts{
		streamName:        port.StreamName,
		subjects:          append([]string(nil), port.Subjects...),
		storage:           port.Storage,
		retentionPolicy:   port.RetentionPolicy,
		retentionDays:     port.RetentionDays,
		maxSizeGB:         port.MaxSizeGB,
		replicas:          port.Replicas,
		consumerName:      port.ConsumerName,
		deliverPolicy:     port.DeliverPolicy,
		ackPolicy:         port.AckPolicy,
		maxDeliver:        port.MaxDeliver,
		ackWait:           port.AckWait,
		heartbeatInterval: port.HeartbeatInterval,
		maxAckPending:     port.MaxAckPending,
	}
	return facts
}

func kvWatchPortFacts(config Portable) PortFacts {
	port := config.(KVWatchPort)
	facts := basePortFacts(port, PatternWatch, port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func kvReadPortFacts(config Portable) PortFacts {
	port := config.(KVReadPort)
	facts := basePortFacts(port, PatternRead, port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.kvReadBucket = port.Bucket
	return facts
}

func kvWritePortFacts(config Portable) PortFacts {
	port := config.(KVWritePort)
	facts := basePortFacts(port, PatternWatch, port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func storeReadPortFacts(config Portable) PortFacts {
	port := config.(StoreReadPort)
	facts := basePortFacts(port, PatternStore, "store-federation")
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.storeReadBucket = port.Bucket
	return facts
}

func storeProvidePortFacts(config Portable) PortFacts {
	port := config.(StoreProvidePort)
	return basePortFacts(port, PatternStore, "store:"+port.Instance)
}

func cloneInterfaceContract(contract *InterfaceContract) *InterfaceContract {
	if contract == nil {
		return nil
	}
	return &InterfaceContract{
		Type:       contract.Type,
		Version:    contract.Version,
		Compatible: append([]string(nil), contract.Compatible...),
	}
}

package component

type portFacts struct {
	kind              PortKind
	resourceID        string
	exclusive         bool
	interfaceContract *InterfaceContract
	interaction       string
	connectionIDs     []string
	natsSubjects      []string
	stream            *streamFacts
}

type streamFacts struct {
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

func basePortFacts(config Portable, interaction string, connectionIDs ...string) portFacts {
	return portFacts{
		kind:          config.Kind(),
		resourceID:    config.ResourceID(),
		exclusive:     config.IsExclusive(),
		interaction:   interaction,
		connectionIDs: append([]string(nil), connectionIDs...),
	}
}

func timerPortFacts(config Portable) portFacts {
	port := config.(TimerPort)
	facts := basePortFacts(port, "timer", port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func networkPortFacts(config Portable) portFacts {
	port := config.(NetworkPort)
	return basePortFacts(port, "network", port.ResourceID())
}

func filePortFacts(config Portable) portFacts {
	port := config.(FilePort)
	return basePortFacts(port, "network", port.Path)
}

func httpClientPortFacts(config Portable) portFacts {
	port := config.(HTTPClientPort)
	facts := basePortFacts(port, "http-client", port.URLPattern)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func natsPortFacts(config Portable) portFacts {
	port := config.(NATSPort)
	facts := basePortFacts(port, "stream", port.Subject)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = []string{port.Subject}
	return facts
}

func natsRequestPortFacts(config Portable) portFacts {
	port := config.(NATSRequestPort)
	facts := basePortFacts(port, "request", port.Subject)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = []string{port.Subject}
	return facts
}

func jetStreamPortFacts(config Portable) portFacts {
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
	facts := basePortFacts(port, "stream", connections...)
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	facts.natsSubjects = append([]string(nil), port.Subjects...)
	facts.stream = &streamFacts{
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

func kvWatchPortFacts(config Portable) portFacts {
	port := config.(KVWatchPort)
	facts := basePortFacts(port, "watch", port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func kvReadPortFacts(config Portable) portFacts {
	port := config.(KVReadPort)
	facts := basePortFacts(port, "watch", port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func kvWritePortFacts(config Portable) portFacts {
	port := config.(KVWritePort)
	facts := basePortFacts(port, "watch", port.ResourceID())
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func storeReadPortFacts(config Portable) portFacts {
	port := config.(StoreReadPort)
	facts := basePortFacts(port, "store", "store-federation")
	facts.interfaceContract = cloneInterfaceContract(port.Interface)
	return facts
}

func storeProvidePortFacts(config Portable) portFacts {
	port := config.(StoreProvidePort)
	return basePortFacts(port, "store", "store:"+port.Instance)
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

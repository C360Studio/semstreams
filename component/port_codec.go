package component

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

type portBinding struct {
	kind       PortKind
	directions map[Direction]struct{}
	newConfig  func() Portable
	normalize  func(Portable) (Portable, error)
	facts      func(Portable) portFacts
}

var canonicalPortKinds = []PortKind{
	PortKindTimer,
	PortKindNetwork,
	PortKindFile,
	PortKindHTTPClient,
	PortKindNATS,
	PortKindNATSRequest,
	PortKindJetStream,
	PortKindKVWatch,
	PortKindKVRead,
	PortKindKVWrite,
	PortKindStoreRead,
	PortKindStoreProvide,
}

var portBindingTable = map[PortKind]portBinding{
	PortKindTimer:        newPortBinding(PortKindTimer, inputOnly(), func() Portable { return &TimerPort{} }, normalizeTimerPort, timerPortFacts),
	PortKindNetwork:      newPortBinding(PortKindNetwork, inputOutput(), func() Portable { return &NetworkPort{} }, normalizeNetworkPort, networkPortFacts),
	PortKindFile:         newPortBinding(PortKindFile, inputOutput(), func() Portable { return &FilePort{} }, normalizeFilePort, filePortFacts),
	PortKindHTTPClient:   newPortBinding(PortKindHTTPClient, inputOnly(), func() Portable { return &HTTPClientPort{} }, normalizeHTTPClientPort, httpClientPortFacts),
	PortKindNATS:         newPortBinding(PortKindNATS, inputOutput(), func() Portable { return &NATSPort{} }, normalizeNATSPort, natsPortFacts),
	PortKindNATSRequest:  newPortBinding(PortKindNATSRequest, inputOutput(), func() Portable { return &NATSRequestPort{} }, normalizeNATSRequestPort, natsRequestPortFacts),
	PortKindJetStream:    newPortBinding(PortKindJetStream, inputOutput(), func() Portable { return &JetStreamPort{} }, normalizeJetStreamPort, jetStreamPortFacts),
	PortKindKVWatch:      newPortBinding(PortKindKVWatch, inputOnly(), func() Portable { return &KVWatchPort{} }, normalizeKVWatchPort, kvWatchPortFacts),
	PortKindKVRead:       newPortBinding(PortKindKVRead, inputOnly(), func() Portable { return &KVReadPort{} }, normalizeKVReadPort, kvReadPortFacts),
	PortKindKVWrite:      newPortBinding(PortKindKVWrite, outputOnly(), func() Portable { return &KVWritePort{} }, normalizeKVWritePort, kvWritePortFacts),
	PortKindStoreRead:    newPortBinding(PortKindStoreRead, inputOnly(), func() Portable { return &StoreReadPort{} }, normalizeStoreReadPort, storeReadPortFacts),
	PortKindStoreProvide: newPortBinding(PortKindStoreProvide, outputOnly(), func() Portable { return &StoreProvidePort{} }, normalizeStoreProvidePort, storeProvidePortFacts),
}

func newPortBinding(
	kind PortKind,
	directions map[Direction]struct{},
	newConfig func() Portable,
	normalize func(Portable) (Portable, error),
	facts func(Portable) portFacts,
) portBinding {
	return portBinding{kind: kind, directions: directions, newConfig: newConfig, normalize: normalize, facts: facts}
}

func inputOnly() map[Direction]struct{} {
	return map[Direction]struct{}{DirectionInput: {}}
}

func outputOnly() map[Direction]struct{} {
	return map[Direction]struct{}{DirectionOutput: {}}
}

func inputOutput() map[Direction]struct{} {
	return map[Direction]struct{}{DirectionInput: {}, DirectionOutput: {}}
}

func portKinds() []PortKind {
	return append([]PortKind(nil), canonicalPortKinds...)
}

func bindingFor(kind PortKind) (portBinding, error) {
	binding, ok := portBindingTable[kind]
	if !ok {
		return portBinding{}, fmt.Errorf("unknown port kind %q", kind)
	}
	return binding, nil
}

func marshalPortable(config Portable) ([]byte, error) {
	canonical, err := canonicalizePortable(config)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal kind %q: %w", canonical.Kind(), err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("marshal kind %q fields: %w", canonical.Kind(), err)
	}
	kind, err := json.Marshal(canonical.Kind())
	if err != nil {
		return nil, fmt.Errorf("marshal kind: %w", err)
	}
	fields["kind"] = kind
	return json.Marshal(fields)
}

func decodePortable(data []byte) (Portable, error) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, errors.New("field \"config\" is required")
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return nil, err
	}
	kindData, ok := fields["kind"]
	if !ok {
		return nil, errors.New("field \"kind\" is required")
	}
	var kind PortKind
	if err := decodeStrict(kindData, &kind); err != nil {
		return nil, fmt.Errorf("field \"kind\": %w", err)
	}
	binding, err := bindingFor(kind)
	if err != nil {
		return nil, err
	}
	delete(fields, "kind")
	configData, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("kind %q config: %w", kind, err)
	}
	return decodeBoundConfig(binding, configData)
}

func canonicalizePortable(config Portable) (Portable, error) {
	if isNilPortable(config) {
		return nil, errors.New("field \"config\" is required")
	}
	binding, err := bindingFor(config.Kind())
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("kind %q config: %w", config.Kind(), err)
	}
	return decodeBoundConfig(binding, data)
}

func decodeBoundConfig(binding portBinding, data []byte) (Portable, error) {
	target := binding.newConfig()
	if err := decodeStrict(data, target); err != nil {
		return nil, fmt.Errorf("kind %q: %w", binding.kind, err)
	}
	value := reflect.ValueOf(target).Elem().Interface().(Portable)
	return binding.normalize(value)
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func kindOf(config Portable) PortKind {
	if isNilPortable(config) {
		return ""
	}
	return config.Kind()
}

func isNilPortable(config Portable) bool {
	if config == nil {
		return true
	}
	value := reflect.ValueOf(config)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func rawPortableKind(data []byte) PortKind {
	var raw struct {
		Kind PortKind `json:"kind"`
	}
	_ = json.Unmarshal(data, &raw)
	return raw.Kind
}

func portConfigError(name string, kind PortKind, field string, err error) error {
	detail := fmt.Sprintf("port %q kind %q field %q: %v", name, kind, field, err)
	return errs.WrapInvalid(err, "component", "port", detail)
}

func validateInterfaceContract(contract *InterfaceContract) error {
	if contract != nil && strings.TrimSpace(contract.Type) == "" {
		return errors.New("field \"interface.type\" is required when interface is present")
	}
	return nil
}

func validatePositiveDuration(field, value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fmt.Errorf("field %q must be a positive duration", field)
	}
	return nil
}

func normalizeTimerPort(config Portable) (Portable, error) {
	port := config.(TimerPort)
	if err := validatePositiveDuration("interval", port.Interval); err != nil {
		return nil, err
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeNetworkPort(config Portable) (Portable, error) {
	port := config.(NetworkPort)
	if strings.TrimSpace(port.Protocol) == "" {
		return nil, errors.New("field \"protocol\" is required")
	}
	if port.Host == "" {
		port.Host = "0.0.0.0"
	}
	if port.Port < 1 || port.Port > 65535 {
		return nil, errors.New("field \"port\" must be between 1 and 65535")
	}
	return port, nil
}

func normalizeFilePort(config Portable) (Portable, error) {
	port := config.(FilePort)
	if strings.TrimSpace(port.Path) == "" {
		return nil, errors.New("field \"path\" is required")
	}
	return port, nil
}

func normalizeHTTPClientPort(config Portable) (Portable, error) {
	port := config.(HTTPClientPort)
	if strings.TrimSpace(port.Method) == "" {
		return nil, errors.New("field \"method\" is required")
	}
	if strings.TrimSpace(port.URLPattern) == "" {
		return nil, errors.New("field \"url_pattern\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeNATSPort(config Portable) (Portable, error) {
	port := config.(NATSPort)
	if strings.TrimSpace(port.Subject) == "" {
		return nil, errors.New("field \"subject\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeNATSRequestPort(config Portable) (Portable, error) {
	port := config.(NATSRequestPort)
	if strings.TrimSpace(port.Subject) == "" {
		return nil, errors.New("field \"subject\" is required")
	}
	if port.Timeout == "" {
		port.Timeout = "1s"
	}
	if err := validatePositiveDuration("timeout", port.Timeout); err != nil {
		return nil, err
	}
	if port.Retries < 0 {
		return nil, errors.New("field \"retries\" must not be negative")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeJetStreamPort(config Portable) (Portable, error) {
	port := config.(JetStreamPort)
	if strings.TrimSpace(port.StreamName) == "" {
		foundSubject := false
		for _, subject := range port.Subjects {
			if strings.TrimSpace(subject) != "" {
				foundSubject = true
				break
			}
		}
		if !foundSubject {
			return nil, errors.New("field \"stream_name\" or non-empty \"subjects\" is required")
		}
	}
	if port.AckWait != "" {
		if err := validatePositiveDuration("ack_wait", port.AckWait); err != nil {
			return nil, err
		}
	}
	if port.HeartbeatInterval != "" {
		if err := validatePositiveDuration("heartbeat_interval", port.HeartbeatInterval); err != nil {
			return nil, err
		}
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeKVWatchPort(config Portable) (Portable, error) {
	port := config.(KVWatchPort)
	if strings.TrimSpace(port.Bucket) == "" {
		return nil, errors.New("field \"bucket\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeKVReadPort(config Portable) (Portable, error) {
	port := config.(KVReadPort)
	if strings.TrimSpace(port.Bucket) == "" {
		return nil, errors.New("field \"bucket\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeKVWritePort(config Portable) (Portable, error) {
	port := config.(KVWritePort)
	if strings.TrimSpace(port.Bucket) == "" {
		return nil, errors.New("field \"bucket\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeStoreReadPort(config Portable) (Portable, error) {
	port := config.(StoreReadPort)
	if strings.TrimSpace(port.Bucket) == "" {
		return nil, errors.New("field \"bucket\" is required")
	}
	if err := validateInterfaceContract(port.Interface); err != nil {
		return nil, err
	}
	return port, nil
}

func normalizeStoreProvidePort(config Portable) (Portable, error) {
	port := config.(StoreProvidePort)
	if strings.TrimSpace(port.Instance) == "" {
		return nil, errors.New("field \"instance\" is required")
	}
	return port, nil
}

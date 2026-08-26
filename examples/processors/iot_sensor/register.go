package iotsensor

import "github.com/c360studio/semstreams/component"

// Register registers the IoT sensor processor component with the given registry.
// This enables the component to be discovered and instantiated by the component
// management system.
//
// The registration includes:
//   - Component factory function for creating instances
//   - Configuration schema for validation and UI generation
//   - Type information (processor, domain: iot)
//   - Protocol identifier for component routing
//   - Version information for compatibility tracking
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "iot_sensor",
		Factory:     NewComponent,
		Ports:       DeclarePorts,
		Schema:      iotSensorSchema,
		Type:        "processor",
		Protocol:    "iot_sensor",
		Domain:      "iot",
		Description: "Transforms incoming JSON sensor data into Graphable SensorReading payloads",
		Version:     "0.1.0",
	})
}

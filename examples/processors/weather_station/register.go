package weatherstation

import "github.com/c360studio/semstreams/component"

// Register registers the component with the registry.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "weather_station",
		Factory:     NewComponent,
		Ports:       DeclarePorts,
		Schema:      weatherStationSchema,
		Type:        "processor",
		Protocol:    "weather_station",
		Domain:      "weather",
		Description: "Transforms weather JSON into Graphable payloads",
		Version:     "0.1.0",
	})
}

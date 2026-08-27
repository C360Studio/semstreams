package composition

import (
	"encoding/json"
	"sort"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/types"
)

// CatalogEntry is one registered factory as the catalog presents it: its
// metadata, its configuration schema, and either the ports its declarer
// resolves for an empty configuration or the reason an empty configuration
// does not declare.
type CatalogEntry struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	Type               string                 `json:"type"`
	Protocol           string                 `json:"protocol"`
	Domain             string                 `json:"domain"`
	Description        string                 `json:"description"`
	Version            string                 `json:"version"`
	Category           string                 `json:"category"`
	Schema             component.ConfigSchema `json:"schema"`
	DefaultPorts       *Ports                 `json:"default_ports,omitempty"`
	PortsRequireConfig bool                   `json:"ports_require_config,omitempty"`
	PortsError         string                 `json:"ports_error,omitempty"`
}

// Ports is the resolved input and output lanes of one declaration.
type Ports struct {
	Inputs  []PortView `json:"inputs"`
	Outputs []PortView `json:"outputs"`
}

// displayNames are the human-readable names the catalog has always carried
// for a handful of factories; every other entry is named by its ID.
var displayNames = map[string]string{
	"udp":               "UDP Input",
	"websocket":         "WebSocket Output",
	"robotics":          "Robotics Processor",
	"graph-processor":   "Graph Processor",
	"rule-processor":    "Rule Processor",
	"context-processor": "Context Processor",
	"objectstore":       "Object Store",
}

// Catalog lists every registered factory, sorted by ID, with its default
// ports (the declarer's output for the empty configuration `{}`) or the
// declarer's refusal.
func Catalog(catalog *component.Registry) []CatalogEntry {
	if catalog == nil {
		return []CatalogEntry{}
	}
	factories := catalog.ListFactories()
	ids := make([]string, 0, len(factories))
	for id := range factories {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]CatalogEntry, 0, len(ids))
	for _, id := range ids {
		registration := factories[id]
		entry := CatalogEntry{
			ID:          id,
			Name:        id,
			Type:        registration.Type,
			Protocol:    registration.Protocol,
			Domain:      registration.Domain,
			Description: registration.Description,
			Version:     registration.Version,
			Category:    registration.Type,
			Schema:      registration.Schema,
		}
		if name, ok := displayNames[id]; ok {
			entry.Name = name
		}
		declaration, err := catalog.Declare("catalog", types.ComponentConfig{
			Name: id, Type: types.ComponentType(registration.Type), Enabled: true, Config: json.RawMessage(`{}`),
		})
		if err != nil {
			entry.PortsRequireConfig = true
			entry.PortsError = err.Error()
		} else {
			entry.DefaultPorts = &Ports{
				Inputs:  portViews(declaration.InputPorts, declaration.InputFacts),
				Outputs: portViews(declaration.OutputPorts, declaration.OutputFacts),
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

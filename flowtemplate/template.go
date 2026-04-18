// Package flowtemplate provides storage and instantiation for parameterised
// flow definitions.
//
// A Template is a flow-shaped JSON blob with named substitution points the
// coordinator agent (or any operator) fills in to produce a concrete
// flowstore.Flow ready to deploy. Step 4 of ADR-029 Pattern B.
//
// Templating is intentionally minimal for the first pass: Go's text/template
// over the marshalled JSON body with a Parameters map. Real templating
// engines (conditionals, ranges over arrays of params) can land when a
// concrete product use case exceeds string substitution.
package flowtemplate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/c360studio/semstreams/flowstore"
)

// Template is the stored form of a parameterised flow definition.
type Template struct {
	// ID uniquely identifies the template in the FLOW_TEMPLATES bucket.
	// Kebab-case convention: "research-pipeline", "temp-anomaly-agent".
	ID string `json:"id"`

	// Name is a human-friendly label surfaced to operators and the
	// coordinator agent when choosing a template.
	Name string `json:"name"`

	// Description is optional context explaining what this template
	// produces and when to use it — surfaced to the coordinator so it
	// can pick sensibly.
	Description string `json:"description,omitempty"`

	// Body is the flow definition with text/template placeholders. When
	// instantiated, placeholders resolve against user-supplied Parameters.
	// Canonical substitution syntax: {{.SomeParam}}.
	Body string `json:"body"`

	// Parameters declares the substitution points and their defaults.
	// Instantiate() uses defaults for any param the caller omits. Order
	// of Parameters is not significant — the slice form is just to
	// preserve authoring order for operators scanning the template.
	Parameters []Parameter `json:"parameters,omitempty"`
}

// Parameter declares one substitution point on a Template.
type Parameter struct {
	// Name matches the placeholder in Body (e.g. "TopicName" for
	// {{.TopicName}}).
	Name string `json:"name"`

	// Description helps the coordinator / operator pick a value.
	Description string `json:"description,omitempty"`

	// Default is the value used when Instantiate is called without
	// overriding this parameter. Empty means "required — no default".
	Default string `json:"default,omitempty"`

	// Required signals the caller must supply a value (no default).
	// When Required is true, Default is ignored.
	Required bool `json:"required,omitempty"`
}

// Validate checks invariants on a stored Template.
func (t *Template) Validate() error {
	if t.ID == "" {
		return errInvalidTemplate{reason: "id is required"}
	}
	if t.Name == "" {
		return errInvalidTemplate{reason: "name is required"}
	}
	if t.Body == "" {
		return errInvalidTemplate{reason: "body is required"}
	}
	// Parse-only to catch malformed placeholders before write.
	if _, err := template.New(t.ID).Parse(t.Body); err != nil {
		return errInvalidTemplate{reason: fmt.Sprintf("body is not a valid text/template: %v", err)}
	}
	return nil
}

// Instantiate renders the template into a concrete flowstore.Flow using
// the supplied parameter values. Unsupplied parameters fall back to
// Default. Required parameters with no supplied value return an error
// so callers see the problem before JSON decoding.
func (t *Template) Instantiate(params map[string]string) (*flowstore.Flow, error) {
	resolved := make(map[string]string, len(t.Parameters))
	for _, p := range t.Parameters {
		val, supplied := params[p.Name]
		if !supplied {
			if p.Required {
				return nil, fmt.Errorf("required parameter %q not supplied", p.Name)
			}
			val = p.Default
		}
		resolved[p.Name] = val
	}
	// Allow callers to pass extra params the template doesn't declare —
	// Go's text/template happily ignores unused keys. Makes templates
	// forgiving to additive parameter lists.
	for k, v := range params {
		if _, declared := resolved[k]; !declared {
			resolved[k] = v
		}
	}

	tmpl, err := template.New(t.ID).Parse(t.Body)
	if err != nil {
		return nil, fmt.Errorf("parse template body: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, resolved); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	var flow flowstore.Flow
	if err := json.Unmarshal(buf.Bytes(), &flow); err != nil {
		return nil, fmt.Errorf("rendered template is not a valid flow: %w", err)
	}
	return &flow, nil
}

type errInvalidTemplate struct {
	reason string
}

func (e errInvalidTemplate) Error() string {
	return "invalid flow template: " + e.reason
}

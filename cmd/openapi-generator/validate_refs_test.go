package main

import (
	"strings"
	"testing"
)

// TestValidateOpenAPIRefs_FlagsDanglingRefs is the gh#228 regression lock: a
// $ref to a schema absent from components.schemas must fail generation. It also
// proves the walker descends into nested schema properties AND array items (not
// just top-level operation refs), since that's where dangling refs hide and
// where the drift gate is blind.
func TestValidateOpenAPIRefs_FlagsDanglingRefs(t *testing.T) {
	doc := OpenAPIDocument{
		Components: ComponentsObject{
			Schemas: map[string]any{
				"Defined": map[string]any{"type": "object"},
				"Referencer": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"child": map[string]any{"$ref": "#/components/schemas/MissingNested"},
						"ok":    map[string]any{"$ref": "#/components/schemas/Defined"},
						"list": map[string]any{
							"type":  "array",
							"items": map[string]any{"$ref": "#/components/schemas/MissingItem"},
						},
					},
				},
			},
		},
	}

	err := validateOpenAPIRefs(doc)
	if err == nil {
		t.Fatal("expected a dangling-ref error for MissingNested + MissingItem")
	}
	for _, want := range []string{"MissingNested", "MissingItem"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name the dangling ref %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Defined") {
		t.Errorf("a resolved ref (Defined) must not be reported: %v", err)
	}
}

// TestValidateOpenAPIRefs_FlagsDanglingRefInOperationBody locks the regression at
// the ACTUAL production seam (gh#227/gh#228): a SchemaRef.Ref in an operation
// response or request body pointing at an unregistered schema. Reflection-
// generated component schemas inline nested structs (no $ref), so dangling refs
// only ever originate from operation bodies — this is the shape that shipped in
// gh#227 (StatePatchRequest on POST /workflows/{type}/{id}/state).
func TestValidateOpenAPIRefs_FlagsDanglingRefInOperationBody(t *testing.T) {
	doc := OpenAPIDocument{
		Components: ComponentsObject{
			Schemas: map[string]any{"DefinedResponse": map[string]any{"type": "object"}},
		},
		Paths: map[string]PathItem{
			"/widgets": {
				Get: &Operation{
					Summary: "list widgets",
					Responses: map[string]Response{
						"200": {
							Description: "ok",
							Content: map[string]MediaType{
								"application/json": {Schema: SchemaRef{Ref: "#/components/schemas/MissingResponse"}},
							},
						},
					},
				},
				Post: &Operation{
					Summary: "create widget",
					RequestBody: &RequestBodyObject{
						Content: map[string]MediaType{
							"application/json": {Schema: SchemaRef{Ref: "#/components/schemas/MissingRequest"}},
						},
					},
					Responses: map[string]Response{
						"201": {
							Description: "created",
							Content: map[string]MediaType{
								"application/json": {Schema: SchemaRef{Ref: "#/components/schemas/DefinedResponse"}},
							},
						},
					},
				},
			},
		},
	}

	err := validateOpenAPIRefs(doc)
	if err == nil {
		t.Fatal("expected dangling-ref error for the operation response + request bodies")
	}
	for _, want := range []string{"MissingResponse", "MissingRequest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name dangling ref %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "DefinedResponse") {
		t.Errorf("resolved ref DefinedResponse must not be reported: %v", err)
	}
}

// TestValidateOpenAPIRefs_PassesWhenAllResolve is the false-positive guard.
func TestValidateOpenAPIRefs_PassesWhenAllResolve(t *testing.T) {
	doc := OpenAPIDocument{
		Components: ComponentsObject{
			Schemas: map[string]any{
				"Defined": map[string]any{"type": "object"},
				"Referencer": map[string]any{
					"properties": map[string]any{
						"child": map[string]any{"$ref": "#/components/schemas/Defined"},
					},
				},
			},
		},
	}
	if err := validateOpenAPIRefs(doc); err != nil {
		t.Fatalf("all refs resolve; expected no error, got: %v", err)
	}
}

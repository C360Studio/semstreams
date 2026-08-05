package graphgateway

import (
	"testing"
)

func TestGraphQLIntrospectionDeclaresExactEntityResult(t *testing.T) {
	schema := buildIntrospectionSchema()
	types, ok := schema["types"].([]map[string]interface{})
	if !ok {
		t.Fatalf("types shape = %T", schema["types"])
	}
	var queryEntityType string
	var queryAliasType string
	var exactFields map[string]string
	for _, definition := range types {
		name, _ := definition["name"].(string)
		fields, _ := definition["fields"].([]map[string]interface{})
		if name == "Query" {
			for _, field := range fields {
				if field["name"] == "entity" {
					typeRef, _ := field["type"].(map[string]interface{})
					queryEntityType, _ = typeRef["name"].(string)
				}
				if field["name"] == "entityByAlias" {
					typeRef, _ := field["type"].(map[string]interface{})
					queryAliasType, _ = typeRef["name"].(string)
				}
			}
		}
		if name == "ExactEntity" {
			exactFields = make(map[string]string, len(fields))
			for _, field := range fields {
				fieldName, _ := field["name"].(string)
				typeRef, _ := field["type"].(map[string]interface{})
				exactFields[fieldName], _ = typeRef["name"].(string)
			}
		}
	}
	if queryEntityType != "ExactEntity" {
		t.Fatalf("Query.entity type = %q, want ExactEntity", queryEntityType)
	}
	if queryAliasType != "ExactEntity" {
		t.Fatalf("Query.entityByAlias type = %q, want ExactEntity", queryAliasType)
	}
	if exactFields["entity"] != "Entity" || exactFields["kvRevision"] != "Uint64" {
		t.Fatalf("ExactEntity fields = %v", exactFields)
	}
}

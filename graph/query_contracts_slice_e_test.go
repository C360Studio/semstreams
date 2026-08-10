package graph

import (
	"reflect"
	"strings"
	"testing"
)

func TestSliceEQueryResponseContainsOnlyDataAndTimestamp(t *testing.T) {
	typeOfResponse := reflect.TypeOf(QueryResponse[struct{}]{})
	keys := make([]string, 0, typeOfResponse.NumField())
	for i := 0; i < typeOfResponse.NumField(); i++ {
		key, _, _ := strings.Cut(typeOfResponse.Field(i).Tag.Get("json"), ",")
		keys = append(keys, key)
	}
	if !reflect.DeepEqual(keys, []string{"data", "timestamp"}) {
		t.Fatalf("QueryResponse JSON fields = %v, want [data timestamp]", keys)
	}
}

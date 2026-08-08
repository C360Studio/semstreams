package agentictools

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
)

func TestDefaultConfigDeclaresExactKVReads(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"entity_states": graph.BucketEntityStates,
		"agent_loops":   "AGENT_LOOPS",
	}
	for _, definition := range DefaultConfig().Ports.Inputs {
		read, ok := definition.Config.(component.KVReadPort)
		if !ok {
			continue
		}
		bucket, exists := want[definition.Name]
		if !exists {
			t.Errorf("unexpected KV read %q", definition.Name)
			continue
		}
		if read.Bucket != bucket {
			t.Errorf("KV read %q bucket = %q, want %q", definition.Name, read.Bucket, bucket)
		}
		delete(want, definition.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing KV reads: %v", want)
	}
}

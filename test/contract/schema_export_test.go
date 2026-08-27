package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaExportCarriesDefaultPorts — every committed component schema
// carries, under x-component-metadata, either default_ports (the declarer's
// resolved output for an empty configuration) or ports_require_config with
// the declarer's error text; never both, never neither.
func TestSchemaExportCarriesDefaultPorts(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot, "schemas"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".v1.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, "schemas", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Metadata struct {
				DefaultPorts       json.RawMessage `json:"default_ports"`
				PortsRequireConfig bool            `json:"ports_require_config"`
				PortsError         string          `json:"ports_error"`
			} `json:"x-component-metadata"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		hasDefaults := len(schema.Metadata.DefaultPorts) > 0 && string(schema.Metadata.DefaultPorts) != "null"
		switch {
		case hasDefaults && !schema.Metadata.PortsRequireConfig && schema.Metadata.PortsError == "":
		case !hasDefaults && schema.Metadata.PortsRequireConfig && schema.Metadata.PortsError != "":
		default:
			t.Errorf("%s: default_ports=%v ports_require_config=%v ports_error=%q — want exactly one shape",
				entry.Name(), hasDefaults, schema.Metadata.PortsRequireConfig, schema.Metadata.PortsError)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no component schemas checked")
	}
}

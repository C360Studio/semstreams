package component

import "fmt"

// FilePort - File system access
type FilePort struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern,omitempty"`
}

// ResourceID returns unique identifier for file ports
func (f FilePort) ResourceID() string {
	return fmt.Sprintf("file:%s", f.Path)
}

// IsExclusive returns false as multiple components can read files
func (f FilePort) IsExclusive() bool {
	return false
}

// Type returns the port type identifier
func (f FilePort) Type() string {
	return "file"
}

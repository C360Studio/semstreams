package component

import "fmt"

// NetworkPort - TCP/UDP network bindings
type NetworkPort struct {
	Protocol string `json:"protocol"` // "tcp", "udp"
	Host     string `json:"host"`     // "0.0.0.0", "localhost"
	Port     int    `json:"port"`     // 14550, 8080
}

// ResourceID returns unique identifier for network ports
func (n NetworkPort) ResourceID() string {
	return fmt.Sprintf("%s:%s:%d", n.Protocol, n.Host, n.Port)
}

// IsExclusive returns true as network ports are exclusive
func (n NetworkPort) IsExclusive() bool {
	return true
}

// Type returns the port type identifier
func (n NetworkPort) Type() string {
	return "network"
}

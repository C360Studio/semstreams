// Package service provides service registration and management
package service

import (
	"fmt"
	"maps"
	"sync"
)

// Registry manages service constructor registration
type Registry struct {
	constructors map[string]Constructor
	mu           sync.RWMutex
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry() *Registry {
	return &Registry{
		constructors: make(map[string]Constructor),
	}
}

// Register registers a service constructor
func (r *Registry) Register(name string, constructor Constructor) error {
	if name == "" {
		return fmt.Errorf("service name cannot be empty")
	}
	if constructor == nil {
		return fmt.Errorf("constructor cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.constructors[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	r.constructors[name] = constructor
	return nil
}

// Constructor returns a constructor for the given service name
func (r *Registry) Constructor(name string) (Constructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, exists := r.constructors[name]
	return constructor, exists
}

// Services returns all registered service names
func (r *Registry) Services() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.constructors))
	for name := range r.constructors {
		names = append(names, name)
	}
	return names
}

// Constructors returns a copy of all constructors
func (r *Registry) Constructors() map[string]Constructor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c := make(map[string]Constructor, len(r.constructors))
	maps.Copy(c, r.constructors)
	return c
}

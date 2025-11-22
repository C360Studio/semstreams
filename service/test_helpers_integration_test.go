//go:build integration

package service

import (
	"net/http/httptest"
	"sync"
)

// safeResponseRecorder is a thread-safe wrapper around httptest.ResponseRecorder
// that protects concurrent access to the body buffer
type safeResponseRecorder struct {
	*httptest.ResponseRecorder
	mu sync.RWMutex
}

// newSafeResponseRecorder creates a new thread-safe response recorder
func newSafeResponseRecorder() *safeResponseRecorder {
	return &safeResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// Write wraps the ResponseRecorder Write method with a write lock
func (s *safeResponseRecorder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ResponseRecorder.Write(b)
}

// BodyString safely reads the body as a string
func (s *safeResponseRecorder) BodyString() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ResponseRecorder.Body.String()
}

// BodyBytes safely reads the body as bytes
func (s *safeResponseRecorder) BodyBytes() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	buf := s.ResponseRecorder.Body.Bytes()
	// Return a copy to prevent external mutation
	result := make([]byte, len(buf))
	copy(result, buf)
	return result
}

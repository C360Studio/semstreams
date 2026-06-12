package agenticdispatch

import (
	"sync"

	"github.com/c360studio/semstreams/agentic"
)

// captureSink collects every UserResponse the component tries to send. Handlers
// that use c.sendResponse (rather than return values) are observed by installing
// sink.add as the component's sendResponseFn.
type captureSink struct {
	mu        sync.Mutex
	responses []agentic.UserResponse
}

func (s *captureSink) add(r agentic.UserResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, r)
}

func (s *captureSink) all() []agentic.UserResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]agentic.UserResponse, len(s.responses))
	copy(out, s.responses)
	return out
}

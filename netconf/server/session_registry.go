package server

import (
	"sync"

	"github.com/gliderlabs/ssh"
)

// sessionRegistry is a concurrency-safe map from NETCONF session-id to the
// live ssh.Session for that session. It is used by kill-session to locate and
// forcibly terminate a target session (RFC 6241 §7.9).
type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[int]ssh.Session
}

// globalSessionRegistry is the process-wide registry; all SessionHandler
// goroutines Register on entry and Unregister (deferred) on exit.
var globalSessionRegistry = &sessionRegistry{
	sessions: make(map[int]ssh.Session),
}

// Register adds a session to the registry under the given NETCONF session-id.
func (r *sessionRegistry) Register(id int, s ssh.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = s
}

// Unregister removes the session with the given id. Safe to call on an id that
// is not present.
func (r *sessionRegistry) Unregister(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// Get returns the session for the given id and whether it was found.
func (r *sessionRegistry) Get(id int) (ssh.Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Len returns the number of currently registered sessions (useful in tests).
func (r *sessionRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

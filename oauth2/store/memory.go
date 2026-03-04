// Package store provides SessionStore implementations for duck/oauth2.
//
// Use [NewMemoryStore] for development and testing.
// Use [NewRedisStore] for production deployments.
//
// You can also implement [oauth2.SessionStore] directly to use any backend.
package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/qwackididuck/duck/oauth2"
)

// ErrSessionNotFound is returned by Get when the session does not exist.
var ErrSessionNotFound = errors.New("session not found")

// MemoryStore is an in-process session store backed by a map.
// Not suitable for production (data lost on restart, not shared across instances).
// Perfect for development, testing, and single-instance deployments.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*oauth2.Session // by session ID
	byUser   map[string][]string        // userID -> []sessionID
}

// NewMemoryStore returns a new in-memory SessionStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: map[string]*oauth2.Session{},
		byUser:   map[string][]string{},
	}
}

// Save persists a session. Implements [oauth2.SessionStore].
func (s *MemoryStore) Save(_ context.Context, session *oauth2.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.ID] = session
	s.byUser[session.UserID] = append(s.byUser[session.UserID], session.ID)

	return nil
}

// Get retrieves a session by ID. Implements [oauth2.SessionStore].
func (s *MemoryStore) Get(_ context.Context, sessionID string) (*oauth2.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

// Delete removes a single session. Implements [oauth2.SessionStore].
func (s *MemoryStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil
	}

	delete(s.sessions, sessionID)
	s.removeFromUser(session.UserID, sessionID)

	return nil
}

// DeleteAllForUser removes all sessions for a user. Implements [oauth2.SessionStore].
func (s *MemoryStore) DeleteAllForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.byUser[userID]
	for _, id := range ids {
		delete(s.sessions, id)
	}

	delete(s.byUser, userID)

	return nil
}

// GC removes expired sessions. Call this periodically in a background goroutine.
//
//	srv.Go(func(ctx context.Context) {
//	    ticker := time.NewTicker(time.Hour)
//	    defer ticker.Stop()
//	    for {
//	        select {
//	        case <-ctx.Done(): return
//	        case <-ticker.C:  store.GC()
//	        }
//	    }
//	})
func (s *MemoryStore) GC() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
			s.removeFromUser(session.UserID, id)
		}
	}
}

// removeFromUser removes a session ID from the user index.
// Must be called with the write lock held.
func (s *MemoryStore) removeFromUser(userID, sessionID string) {
	ids := s.byUser[userID]
	filtered := ids[:0]

	for _, id := range ids {
		if id != sessionID {
			filtered = append(filtered, id)
		}
	}

	if len(filtered) == 0 {
		delete(s.byUser, userID)
	} else {
		s.byUser[userID] = filtered
	}
}

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/qwackididuck/duck/oauth2"
	"github.com/qwackididuck/duck/oauth2/store"
)

func makeSession(id, userID string, ttl time.Duration) *oauth2.Session {
	return &oauth2.Session{
		ID:        id,
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}
}

//nolint:cyclop
func TestMemoryStore(t *testing.T) {
	t.Parallel()

	t.Run("Save and Get", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()
		session := makeSession("sess-1", "user-1", time.Hour)

		if err := s.Save(context.Background(), session); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := s.Get(context.Background(), "sess-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got.UserID != "user-1" {
			t.Errorf("UserID: expected %q, got %q", "user-1", got.UserID)
		}
	})

	t.Run("Get non-existent returns error", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()

		_, err := s.Get(context.Background(), "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
	})

	t.Run("Delete removes session", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()
		session := makeSession("sess-2", "user-2", time.Hour)
		_ = s.Save(context.Background(), session)

		if err := s.Delete(context.Background(), "sess-2"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		if _, err := s.Get(context.Background(), "sess-2"); err == nil {
			t.Fatal("expected error after delete, got nil")
		}
	})

	t.Run("Delete non-existent does not error", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()
		if err := s.Delete(context.Background(), "ghost"); err != nil {
			t.Errorf("expected no error deleting non-existent session, got %v", err)
		}
	})

	t.Run("DeleteAllForUser removes all user sessions", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()
		_ = s.Save(context.Background(), makeSession("s1", "user-3", time.Hour))
		_ = s.Save(context.Background(), makeSession("s2", "user-3", time.Hour))
		_ = s.Save(context.Background(), makeSession("s3", "other-user", time.Hour))

		if err := s.DeleteAllForUser(context.Background(), "user-3"); err != nil {
			t.Fatalf("DeleteAllForUser: %v", err)
		}

		if _, err := s.Get(context.Background(), "s1"); err == nil {
			t.Error("expected s1 to be deleted")
		}

		if _, err := s.Get(context.Background(), "s2"); err == nil {
			t.Error("expected s2 to be deleted")
		}

		// Other user session must be untouched.
		if _, err := s.Get(context.Background(), "s3"); err != nil {
			t.Errorf("expected s3 to still exist: %v", err)
		}
	})

	t.Run("GC removes expired sessions", func(t *testing.T) {
		t.Parallel()

		s := store.NewMemoryStore()
		_ = s.Save(context.Background(), makeSession("expired", "user-4", -time.Second))
		_ = s.Save(context.Background(), makeSession("valid", "user-4", time.Hour))

		s.GC()

		if _, err := s.Get(context.Background(), "expired"); err == nil {
			t.Error("expected expired session to be removed by GC")
		}

		if _, err := s.Get(context.Background(), "valid"); err != nil {
			t.Errorf("expected valid session to survive GC: %v", err)
		}
	})
}

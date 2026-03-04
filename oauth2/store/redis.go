package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/qwackididuck/duck/oauth2"
)

// RedisStore is a Redis-backed session store.
// Sessions are stored as JSON with automatic TTL expiration.
// The user index uses Redis Sets to support DeleteAllForUser.
type RedisStore struct {
	client *redis.Client
	opts   redisOptions
}

type redisOptions struct {
	keyPrefix string
	ttl       time.Duration
}

// RedisOption is a functional option for [NewRedisStore].
type RedisOption func(*redisOptions)

// WithKeyPrefix sets the Redis key prefix. Defaults to "duck:sessions:".
// Use this to namespace keys when sharing a Redis instance.
func WithKeyPrefix(prefix string) RedisOption {
	return func(o *redisOptions) {
		o.keyPrefix = prefix
	}
}

// WithTTL sets the session TTL in Redis. Should match the TTL configured in
// [oauth2.WithSessionTTL]. Defaults to 7 days.
func WithTTL(d time.Duration) RedisOption {
	return func(o *redisOptions) {
		o.ttl = d
	}
}

// NewRedisStore returns a Redis-backed SessionStore.
//
//	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	store := store.NewRedisStore(client,
//	    store.WithKeyPrefix("myapp:sessions:"),
//	    store.WithTTL(7 * 24 * time.Hour),
//	)
func NewRedisStore(client *redis.Client, opts ...RedisOption) *RedisStore {
	o := redisOptions{
		keyPrefix: "duck:sessions:",
		ttl:       7 * 24 * time.Hour,
	}

	for _, opt := range opts {
		opt(&o)
	}

	return &RedisStore{client: client, opts: o}
}

// Save persists a session. Implements [oauth2.SessionStore].
func (s *RedisStore) Save(ctx context.Context, session *oauth2.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("redis store: marshal session: %w", err)
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		ttl = s.opts.ttl
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.sessionKey(session.ID), data, ttl)
	pipe.SAdd(ctx, s.userKey(session.UserID), session.ID)
	pipe.Expire(ctx, s.userKey(session.UserID), ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis store: save session: %w", err)
	}

	return nil
}

// Get retrieves a session by ID. Implements [oauth2.SessionStore].
func (s *RedisStore) Get(ctx context.Context, sessionID string) (*oauth2.Session, error) {
	data, err := s.client.Get(ctx, s.sessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrSessionNotFound
		}

		return nil, fmt.Errorf("redis store: get session: %w", err)
	}

	var session oauth2.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("redis store: unmarshal session: %w", err)
	}

	return &session, nil
}

// Delete removes a single session. Implements [oauth2.SessionStore].
func (s *RedisStore) Delete(ctx context.Context, sessionID string) error {
	data, err := s.client.Get(ctx, s.sessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}

		return fmt.Errorf("redis store: get for delete: %w", err)
	}

	var session oauth2.Session
	if err := json.Unmarshal(data, &session); err == nil {
		s.client.SRem(ctx, s.userKey(session.UserID), sessionID)
	}

	if err := s.client.Del(ctx, s.sessionKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("redis store: delete session: %w", err)
	}

	return nil
}

// DeleteAllForUser removes all sessions for a user. Implements [oauth2.SessionStore].
func (s *RedisStore) DeleteAllForUser(ctx context.Context, userID string) error {
	userKey := s.userKey(userID)

	ids, err := s.client.SMembers(ctx, userKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis store: get user sessions: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	keys := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		keys = append(keys, s.sessionKey(id))
	}

	keys = append(keys, userKey)

	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis store: delete all user sessions: %w", err)
	}

	return nil
}

// sessionKey returns the Redis key for a session.
func (s *RedisStore) sessionKey(id string) string {
	return s.opts.keyPrefix + id
}

// userKey returns the Redis key for the user session index.
func (s *RedisStore) userKey(userID string) string {
	return s.opts.keyPrefix + "user:" + userID
}

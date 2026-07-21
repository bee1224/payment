package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

type ReplayNonceStore interface {
	Use(ctx context.Context, scope, nonceKey string, expiresAt, now time.Time) (bool, error)
}

type InMemoryReplayNonceStore struct {
	mu      sync.Mutex
	expires map[string]time.Time
}

func NewInMemoryReplayNonceStore() *InMemoryReplayNonceStore {
	return &InMemoryReplayNonceStore{expires: make(map[string]time.Time)}
}

func (s *InMemoryReplayNonceStore) Use(_ context.Context, scope, nonceKey string, expiresAt, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, expiry := range s.expires {
		if !expiry.After(now) {
			delete(s.expires, key)
		}
	}

	key := replayNonceCompositeKey(scope, nonceKey)
	if expiry, ok := s.expires[key]; ok && expiry.After(now) {
		return false, nil
	}
	s.expires[key] = expiresAt
	return true, nil
}

type MySQLReplayNonceStore struct {
	db *sql.DB
}

func NewMySQLReplayNonceStore(db *sql.DB) *MySQLReplayNonceStore {
	return &MySQLReplayNonceStore{db: db}
}

func (s *MySQLReplayNonceStore) Use(ctx context.Context, scope, nonceKey string, expiresAt, now time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return true, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer rollback(tx)

	scope = strings.TrimSpace(scope)
	nonceKey = strings.TrimSpace(nonceKey)

	var existingExpiry time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT expires_at
		FROM replay_nonces
		WHERE scope = ? AND nonce_key = ?
		FOR UPDATE
	`, scope, nonceKey).Scan(&existingExpiry)
	switch {
	case err == nil:
		if existingExpiry.After(now) {
			return false, tx.Commit()
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE replay_nonces
			SET expires_at = ?, updated_at = CURRENT_TIMESTAMP
			WHERE scope = ? AND nonce_key = ?
		`, expiresAt, scope, nonceKey); err != nil {
			return false, err
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO replay_nonces (scope, nonce_key, expires_at)
			VALUES (?, ?, ?)
		`, scope, nonceKey, expiresAt); err != nil {
			return false, err
		}
	default:
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM replay_nonces
		WHERE expires_at <= ?
		LIMIT 200
	`, now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func replayNonceCompositeKey(scope, nonceKey string) string {
	return strings.TrimSpace(scope) + "|" + strings.TrimSpace(nonceKey)
}

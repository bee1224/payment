package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// WorkerLeaseStore elects one active worker for a job category.  The lease is
// deliberately short-lived: another instance can take over after a crash.
type WorkerLeaseStore interface {
	TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error)
}

type InMemoryWorkerLeaseStore struct{}

func NewInMemoryWorkerLeaseStore() *InMemoryWorkerLeaseStore { return &InMemoryWorkerLeaseStore{} }
func (*InMemoryWorkerLeaseStore) TryAcquire(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

type MySQLWorkerLeaseStore struct{ db *sql.DB }

func NewMySQLWorkerLeaseStore(db *sql.DB) *MySQLWorkerLeaseStore {
	return &MySQLWorkerLeaseStore{db: db}
}

func (s *MySQLWorkerLeaseStore) TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error) {
	if s == nil || s.db == nil {
		return true, nil
	}
	name, holder = strings.TrimSpace(name), strings.TrimSpace(holder)
	if name == "" || holder == "" {
		return false, nil
	}
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	var currentHolder string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT holder, expires_at FROM worker_leases WHERE name=? FOR UPDATE`, name).Scan(&currentHolder, &expiresAt)
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `INSERT INTO worker_leases (name, holder, expires_at) VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND))`, name, holder, int(ttl.Seconds()))
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if currentHolder != holder && expiresAt.After(time.Now().UTC()) {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE worker_leases SET holder=?, expires_at=DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND), updated_at=CURRENT_TIMESTAMP WHERE name=?`, holder, int(ttl.Seconds()), name)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

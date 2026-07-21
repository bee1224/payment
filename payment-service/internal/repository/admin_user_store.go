package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AdminUser struct {
	Username     string
	PasswordHash string
	Role         string
	MFASecret    string
}
type AdminUserStore struct{ db *sql.DB }

func NewAdminUserStore(db *sql.DB) *AdminUserStore { return &AdminUserStore{db: db} }
func (s *AdminUserStore) FindActive(ctx context.Context, username string) (AdminUser, error) {
	var u AdminUser
	err := s.db.QueryRowContext(ctx, `SELECT username,password_hash,role,COALESCE(mfa_secret,'') FROM admin_users WHERE username=? AND status='active'`, strings.ToUpper(strings.TrimSpace(username))).Scan(&u.Username, &u.PasswordHash, &u.Role, &u.MFASecret)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUser{}, ErrNotFound
	}
	return u, err
}

func (s *AdminUserStore) BeginMFAEnrollment(ctx context.Context, username, encryptedSecret string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_mfa_enrollments (username,secret,expires_at) VALUES (?,?,?) ON DUPLICATE KEY UPDATE secret=VALUES(secret),expires_at=VALUES(expires_at),created_at=CURRENT_TIMESTAMP`, strings.ToUpper(strings.TrimSpace(username)), encryptedSecret, time.Now().UTC().Add(10*time.Minute))
	return err
}

func (s *AdminUserStore) CompleteMFAEnrollment(ctx context.Context, username, encryptedSecret string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE admin_users SET mfa_secret=?,mfa_enabled_at=CURRENT_TIMESTAMP WHERE username=? AND status='active'`, encryptedSecret, strings.ToUpper(strings.TrimSpace(username)))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_mfa_enrollments WHERE username=?`, strings.ToUpper(strings.TrimSpace(username))); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AdminUserStore) EnrollmentSecret(ctx context.Context, username string) (string, error) {
	var secret string
	err := s.db.QueryRowContext(ctx, `SELECT secret FROM admin_mfa_enrollments WHERE username=? AND expires_at>CURRENT_TIMESTAMP`, strings.ToUpper(strings.TrimSpace(username))).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return secret, err
}

func (s *AdminUserStore) LogAuthEvent(ctx context.Context, username, eventType, sourceIP, requestID string) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO admin_auth_audit_logs (username,event_type,source_ip,request_id) VALUES (?,?,?,?)`, strings.ToUpper(strings.TrimSpace(username)), strings.TrimSpace(eventType), strings.TrimSpace(sourceIP), strings.TrimSpace(requestID))
}
func (s *AdminUserStore) SeedInitial(ctx context.Context, username, passwordHash string) error {
	username = strings.ToUpper(strings.TrimSpace(username))
	if username == "" || strings.TrimSpace(passwordHash) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_users (username,password_hash,role,status) VALUES (?,?, 'ADMIN','active') ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash), role='ADMIN', status='active'`, username, passwordHash)
	return err
}

-- Administrative access is deliberately separate from payment API credentials.
-- MFA seeds are AES-GCM encrypted by the application before persistence.
ALTER TABLE admin_users ADD COLUMN mfa_secret VARCHAR(1024) NULL;
ALTER TABLE admin_users ADD COLUMN mfa_enabled_at TIMESTAMP NULL;

CREATE TABLE IF NOT EXISTS admin_mfa_enrollments (
  username VARCHAR(128) PRIMARY KEY,
  secret VARCHAR(1024) NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_admin_mfa_enrollments_expires_at (expires_at)
);

CREATE TABLE IF NOT EXISTS admin_auth_audit_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  username VARCHAR(128) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  source_ip VARCHAR(128) NULL,
  request_id VARCHAR(128) NULL,
  details JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_admin_auth_audit_logs_user_created (username, created_at)
);

CREATE TABLE IF NOT EXISTS merchant_callback_signing_keys (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  key_id VARCHAR(64) NOT NULL,
  secret_ciphertext TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  is_primary BOOLEAN NOT NULL DEFAULT TRUE,
  previous_expires_at TIMESTAMP NULL,
  revoked_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_merchant_callback_signing_key_id (merchant_id, key_id),
  KEY idx_merchant_callback_signing_key_current (merchant_id, status, is_primary, revoked_at),
  CONSTRAINT fk_merchant_callback_signing_keys_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

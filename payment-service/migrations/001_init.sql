CREATE DATABASE IF NOT EXISTS payment_service CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE payment_service;

CREATE TABLE IF NOT EXISTS merchants (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  code VARCHAR(64) NOT NULL UNIQUE,
  name VARCHAR(255) NOT NULL,
  api_key_hash VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  callback_url VARCHAR(500) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payment_providers (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  code VARCHAR(64) NOT NULL UNIQUE,
  name VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payment_channels (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  provider_id BIGINT NOT NULL,
  code VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  method VARCHAR(64) NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  priority INT NOT NULL DEFAULT 100,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payment_channels_provider_code (provider_id, code),
  CONSTRAINT fk_payment_channels_provider FOREIGN KEY (provider_id) REFERENCES payment_providers(id)
);

CREATE TABLE IF NOT EXISTS merchant_balances (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  available_cents BIGINT NOT NULL DEFAULT 0,
  pending_cents BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_merchant_balances_merchant_currency (merchant_id, currency),
  CONSTRAINT fk_merchant_balances_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

CREATE TABLE IF NOT EXISTS orders (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  channel_id BIGINT NULL,
  order_no VARCHAR(64) NOT NULL UNIQUE,
  merchant_order_no VARCHAR(64) NOT NULL,
  amount_cents BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  item_desc VARCHAR(255) NOT NULL,
  paid_at TIMESTAMP NULL,
  expired_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_orders_merchant_order_no (merchant_id, merchant_order_no),
  KEY idx_orders_status_created_at (status, created_at),
  CONSTRAINT fk_orders_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_orders_channel FOREIGN KEY (channel_id) REFERENCES payment_channels(id)
);

CREATE TABLE IF NOT EXISTS provider_transactions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  order_id BIGINT NOT NULL,
  provider_id BIGINT NOT NULL,
  provider_trade_no VARCHAR(128) NULL,
  provider_order_no VARCHAR(128) NOT NULL,
  request_payload JSON NULL,
  response_payload JSON NULL,
  notify_payload JSON NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  amount_cents BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  paid_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_provider_transactions_provider_order (provider_id, provider_order_no),
  KEY idx_provider_transactions_trade_no (provider_trade_no),
  CONSTRAINT fk_provider_transactions_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_provider_transactions_provider FOREIGN KEY (provider_id) REFERENCES payment_providers(id)
);

CREATE TABLE IF NOT EXISTS provider_callbacks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  provider_id BIGINT NOT NULL,
  order_id BIGINT NULL,
  provider_transaction_id BIGINT NULL,
  provider_trade_no VARCHAR(128) NULL,
  provider_order_no VARCHAR(128) NULL,
  event_type VARCHAR(64) NOT NULL DEFAULT 'payment_notify',
  payload JSON NOT NULL,
  headers JSON NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'received',
  error_message VARCHAR(500) NULL,
  received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  processed_at TIMESTAMP NULL,
  KEY idx_provider_callbacks_provider_order_no (provider_id, provider_order_no),
  KEY idx_provider_callbacks_received_at (received_at),
  CONSTRAINT fk_provider_callbacks_provider FOREIGN KEY (provider_id) REFERENCES payment_providers(id),
  CONSTRAINT fk_provider_callbacks_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_provider_callbacks_transaction FOREIGN KEY (provider_transaction_id) REFERENCES provider_transactions(id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  order_id BIGINT NULL,
  payout_order_id BIGINT NULL,
  provider_transaction_id BIGINT NULL,
  payout_transaction_id BIGINT NULL,
  entry_no VARCHAR(64) NOT NULL UNIQUE,
  direction VARCHAR(16) NOT NULL,
  type VARCHAR(32) NOT NULL,
  amount_cents BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  balance_after_cents BIGINT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_ledger_entries_merchant_created_at (merchant_id, created_at),
  CONSTRAINT fk_ledger_entries_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_ledger_entries_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_ledger_entries_transaction FOREIGN KEY (provider_transaction_id) REFERENCES provider_transactions(id)
);

ALTER TABLE ledger_entries
  ADD COLUMN payout_order_id BIGINT NULL AFTER order_id;

ALTER TABLE ledger_entries
  ADD COLUMN payout_transaction_id BIGINT NULL AFTER provider_transaction_id;

CREATE TABLE IF NOT EXISTS payout_orders (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  payout_no VARCHAR(64) NOT NULL UNIQUE,
  merchant_payout_no VARCHAR(64) NOT NULL,
  provider_code VARCHAR(64) NOT NULL DEFAULT 'ry',
  provider_order_no VARCHAR(128) NULL,
  provider_trade_no VARCHAR(128) NULL,
  amount_cents BIGINT NOT NULL,
  fee_cents BIGINT NOT NULL DEFAULT 0,
  total_debit_cents BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL DEFAULT 'TWD',
  status VARCHAR(32) NOT NULL DEFAULT 'pending_review',
  failure_code VARCHAR(64) NULL,
  failure_message VARCHAR(500) NULL,
  callback_url VARCHAR(500) NULL,
  approved_at TIMESTAMP NULL,
  submitted_at TIMESTAMP NULL,
  completed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payout_orders_merchant_payout_no (merchant_id, merchant_payout_no),
  KEY idx_payout_orders_status_updated_at (status, updated_at),
  CONSTRAINT fk_payout_orders_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

CREATE TABLE IF NOT EXISTS payout_beneficiaries (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  payout_order_id BIGINT NOT NULL,
  pay_account_name VARCHAR(255) NOT NULL,
  pay_card_no VARCHAR(128) NOT NULL,
  pay_bank_name VARCHAR(32) NOT NULL,
  pay_sub_branch VARCHAR(255) NULL,
  pay_sub_branch_code VARCHAR(64) NULL,
  pay_city VARCHAR(128) NULL,
  pay_validate_id VARCHAR(128) NULL,
  pay_currency VARCHAR(32) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payout_beneficiaries_order (payout_order_id),
  CONSTRAINT fk_payout_beneficiaries_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id)
);

CREATE TABLE IF NOT EXISTS payout_transactions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  payout_order_id BIGINT NOT NULL,
  provider_id BIGINT NOT NULL,
  attempt_no INT NOT NULL,
  provider_order_no VARCHAR(128) NULL,
  provider_trade_no VARCHAR(128) NULL,
  request_payload JSON NULL,
  response_payload JSON NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  error_message VARCHAR(500) NULL,
  submitted_at TIMESTAMP NULL,
  completed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payout_transactions_attempt (payout_order_id, attempt_no),
  KEY idx_payout_transactions_provider_order_no (provider_id, provider_order_no),
  CONSTRAINT fk_payout_transactions_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id),
  CONSTRAINT fk_payout_transactions_provider FOREIGN KEY (provider_id) REFERENCES payment_providers(id)
);

CREATE TABLE IF NOT EXISTS payout_callbacks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  provider_id BIGINT NOT NULL,
  payout_order_id BIGINT NOT NULL,
  payout_transaction_id BIGINT NULL,
  provider_order_no VARCHAR(128) NULL,
  provider_trade_no VARCHAR(128) NULL,
  provider_event_key VARCHAR(255) NULL,
  payload JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'received',
  error_message VARCHAR(500) NULL,
  received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  processed_at TIMESTAMP NULL,
  UNIQUE KEY uk_payout_callbacks_provider_event (provider_id, provider_event_key),
  CONSTRAINT fk_payout_callbacks_provider FOREIGN KEY (provider_id) REFERENCES payment_providers(id),
  CONSTRAINT fk_payout_callbacks_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id),
  CONSTRAINT fk_payout_callbacks_transaction FOREIGN KEY (payout_transaction_id) REFERENCES payout_transactions(id)
);

CREATE TABLE IF NOT EXISTS merchant_payout_callback_tasks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  payout_order_id BIGINT NOT NULL,
  callback_url VARCHAR(500) NOT NULL,
  payload JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  retry_count INT NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_error VARCHAR(500) NULL,
  sent_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_merchant_payout_callback_tasks_due (status, next_retry_at),
  CONSTRAINT fk_merchant_payout_callback_tasks_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_merchant_payout_callback_tasks_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id)
);

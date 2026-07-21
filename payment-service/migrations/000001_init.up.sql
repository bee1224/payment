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

CREATE TABLE IF NOT EXISTS merchant_api_keys (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  key_hash CHAR(64) NOT NULL,
  secret_ciphertext TEXT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  is_primary BOOLEAN NOT NULL DEFAULT FALSE,
  last_used_at TIMESTAMP NULL,
  last_rotated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TIMESTAMP NULL,
  revoked_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_merchant_api_keys_hash (merchant_id, key_hash),
  KEY idx_merchant_api_keys_active (merchant_id, status, expires_at, revoked_at),
  CONSTRAINT fk_merchant_api_keys_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

CREATE TABLE IF NOT EXISTS merchant_api_key_audit_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  merchant_api_key_id BIGINT NULL,
  action VARCHAR(32) NOT NULL,
  key_hash CHAR(64) NOT NULL,
  actor VARCHAR(255) NOT NULL,
  reason VARCHAR(500) NULL,
  request_id VARCHAR(128) NULL,
  source_ip VARCHAR(128) NULL,
  user_agent VARCHAR(500) NULL,
  metadata JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_merchant_api_key_audit_logs_merchant_created (merchant_id, created_at),
  KEY idx_merchant_api_key_audit_logs_action (merchant_id, action, created_at),
  CONSTRAINT fk_merchant_api_key_audit_logs_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_merchant_api_key_audit_logs_key FOREIGN KEY (merchant_api_key_id) REFERENCES merchant_api_keys(id)
);

CREATE TABLE IF NOT EXISTS replay_nonces (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  scope VARCHAR(128) NOT NULL,
  nonce_key VARCHAR(255) NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_replay_nonces_scope_key (scope, nonce_key),
  KEY idx_replay_nonces_expires_at (expires_at)
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

CREATE TABLE IF NOT EXISTS merchant_deposit_callback_tasks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  order_id BIGINT NOT NULL,
  callback_url VARCHAR(500) NOT NULL,
  payload JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  retry_count INT NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_error VARCHAR(500) NULL,
  sent_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_merchant_deposit_callback_tasks_due (status, next_retry_at),
  CONSTRAINT fk_merchant_deposit_callback_tasks_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_merchant_deposit_callback_tasks_order FOREIGN KEY (order_id) REFERENCES orders(id)
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
  balance_before_cents BIGINT NULL,
  balance_after_cents BIGINT NULL,
  reference_type VARCHAR(32) NOT NULL,
  reference_id BIGINT NOT NULL,
  source_event VARCHAR(64) NOT NULL,
  reversal_of_entry_id BIGINT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_ledger_entries_merchant_created_at (merchant_id, created_at),
  KEY idx_ledger_entries_reference (reference_type, reference_id, created_at),
  KEY idx_ledger_entries_reversal_of_entry_id (reversal_of_entry_id),
  CONSTRAINT fk_ledger_entries_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_ledger_entries_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_ledger_entries_transaction FOREIGN KEY (provider_transaction_id) REFERENCES provider_transactions(id),
  CONSTRAINT fk_ledger_entries_reversal FOREIGN KEY (reversal_of_entry_id) REFERENCES ledger_entries(id)
);

CREATE TABLE IF NOT EXISTS payout_orders (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  payout_no VARCHAR(64) NOT NULL UNIQUE,
  merchant_payout_no VARCHAR(64) NOT NULL,
  provider_code VARCHAR(64) NOT NULL DEFAULT 'gateway',
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
	KEY idx_payout_transactions_provider_trade_no (provider_trade_no),
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

CREATE TABLE IF NOT EXISTS payout_review_audit_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  payout_order_id BIGINT NOT NULL,
  action VARCHAR(32) NOT NULL,
  actor VARCHAR(255) NOT NULL,
  reason VARCHAR(500) NULL,
  request_id VARCHAR(128) NULL,
  source_ip VARCHAR(128) NULL,
  user_agent VARCHAR(500) NULL,
  metadata JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_payout_review_audit_logs_order_created (payout_order_id, created_at),
  KEY idx_payout_review_audit_logs_actor (actor, created_at),
  CONSTRAINT fk_payout_review_audit_logs_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_payout_review_audit_logs_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id)
);

CREATE TABLE IF NOT EXISTS payout_operational_alerts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  merchant_id BIGINT NOT NULL,
  payout_order_id BIGINT NOT NULL,
  category VARCHAR(64) NOT NULL,
  severity VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'open',
  summary VARCHAR(255) NOT NULL,
  details VARCHAR(1000) NULL,
  occurrence_count INT NOT NULL DEFAULT 1,
  first_occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  resolved_at TIMESTAMP NULL,
  resolved_by VARCHAR(255) NULL,
  resolve_reason VARCHAR(500) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payout_operational_alerts_open (payout_order_id, category, status),
  KEY idx_payout_operational_alerts_status_severity (status, severity, last_occurred_at),
  CONSTRAINT fk_payout_operational_alerts_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_payout_operational_alerts_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id)
);

CREATE TABLE IF NOT EXISTS reconciliation_runs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  scope_type VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'running',
  merchant_code VARCHAR(64) NULL,
  order_no VARCHAR(64) NULL,
  payout_no VARCHAR(64) NULL,
  mismatch_count INT NOT NULL DEFAULT 0,
  started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  KEY idx_reconciliation_runs_started_at (started_at),
  KEY idx_reconciliation_runs_scope (scope_type, status, started_at)
);

CREATE TABLE IF NOT EXISTS reconciliation_run_items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  run_id BIGINT NOT NULL,
  mismatch_type VARCHAR(64) NOT NULL,
  merchant_id BIGINT NULL,
  merchant_code VARCHAR(64) NULL,
  entity_type VARCHAR(32) NOT NULL,
  entity_id BIGINT NULL,
  order_no VARCHAR(64) NULL,
  payout_no VARCHAR(64) NULL,
  table_name VARCHAR(64) NOT NULL,
  field_name VARCHAR(64) NOT NULL,
  expected_value VARCHAR(255) NULL,
  actual_value VARCHAR(255) NULL,
  details VARCHAR(1000) NULL,
  resolution_status VARCHAR(32) NOT NULL DEFAULT 'open',
  resolution_type VARCHAR(32) NULL,
  resolution_note VARCHAR(500) NULL,
  resolution_ledger_entry_id BIGINT NULL,
  resolved_at TIMESTAMP NULL,
  resolved_by VARCHAR(255) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_reconciliation_run_items_run_id (run_id, mismatch_type),
  KEY idx_reconciliation_run_items_lookup (merchant_code, order_no, payout_no),
	KEY idx_reconciliation_run_items_entity (entity_type, entity_id, created_at),
  KEY idx_reconciliation_run_items_resolution (resolution_status, resolved_at),
  CONSTRAINT fk_reconciliation_run_items_run FOREIGN KEY (run_id) REFERENCES reconciliation_runs(id),
  CONSTRAINT fk_reconciliation_run_items_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_reconciliation_run_items_resolution_ledger FOREIGN KEY (resolution_ledger_entry_id) REFERENCES ledger_entries(id)
);

CREATE TABLE IF NOT EXISTS reconciliation_resolution_actions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  run_id BIGINT NOT NULL,
  reconciliation_item_id BIGINT NOT NULL,
  merchant_id BIGINT NULL,
  action_type VARCHAR(32) NOT NULL,
  ledger_entry_id BIGINT NULL,
  actor VARCHAR(255) NOT NULL,
  checker VARCHAR(255) NOT NULL,
  reason VARCHAR(500) NOT NULL,
  request_id VARCHAR(128) NOT NULL,
  source_ip VARCHAR(128) NULL,
  user_agent VARCHAR(500) NULL,
  metadata JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_reconciliation_resolution_actions_item_created (reconciliation_item_id, created_at),
  KEY idx_reconciliation_resolution_actions_actor_created (actor, created_at),
  CONSTRAINT fk_reconciliation_resolution_actions_run FOREIGN KEY (run_id) REFERENCES reconciliation_runs(id),
  CONSTRAINT fk_reconciliation_resolution_actions_item FOREIGN KEY (reconciliation_item_id) REFERENCES reconciliation_run_items(id),
  CONSTRAINT fk_reconciliation_resolution_actions_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id),
  CONSTRAINT fk_reconciliation_resolution_actions_ledger FOREIGN KEY (ledger_entry_id) REFERENCES ledger_entries(id)
);

-- Manual payout operations are deliberately separate from the gateway payout
-- dispatch queue. Their statuses are uppercase to make accidental use by the
-- existing gateway reconcile loop impossible.
CREATE TABLE IF NOT EXISTS manual_payout_cases (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  payout_order_id BIGINT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
  operator_id VARCHAR(255) NULL,
  confirmed_by VARCHAR(255) NULL,
  confirmed_at TIMESTAMP NULL,
  failure_reason VARCHAR(500) NULL,
  version INT NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_manual_payout_cases_order (payout_order_id),
  KEY idx_manual_payout_cases_status_updated (status, updated_at),
  CONSTRAINT fk_manual_payout_cases_order FOREIGN KEY (payout_order_id) REFERENCES payout_orders(id)
);

CREATE TABLE IF NOT EXISTS payout_receipts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  manual_payout_case_id BIGINT NOT NULL,
  storage_key VARCHAR(255) NOT NULL UNIQUE,
  original_filename VARCHAR(255) NOT NULL,
  content_type VARCHAR(128) NOT NULL,
  size_bytes BIGINT NOT NULL,
  sha256 CHAR(64) NOT NULL,
  uploaded_by VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_payout_receipts_case_hash (manual_payout_case_id, sha256),
  CONSTRAINT fk_payout_receipts_case FOREIGN KEY (manual_payout_case_id) REFERENCES manual_payout_cases(id)
);

CREATE TABLE IF NOT EXISTS payout_status_history (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  manual_payout_case_id BIGINT NOT NULL,
  from_status VARCHAR(32) NULL,
  to_status VARCHAR(32) NOT NULL,
  actor VARCHAR(255) NOT NULL,
  reason VARCHAR(500) NULL,
  request_id VARCHAR(128) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_payout_status_history_case_created (manual_payout_case_id, created_at),
  CONSTRAINT fk_payout_status_history_case FOREIGN KEY (manual_payout_case_id) REFERENCES manual_payout_cases(id)
);

CREATE TABLE IF NOT EXISTS payout_operation_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  manual_payout_case_id BIGINT NOT NULL,
  action VARCHAR(64) NOT NULL,
  actor VARCHAR(255) NOT NULL,
  request_id VARCHAR(128) NOT NULL,
  source_ip VARCHAR(128) NULL,
  user_agent VARCHAR(500) NULL,
  details JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_payout_operation_logs_case_created (manual_payout_case_id, created_at),
  CONSTRAINT fk_payout_operation_logs_case FOREIGN KEY (manual_payout_case_id) REFERENCES manual_payout_cases(id)
);

CREATE TABLE IF NOT EXISTS callback_jobs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  manual_payout_case_id BIGINT NOT NULL,
  idempotency_key VARCHAR(255) NOT NULL UNIQUE,
  callback_url VARCHAR(500) NOT NULL,
  payload JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  attempt_count INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  locked_at TIMESTAMP NULL,
  locked_by VARCHAR(128) NULL,
  last_error VARCHAR(1000) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_callback_jobs_due (status, next_attempt_at),
  CONSTRAINT fk_callback_jobs_case FOREIGN KEY (manual_payout_case_id) REFERENCES manual_payout_cases(id)
);

CREATE TABLE IF NOT EXISTS callback_attempts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  callback_job_id BIGINT NOT NULL,
  request_body JSON NOT NULL,
  response_status INT NULL,
  response_body TEXT NULL,
  error_message VARCHAR(1000) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_callback_attempts_job_created (callback_job_id, created_at),
  CONSTRAINT fk_callback_attempts_job FOREIGN KEY (callback_job_id) REFERENCES callback_jobs(id)
);

CREATE TABLE IF NOT EXISTS admin_users (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  username VARCHAR(128) NOT NULL UNIQUE,
  password_hash VARCHAR(500) NOT NULL,
  role VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_admin_users_status (status, role)
);

-- Coordinates background jobs across API replicas. A holder must renew its
-- short lease before doing work, and a crashed replica is automatically replaced.
CREATE TABLE IF NOT EXISTS worker_leases (
  name VARCHAR(128) PRIMARY KEY,
  holder VARCHAR(128) NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_worker_leases_expires_at (expires_at)
);

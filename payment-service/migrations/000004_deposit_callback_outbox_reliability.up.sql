-- Reliable, idempotent deposit callback outbox.  Existing rows retain their
-- history and receive a legacy key before the uniqueness constraint is added.
ALTER TABLE merchant_deposit_callback_tasks
  ADD COLUMN event_key VARCHAR(191) NULL AFTER order_id;

UPDATE merchant_deposit_callback_tasks t
LEFT JOIN orders o ON o.id = t.order_id
SET t.event_key = CASE
  WHEN o.order_no IS NOT NULL AND o.order_no <> ''
    THEN CONCAT('legacy:merchant.deposit:', o.order_no, ':', t.id)
  ELSE CONCAT('legacy:merchant.deposit:', t.id)
END
WHERE t.event_key IS NULL OR t.event_key = '';

-- Fail the migration rather than silently merging historical tasks if a
-- future manual change has introduced duplicate keys.
ALTER TABLE merchant_deposit_callback_tasks
  MODIFY COLUMN event_key VARCHAR(191) NOT NULL,
  ADD UNIQUE KEY uk_merchant_deposit_callback_tasks_event_key (event_key),
  ADD KEY idx_merchant_deposit_callback_tasks_event_status (event_key, status);

CREATE TABLE merchant_deposit_callback_attempts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id BIGINT NOT NULL,
  attempt_no INT NOT NULL,
  stage VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TIMESTAMP NULL,
  http_status INT NULL,
  response_body_summary VARCHAR(512) NULL,
  error_code VARCHAR(64) NULL,
  elapsed_ms BIGINT NULL,
  next_retry_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_merchant_deposit_callback_attempts_task_no (task_id, attempt_no),
  KEY idx_merchant_deposit_callback_attempts_task_created (task_id, created_at),
  KEY idx_merchant_deposit_callback_attempts_status_retry (status, next_retry_at),
  CONSTRAINT fk_merchant_deposit_callback_attempts_task FOREIGN KEY (task_id) REFERENCES merchant_deposit_callback_tasks(id)
);

-- 000004 may already have been applied.  Additive migration only: preserve
-- existing tasks and make lease expiry explicit for the outbox worker.
ALTER TABLE merchant_deposit_callback_tasks
  ADD COLUMN claim_expires_at TIMESTAMP NULL AFTER claimed_at,
  ADD COLUMN attempt_count INT NOT NULL DEFAULT 0 AFTER retry_count,
  ADD KEY idx_merchant_deposit_callback_tasks_due_lease (status, next_retry_at, claim_expires_at);

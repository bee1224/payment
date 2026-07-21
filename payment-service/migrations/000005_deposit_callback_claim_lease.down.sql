ALTER TABLE merchant_deposit_callback_tasks
  DROP KEY idx_merchant_deposit_callback_tasks_due_lease,
  DROP COLUMN attempt_count,
  DROP COLUMN claim_expires_at;

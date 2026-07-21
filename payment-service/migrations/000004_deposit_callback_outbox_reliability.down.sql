DROP TABLE IF EXISTS merchant_deposit_callback_attempts;
ALTER TABLE merchant_deposit_callback_tasks
  DROP INDEX uk_merchant_deposit_callback_tasks_event_key,
  DROP INDEX idx_merchant_deposit_callback_tasks_event_status,
  DROP COLUMN event_key;

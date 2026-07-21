ALTER TABLE merchant_deposit_callback_tasks
  ADD COLUMN claim_token VARCHAR(64) NULL AFTER last_error,
  ADD COLUMN claimed_at TIMESTAMP NULL AFTER claim_token,
  ADD KEY idx_merchant_deposit_callback_tasks_claim (status, claimed_at);

ALTER TABLE merchant_payout_callback_tasks
  ADD COLUMN claim_token VARCHAR(64) NULL AFTER last_error,
  ADD COLUMN claimed_at TIMESTAMP NULL AFTER claim_token,
  ADD KEY idx_merchant_payout_callback_tasks_claim (status, claimed_at);

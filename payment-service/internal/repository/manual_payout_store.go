package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"payment-service/internal/domain"
)

// ManualPayoutStore owns only the isolated manual workflow. It never calls a
// provider client and uses upper-case status values, so the gateway reconcile
// loop cannot pick up a manually handled order.
type ManualPayoutStore struct{ db *sql.DB }

func NewManualPayoutStore(db *sql.DB) *ManualPayoutStore { return &ManualPayoutStore{db: db} }

func (s *ManualPayoutStore) Start(ctx context.Context, payoutNo, actor, requestID string) (domain.ManualPayoutCase, error) {
	return s.transition(ctx, payoutNo, domain.ManualPayoutPending, domain.ManualPayoutProcessing, actor, requestID, "start_processing", "")
}

func (s *ManualPayoutStore) Find(ctx context.Context, payoutNo string) (domain.ManualPayoutCase, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	return caseRow, nil
}

func (s *ManualPayoutStore) AttachReceipt(ctx context.Context, payoutNo string, receipt domain.PayoutReceipt, requestID string) (domain.ManualPayoutCase, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if caseRow.Status != domain.ManualPayoutProcessing {
		return domain.ManualPayoutCase{}, fmt.Errorf("manual payout must be PROCESSING before a receipt can be uploaded")
	}
	if receipt.UploadedBy != caseRow.OperatorID {
		return domain.ManualPayoutCase{}, errors.New("receipt uploader must be the assigned operator")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO payout_receipts (manual_payout_case_id,storage_key,original_filename,content_type,size_bytes,sha256,uploaded_by) VALUES (?,?,?,?,?,?,?)`, caseRow.ID, receipt.StorageKey, receipt.OriginalFilename, receipt.ContentType, receipt.SizeBytes, receipt.SHA256, receipt.UploadedBy)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	receipt.ID, _ = result.LastInsertId()
	if err := updateManualStatus(ctx, tx, caseRow, domain.ManualPayoutPendingReview, receipt.UploadedBy, requestID, "receipt_uploaded", ""); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow.Status = domain.ManualPayoutPendingReview
	caseRow.Version++
	return caseRow, nil
}

func (s *ManualPayoutStore) Confirm(ctx context.Context, payoutNo, reviewer, requestID string) (domain.ManualPayoutCase, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if caseRow.Status != domain.ManualPayoutPendingReview {
		return domain.ManualPayoutCase{}, errors.New("manual payout is not awaiting review")
	}
	if reviewer == "" || reviewer == caseRow.OperatorID {
		return domain.ManualPayoutCase{}, errors.New("reviewer must be different from the operator")
	}
	var receiptCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payout_receipts WHERE manual_payout_case_id=?`, caseRow.ID).Scan(&receiptCount); err != nil || receiptCount == 0 {
		if err != nil {
			return domain.ManualPayoutCase{}, err
		}
		return domain.ManualPayoutCase{}, errors.New("a receipt is required before confirmation")
	}
	now := time.Now().UTC()
	if err := updateManualStatus(ctx, tx, caseRow, domain.ManualPayoutSuccess, reviewer, requestID, "confirmed_success", ""); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE manual_payout_cases SET confirmed_by=?,confirmed_at=? WHERE id=?`, reviewer, now, caseRow.ID); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	order, err := manualPayoutOrderForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := finalizePayoutHoldTx(ctx, tx, order, "", "", now); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	var callbackURL, merchantPayoutNo string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(callback_url,''),merchant_payout_no FROM payout_orders WHERE id=?`, caseRow.PayoutOrderID).Scan(&callbackURL, &merchantPayoutNo); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if strings.TrimSpace(callbackURL) != "" {
		payload := fmt.Sprintf(`{"payout_no":%q,"merchant_payout_no":%q,"status":"SUCCESS","completed_at":%q}`, payoutNo, merchantPayoutNo, now.Format(time.RFC3339))
		_, err = tx.ExecContext(ctx, `INSERT INTO callback_jobs (manual_payout_case_id,idempotency_key,callback_url,payload) VALUES (?,?,?,?)`, caseRow.ID, "manual-success:"+payoutNo, callbackURL, payload)
		if err != nil {
			return domain.ManualPayoutCase{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow.Status = domain.ManualPayoutSuccess
	caseRow.ConfirmedBy = reviewer
	caseRow.ConfirmedAt = &now
	return caseRow, nil
}

func (s *ManualPayoutStore) Fail(ctx context.Context, payoutNo, reviewer, reason, requestID string) (domain.ManualPayoutCase, error) {
	if strings.TrimSpace(reason) == "" {
		return domain.ManualPayoutCase{}, errors.New("failure reason is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if caseRow.Status != domain.ManualPayoutPendingReview {
		return domain.ManualPayoutCase{}, errors.New("manual payout is not awaiting review")
	}
	if reviewer == "" || reviewer == caseRow.OperatorID {
		return domain.ManualPayoutCase{}, errors.New("reviewer must be different from the operator")
	}
	if err := updateManualStatus(ctx, tx, caseRow, domain.ManualPayoutFailed, reviewer, requestID, "confirmed_failed", reason); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	order, err := manualPayoutOrderForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := releasePayoutHoldTx(ctx, tx, order, string(domain.PayoutOrderStatusFailed), reason, 0); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow.Status = domain.ManualPayoutFailed
	caseRow.FailureReason = reason
	return caseRow, nil
}

func (s *ManualPayoutStore) Cancel(ctx context.Context, payoutNo, actor, reason, requestID string) (domain.ManualPayoutCase, error) {
	if strings.TrimSpace(reason) == "" {
		return domain.ManualPayoutCase{}, errors.New("cancellation reason is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if caseRow.Status != domain.ManualPayoutProcessing {
		return domain.ManualPayoutCase{}, errors.New("only a PROCESSING manual payout without a receipt can be cancelled")
	}
	var receipts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payout_receipts WHERE manual_payout_case_id=?`, caseRow.ID).Scan(&receipts); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if receipts != 0 {
		return domain.ManualPayoutCase{}, errors.New("a receipt exists; use reviewer failure instead of cancellation")
	}
	if err := updateManualStatus(ctx, tx, caseRow, domain.ManualPayoutCancelled, actor, requestID, "cancelled", reason); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	order, err := manualPayoutOrderForUpdate(ctx, tx, payoutNo)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := releasePayoutHoldTx(ctx, tx, order, string(domain.PayoutOrderStatusCancelled), reason, 0); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow.Status = domain.ManualPayoutCancelled
	return caseRow, nil
}

func (s *ManualPayoutStore) FindReceipt(ctx context.Context, payoutNo string, receiptID int64) (domain.PayoutReceipt, error) {
	var r domain.PayoutReceipt
	err := s.db.QueryRowContext(ctx, `SELECT r.id,r.manual_payout_case_id,r.storage_key,r.original_filename,r.content_type,r.size_bytes,r.sha256,r.uploaded_by,r.created_at FROM payout_receipts r JOIN manual_payout_cases c ON c.id=r.manual_payout_case_id JOIN payout_orders o ON o.id=c.payout_order_id WHERE o.payout_no=? AND r.id=?`, payoutNo, receiptID).Scan(&r.ID, &r.ManualPayoutCaseID, &r.StorageKey, &r.OriginalFilename, &r.ContentType, &r.SizeBytes, &r.SHA256, &r.UploadedBy, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *ManualPayoutStore) FindLatestReceipt(ctx context.Context, payoutNo string) (domain.PayoutReceipt, error) {
	var r domain.PayoutReceipt
	err := s.db.QueryRowContext(ctx, `SELECT r.id,r.manual_payout_case_id,r.storage_key,r.original_filename,r.content_type,r.size_bytes,r.sha256,r.uploaded_by,r.created_at FROM payout_receipts r JOIN manual_payout_cases c ON c.id=r.manual_payout_case_id JOIN payout_orders o ON o.id=c.payout_order_id WHERE o.payout_no=? ORDER BY r.id DESC LIMIT 1`, payoutNo).Scan(&r.ID, &r.ManualPayoutCaseID, &r.StorageKey, &r.OriginalFilename, &r.ContentType, &r.SizeBytes, &r.SHA256, &r.UploadedBy, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *ManualPayoutStore) ListCallbackAttempts(ctx context.Context, payoutNo string) ([]domain.ManualCallbackAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.request_body,COALESCE(a.response_status,0),COALESCE(a.response_body,''),COALESCE(a.error_message,''),a.created_at FROM callback_attempts a JOIN callback_jobs j ON j.id=a.callback_job_id JOIN manual_payout_cases c ON c.id=j.manual_payout_case_id JOIN payout_orders o ON o.id=c.payout_order_id WHERE o.payout_no=? ORDER BY a.id DESC`, payoutNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ManualCallbackAttempt
	for rows.Next() {
		var item domain.ManualCallbackAttempt
		if err := rows.Scan(&item.RequestBody, &item.ResponseStatus, &item.ResponseBody, &item.ErrorMessage, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *ManualPayoutStore) RetryCallback(ctx context.Context, payoutNo string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE callback_jobs j JOIN manual_payout_cases c ON c.id=j.manual_payout_case_id JOIN payout_orders o ON o.id=c.payout_order_id SET j.status='pending',j.next_attempt_at=CURRENT_TIMESTAMP,j.locked_at=NULL,j.locked_by=NULL,j.last_error=NULL WHERE o.payout_no=? AND j.status IN ('failed','retrying')`, payoutNo)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ManualPayoutStore) transition(ctx context.Context, payoutNo string, from, to domain.ManualPayoutStatus, actor, requestID, action, reason string) (domain.ManualPayoutCase, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(requestID) == "" {
		return domain.ManualPayoutCase{}, errors.New("actor and request ID are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	defer tx.Rollback()
	caseRow, err := manualCaseForUpdate(ctx, tx, payoutNo)
	if errors.Is(err, ErrNotFound) && from == domain.ManualPayoutPending {
		var orderID int64
		var orderStatus string
		err = tx.QueryRowContext(ctx, `SELECT id,status FROM payout_orders WHERE payout_no=? FOR UPDATE`, payoutNo).Scan(&orderID, &orderStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ManualPayoutCase{}, ErrNotFound
		}
		if err != nil {
			return domain.ManualPayoutCase{}, err
		}
		if orderStatus != "pending_review" {
			return domain.ManualPayoutCase{}, errors.New("only a pending-review payout can enter the manual workflow")
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO manual_payout_cases (payout_order_id,status,operator_id) VALUES (?,?,?)`, orderID, from, actor)
		if err != nil {
			return domain.ManualPayoutCase{}, err
		}
		id, _ := result.LastInsertId()
		caseRow = domain.ManualPayoutCase{ID: id, PayoutOrderID: orderID, PayoutNo: payoutNo, Status: from, OperatorID: actor, Version: 1}
	} else if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if caseRow.Status != from {
		return domain.ManualPayoutCase{}, fmt.Errorf("invalid manual payout transition from %s", caseRow.Status)
	}
	if err := updateManualStatus(ctx, tx, caseRow, to, actor, requestID, action, reason); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payout_orders SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(to), caseRow.PayoutOrderID); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow.Status = to
	caseRow.Version++
	return caseRow, nil
}

func manualCaseForUpdate(ctx context.Context, tx *sql.Tx, payoutNo string) (domain.ManualPayoutCase, error) {
	var c domain.ManualPayoutCase
	var confirmed sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT c.id,c.payout_order_id,o.payout_no,c.status,COALESCE(c.operator_id,''),COALESCE(c.confirmed_by,''),c.confirmed_at,COALESCE(c.failure_reason,''),c.version,c.created_at,c.updated_at FROM manual_payout_cases c JOIN payout_orders o ON o.id=c.payout_order_id WHERE o.payout_no=? FOR UPDATE`, payoutNo).Scan(&c.ID, &c.PayoutOrderID, &c.PayoutNo, &c.Status, &c.OperatorID, &c.ConfirmedBy, &confirmed, &c.FailureReason, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if confirmed.Valid {
		c.ConfirmedAt = &confirmed.Time
	}
	return c, err
}

func manualPayoutOrderForUpdate(ctx context.Context, tx *sql.Tx, payoutNo string) (domain.PayoutOrder, error) {
	row := tx.QueryRowContext(ctx, payoutOrderSelectQuery()+` WHERE po.payout_no = ? FOR UPDATE`, payoutNo)
	return scanPayoutOrderFromRow(row)
}
func updateManualStatus(ctx context.Context, tx *sql.Tx, c domain.ManualPayoutCase, to domain.ManualPayoutStatus, actor, requestID, action, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE manual_payout_cases SET status=?,version=version+1 WHERE id=? AND version=?`, to, c.ID, c.Version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payout_status_history (manual_payout_case_id,from_status,to_status,actor,reason,request_id) VALUES (?,?,?,?,?,?)`, c.ID, c.Status, to, actor, nullableString(reason), requestID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO payout_operation_logs (manual_payout_case_id,action,actor,request_id) VALUES (?,?,?,?)`, c.ID, action, actor, requestID)
	return err
}

func (s *ManualPayoutStore) ClaimDueCallbackJobs(ctx context.Context, workerID string, limit int) ([]domain.ManualCallbackJob, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT j.id,j.manual_payout_case_id,m.code,j.callback_url,CAST(j.payload AS CHAR),j.attempt_count,j.idempotency_key FROM callback_jobs j JOIN manual_payout_cases c ON c.id=j.manual_payout_case_id JOIN payout_orders o ON o.id=c.payout_order_id JOIN merchants m ON m.id=o.merchant_id WHERE j.status IN ('pending','retrying') AND j.next_attempt_at<=CURRENT_TIMESTAMP AND (j.locked_at IS NULL OR j.locked_at < DATE_SUB(CURRENT_TIMESTAMP,INTERVAL 5 MINUTE)) ORDER BY j.next_attempt_at ASC LIMIT ? FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.ManualCallbackJob
	for rows.Next() {
		var job domain.ManualCallbackJob
		if err := rows.Scan(&job.ID, &job.ManualCaseID, &job.MerchantCode, &job.CallbackURL, &job.Payload, &job.AttemptCount, &job.IdempotencyKey); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if _, err := tx.ExecContext(ctx, `UPDATE callback_jobs SET status='processing',locked_at=CURRENT_TIMESTAMP,locked_by=? WHERE id=?`, workerID, job.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *ManualPayoutStore) FinishCallbackJob(ctx context.Context, jobID int64, success bool, exhausted bool, next time.Time, attempt domain.ManualCallbackAttempt) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO callback_attempts (callback_job_id,request_body,response_status,response_body,error_message) VALUES (?,?,?,?,?)`, jobID, attempt.RequestBody, nullableInt(attempt.ResponseStatus), nullableString(attempt.ResponseBody), nullableString(attempt.ErrorMessage))
	if err != nil {
		return err
	}
	if success {
		_, err = tx.ExecContext(ctx, `UPDATE callback_jobs SET status='sent',attempt_count=attempt_count+1,locked_at=NULL,locked_by=NULL,last_error=NULL WHERE id=?`, jobID)
	} else {
		status := "retrying"
		if exhausted {
			status = "failed"
		}
		_, err = tx.ExecContext(ctx, `UPDATE callback_jobs SET status=?,attempt_count=attempt_count+1,next_attempt_at=?,locked_at=NULL,locked_by=NULL,last_error=? WHERE id=?`, status, next, nullableString(attempt.ErrorMessage), jobID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

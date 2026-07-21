import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import * as api from '../api/payoutApi'
import { useAuth } from '../contexts/AuthContext'
import { isAllowedReceipt, payoutStatusLabel } from '../utils/payoutPresentation'
import { formatDateTime } from '../utils/operationPresentation'
import { userFacingError } from '../api/httpClient'

const dateTime = formatDateTime

function DetailField({ label, children }) {
  return <div><small>{label}</small><strong>{children || '—'}</strong></div>
}

export default function PayoutDetailPage() {
  const { payoutNo } = useParams()
  const queryClient = useQueryClient()
  const { user } = useAuth()
  const [file, setFile] = useState()
  const [fileError, setFileError] = useState('')
  const [dialog, setDialog] = useState(null)
  const [reason, setReason] = useState('')
  const [actionError, setActionError] = useState('')
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['payout', payoutNo] })
  const { data, isLoading, isError, error, refetch } = useQuery({ queryKey: ['payout', payoutNo], queryFn: () => api.getPayout(payoutNo) })
  const callbacks = useQuery({ queryKey: ['payout-callbacks', payoutNo], queryFn: () => api.listCallbackAttempts(payoutNo), enabled: !!data, refetchInterval: 30000, refetchIntervalInBackground: false })
  const afterSuccess = () => { setDialog(null); setReason(''); setActionError(''); refresh() }
  const showError = (error) => setActionError(userFacingError(error))
  const start = useMutation({ mutationFn: () => api.startProcessing(payoutNo), onSuccess: afterSuccess, onError: showError })
  const upload = useMutation({ mutationFn: () => api.uploadReceipt(payoutNo, file), onSuccess: afterSuccess, onError: showError })
  const confirm = useMutation({ mutationFn: () => api.confirmSuccess(payoutNo), onSuccess: afterSuccess, onError: showError })
  const fail = useMutation({ mutationFn: () => api.markFailed(payoutNo, reason.trim()), onSuccess: afterSuccess, onError: showError })
  const cancel = useMutation({ mutationFn: () => api.cancelPayout(payoutNo, reason.trim()), onSuccess: afterSuccess, onError: showError })
  const retryCallback = useMutation({ mutationFn: () => api.retryCallback(payoutNo), onSuccess: () => { afterSuccess(); queryClient.invalidateQueries({ queryKey: ['payout-callbacks', payoutNo] }) }, onError: showError })

  if (isLoading) return <p className="empty" aria-live="polite">明細讀取中…</p>
  if (isError) return <section className="panel detail-panel"><p className="empty error">無法載入案件明細。{error?.response?.status === 404 ? '此代付單不存在。' : ''}</p><button className="outline-button" onClick={() => refetch()}>重新整理</button></section>

  const payout = data.payout
  const manual = data.manual_case
  const operator = user.role === 'OPERATOR' || user.role === 'ADMIN'
  const reviewer = user.role === 'REVIEWER' || user.role === 'ADMIN'
  const history = [
    { action: '建立代付單', actor: payout.merchant_id, created_at: payout.created_at },
    ...(data.audit_logs || []),
  ].sort((a, b) => new Date(a.created_at) - new Date(b.created_at))
  const selectFile = (nextFile) => {
    setFileError('')
    if (!nextFile) { setFile(undefined); return }
    const allowed = ['application/pdf', 'image/jpeg', 'image/png', 'image/webp']
    if (!allowed.includes(nextFile.type) || !isAllowedReceipt(nextFile)) { setFile(undefined); setFileError('僅接受 JPEG、PNG、WebP 或 PDF，且檔案不得超過 10MB。'); return }
    setFile(nextFile)
  }
  const runDialogAction = () => {
    if ((dialog === 'fail' || dialog === 'cancel' || dialog === 'retry-callback') && !reason.trim()) { setActionError('請輸入原因。'); return }
    if (dialog === 'start') start.mutate()
    if (dialog === 'confirm') confirm.mutate()
    if (dialog === 'fail') fail.mutate()
    if (dialog === 'cancel') cancel.mutate()
    if (dialog === 'retry-callback') retryCallback.mutate()
  }
  const actionPending = start.isPending || confirm.isPending || fail.isPending || cancel.isPending || retryCallback.isPending
  const callbackAttempts = callbacks.data || []

  return <section className="panel detail-panel">
    <div className="detail-title"><div><small>系統單號</small><h2>{payout.payout_no}</h2></div><span className={`status status-${payout.status?.toLowerCase()}`}>{payoutStatusLabel(payout.status)}</span></div>
    <div className="detail-grid">
      <DetailField label="金額">{payout.amount} {payout.currency}</DetailField>
      <DetailField label="商戶號">{payout.merchant_id}</DetailField>
      <DetailField label="商戶代付單號">{payout.merchant_payout_no}</DetailField>
      <DetailField label="建立時間">{dateTime(payout.created_at)}</DetailField>
      <DetailField label="最後更新">{dateTime(payout.updated_at)}</DetailField>
      <DetailField label="人工處理狀態">{payoutStatusLabel(manual?.Status || manual?.status)}</DetailField>
    </div>
    {manual && <div className="detail-grid detail-subgrid">
      <DetailField label="處理人">{manual.OperatorID || manual.operator_id}</DetailField>
      <DetailField label="覆核人">{manual.ConfirmedBy || manual.confirmed_by}</DetailField>
      <DetailField label="覆核時間">{dateTime(manual.ConfirmedAt || manual.confirmed_at)}</DetailField>
      <DetailField label="失敗／取消原因">{manual.FailureReason || manual.failure_reason}</DetailField>
    </div>}
    <div className="action-row">
      {operator && !manual && <button className="search-button" disabled={actionPending} onClick={() => setDialog('start')}>開始處理</button>}
      {operator && (manual?.Status || manual?.status) === 'PROCESSING' && <><input type="file" accept="application/pdf,image/jpeg,image/png,image/webp" onChange={e => selectFile(e.target.files[0])} /><button className="search-button" disabled={!file || upload.isPending} onClick={() => upload.mutate()}>{upload.isPending ? '上傳中…' : '上傳收據'}</button><button className="outline-button" onClick={() => setDialog('cancel')}>取消</button>{fileError && <span className="field-error">{fileError}</span>}<a className="detail-link" href={api.receiptDownloadUrl(payoutNo)}>下載最新憑證</a></>}
      {reviewer && (manual?.Status || manual?.status) === 'PAID_PENDING_REVIEW' && <><button className="search-button" disabled={actionPending} onClick={() => setDialog('confirm')}>確認成功</button><button className="outline-button" disabled={actionPending} onClick={() => setDialog('fail')}>標記失敗</button><a className="detail-link" href={api.receiptDownloadUrl(payoutNo)}>下載憑證</a></>}
    </div>
    <h3>操作時間線</h3>
    <ol className="timeline">{history.map((entry, index) => <li key={`${entry.created_at}-${index}`}><time>{dateTime(entry.created_at)}</time><div><strong>{entry.action}</strong><span>操作人：{entry.actor || '系統'}</span>{entry.reason && <span>原因：{entry.reason}</span>}{entry.request_id && <span>Request ID：{entry.request_id}</span>}</div></li>)}</ol>
    <section className="callback-panel"><div className="callback-heading"><h3>商戶 Callback</h3>{reviewer && <button className="outline-button" disabled={actionPending} onClick={() => setDialog('retry-callback')}>手動重送</button>}</div>{callbacks.isLoading ? <p>Callback 紀錄讀取中…</p> : callbacks.isError ? <p className="field-error">無法讀取 Callback 紀錄。</p> : callbackAttempts.length ? <table className="callback-table"><thead><tr><th>時間</th><th>結果</th><th>HTTP 狀態</th><th>說明</th></tr></thead><tbody>{callbackAttempts.map((attempt, index) => <tr key={`${attempt.created_at}-${index}`}><td>{dateTime(attempt.created_at)}</td><td>{attempt.error_message ? '失敗' : '已送出'}</td><td>{attempt.response_status || '—'}</td><td>{attempt.error_message || (attempt.response_status >= 200 && attempt.response_status < 300 ? '商戶已回應成功。' : '商戶回應未成功。')}</td></tr>)}</tbody></table> : <p>尚無 Callback 發送紀錄。</p>}</section>
    {dialog && <div className="dialog-backdrop" role="presentation"><section className="dialog" role="dialog" aria-modal="true" aria-labelledby="action-dialog-title"><h3 id="action-dialog-title">{({ start: '確認開始處理', confirm: '確認代付成功', fail: '確認標記失敗', cancel: '確認取消案件', 'retry-callback': '確認手動重送 Callback' })[dialog]}</h3><p>代付單號：{payout.payout_no}；金額：{payout.amount} {payout.currency}</p>{(dialog === 'fail' || dialog === 'cancel' || dialog === 'retry-callback') && <label>{dialog === 'retry-callback' ? '重送原因（前端驗證）' : '原因'}<textarea value={reason} maxLength="300" onChange={e => { setReason(e.target.value); setActionError('') }} placeholder="請說明原因" /></label>}{actionError && <p className="field-error" role="alert">{actionError}</p>}<div className="dialog-actions"><button className="outline-button" disabled={actionPending} onClick={() => { setDialog(null); setReason(''); setActionError('') }}>返回</button><button className="search-button" disabled={actionPending} onClick={runDialogAction}>{actionPending ? '送出中…' : '確認送出'}</button></div></section></div>}
  </section>
}

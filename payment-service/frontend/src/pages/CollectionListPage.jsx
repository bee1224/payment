import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { listCollections } from '../api/collectionApi'
import OperationsListHeader from '../components/OperationsListHeader'
import { formatAmount, formatDateTime, maskAccount, statusMeta } from '../utils/operationPresentation'
import './OperationsList.css'

export default function CollectionListPage() {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const { data = [], isLoading, isFetching, error, refetch } = useQuery({ queryKey: ['collections'], queryFn: listCollections })
  const rows = useMemo(() => data.filter(row => `${row.order_no || ''} ${row.merchant_order_no || ''} ${row.merchant_code || ''}`.toLowerCase().includes(query.toLowerCase()) && (!status || String(row.status || '').toUpperCase() === status)), [data, query, status])
  const pending = rows.filter(row => String(row.status || '').toUpperCase() === 'PENDING').length
  const success = rows.filter(row => ['PAID', 'SUCCESS'].includes(String(row.status || '').toUpperCase())).length

  return <section className="panel operations-panel">
    <OperationsListHeader title="代收訂單" description="快速查找目前載入的代收訂單；帳號資訊已遮罩，需進一步處理請依授權流程操作。" total={rows.length} isRefreshing={isFetching} onRefresh={refetch}>
      <div className="operations-filterbar">
        <input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜尋系統單號、商戶單號或商戶號" aria-label="搜尋代收訂單" />
        <select value={status} onChange={event => setStatus(event.target.value)} aria-label="代收狀態"><option value="">全部狀態</option><option value="PENDING">待付款</option><option value="PAID">已付款</option><option value="SUCCESS">成功</option><option value="FAILED">失敗</option><option value="EXPIRED">已逾期</option></select>
        <div aria-hidden="true" />
        <button type="button" className="operations-search" onClick={() => refetch()}>重新查詢</button>
        <p className="operations-hint">目前 API 未提供伺服器端篩選與分頁；搜尋只套用於本次載入的資料，不會修改任何訂單。</p>
      </div>
    </OperationsListHeader>
    <div className="operations-summary"><div><small>目前筆數</small><strong>{rows.length} 筆</strong></div><div><small>待付款</small><strong>{pending} 筆</strong></div><div><small>已完成</small><strong>{success} 筆</strong></div><div><small>資料範圍</small><strong>本次載入</strong></div></div>
    <div className="table-scroll"><table className="operations-table"><thead><tr><th>系統單號／商戶</th><th>商戶單號</th><th>付款資訊</th><th className="numeric">金額</th><th>狀態</th><th>建立時間</th></tr></thead><tbody>
      {isLoading ? <tr><td colSpan="6" className="operations-empty">正在讀取代收訂單…</td></tr> : error ? <tr><td colSpan="6" className="operations-empty error">暫時無法讀取資料，請重新整理或稍後再試。</td></tr> : rows.length ? rows.map(row => { const [label, tone] = statusMeta(row.status); const accounts = Array.isArray(row.bank_accounts) ? row.bank_accounts.map(maskAccount).join('、') : maskAccount(row.bank_account); return <tr key={row.order_no}><td><strong>{row.order_no || '—'}</strong><span className="operations-subtext">商戶：{row.merchant_code || '—'}｜渠道：{row.channel_code || '—'}</span></td><td>{row.merchant_order_no || '—'}</td><td>{row.bank_id || '—'}<span className="operations-subtext">帳號：{accounts}</span></td><td className="numeric">{formatAmount(row.amount, row.currency || 'TWD')}</td><td><span className={`operations-status ${tone}`}>{label}</span></td><td>{formatDateTime(row.created_at)}</td></tr> }) : <tr><td colSpan="6" className="operations-empty">沒有符合條件的代收訂單。</td></tr>}
    </tbody></table></div>
  </section>
}

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { listPayouts } from '../api/payoutApi'
import OperationsListHeader from '../components/OperationsListHeader'
import { formatAmount, statusMeta, taipeiDateRangeToUTC } from '../utils/operationPresentation'
import './OperationsList.css'

const statuses = [['', '全部狀態'], ['pending', '待處理'], ['processing', '處理中'], ['paid_pending_review', '待覆核'], ['paid', '已付款'], ['failed', '失敗']]

export default function PayoutListPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => {
    const createdFromDate = searchParams.get('created_from_date') || ''
    const createdToDate = searchParams.get('created_to_date') || ''
    return { page: Number(searchParams.get('page') || 1), page_size: Number(searchParams.get('page_size') || 20), query: searchParams.get('query') || '', status: searchParams.get('status') || '', createdFromDate, createdToDate, ...taipeiDateRangeToUTC(createdFromDate, createdToDate) }
  }, [searchParams])
  const { data, isLoading, isFetching, error, refetch } = useQuery({ queryKey: ['payouts', filters], queryFn: () => listPayouts({ page: filters.page, page_size: filters.page_size, query: filters.query, status: filters.status, created_from: filters.created_from, created_to: filters.created_to }), placeholderData: previous => previous, refetchInterval: 60000, refetchIntervalInBackground: false })
  const rows = data?.items || []
  const totalRows = data?.total || 0
  const update = (values) => setSearchParams(previous => { const next = new URLSearchParams(previous); Object.entries(values).forEach(([key, value]) => value ? next.set(key, String(value)) : next.delete(key)); if (!Object.hasOwn(values, 'page')) next.set('page', '1'); return next })
  const queue = rows.filter(row => ['PENDING', 'PROCESSING', 'PAID_PENDING_REVIEW'].includes(String(row.status || '').toUpperCase())).length
  const failed = rows.filter(row => String(row.status || '').toUpperCase() === 'FAILED').length
  const pageCount = Math.max(1, Math.ceil(totalRows / filters.page_size))

  return <section className="panel operations-panel">
    <OperationsListHeader title="代付工作佇列" description="優先處理待辦案件；點入案件後執行認領、憑證與覆核作業。" total={totalRows} isRefreshing={isFetching} onRefresh={refetch}>
      <div className="operations-filterbar">
        <input value={filters.query} onChange={event => update({ query: event.target.value })} placeholder="搜尋代付單號、商戶單號或商戶號" aria-label="搜尋代付訂單" />
        <select value={filters.status} onChange={event => update({ status: event.target.value })} aria-label="代付狀態">{statuses.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
        <label className="operations-date">起日（台北）<input type="date" value={filters.createdFromDate} onChange={event => update({ created_from_date: event.target.value })} aria-label="建立起日（台北時間）" /></label>
        <label className="operations-date">迄日（台北）<input type="date" value={filters.createdToDate} onChange={event => update({ created_to_date: event.target.value })} aria-label="建立迄日（台北時間）" /></label>
        <select value={filters.page_size} onChange={event => update({ page_size: event.target.value })} aria-label="每頁筆數"><option value="20">每頁 20 筆</option><option value="50">每頁 50 筆</option><option value="100">每頁 100 筆</option></select>
        <button type="button" className="operations-search" onClick={() => refetch()}>查詢</button>
        <p className="operations-hint">日期以台北日曆日查詢，系統會轉為 UTC RFC3339 傳給後端；篩選與分頁由後端執行。</p>
      </div>
    </OperationsListHeader>
    <div className="operations-summary"><div><small>本頁待辦</small><strong>{queue} 筆</strong></div><div><small>本頁失敗</small><strong>{failed} 筆</strong></div><div><small>排序方式</small><strong>依後端回傳</strong></div><div><small>查詢結果</small><strong>{totalRows} 筆</strong></div></div>
    <div className="table-scroll"><table className="operations-table"><thead><tr><th>代付單號／商戶</th><th>商戶單號</th><th className="numeric">金額</th><th>人工案件狀態</th><th>操作</th></tr></thead><tbody>
      {isLoading ? <tr><td colSpan="5" className="operations-empty">正在讀取代付工作佇列…</td></tr> : error ? <tr><td colSpan="5" className="operations-empty error">暫時無法讀取資料，請重新整理或稍後再試。</td></tr> : rows.length ? rows.map(row => { const [label, tone] = statusMeta(row.status); return <tr key={row.payout_no}><td><Link className="operations-id" to={`/admin/payouts/${row.payout_no}`}>{row.payout_no}</Link><span className="operations-subtext">商戶：{row.merchant_code || '—'}</span></td><td>{row.merchant_payout_no || '—'}</td><td className="numeric">{formatAmount(row.amount, row.currency || 'TWD')}</td><td><span className={`operations-status ${tone}`}>{label}</span></td><td><Link className="operations-action" to={`/admin/payouts/${row.payout_no}`}>處理案件</Link></td></tr> }) : <tr><td colSpan="5" className="operations-empty">沒有符合條件的代付訂單。</td></tr>}
    </tbody></table></div>
    <div className="operations-pagination"><button type="button" disabled={filters.page <= 1} onClick={() => update({ page: filters.page - 1 })}>上一頁</button><span>第 {filters.page} / {pageCount} 頁</span><button type="button" disabled={filters.page >= pageCount} onClick={() => update({ page: filters.page + 1 })}>下一頁</button></div>
  </section>
}

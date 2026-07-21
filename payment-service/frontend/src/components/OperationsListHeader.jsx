export default function OperationsListHeader({ title, description, total, isRefreshing, onRefresh, children }) {
  return <header className="operations-header">
    <div>
      <p className="operations-eyebrow">營運工作台</p>
      <h2>{title}</h2>
      <p className="operations-description">{description}</p>
    </div>
    <div className="operations-header-actions">
      <span className="operations-count">本頁 {total} 筆</span>
      <button type="button" className="operations-refresh" onClick={onRefresh} disabled={isRefreshing}>{isRefreshing ? '更新中…' : '重新整理'}</button>
    </div>
    {children}
  </header>
}

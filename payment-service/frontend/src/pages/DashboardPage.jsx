import { Link } from 'react-router-dom'

const cards = [
  { title: '代付作業', description: '人工代付案件的查詢、認領、憑證與覆核作業。', groups: [[{ label: '開啟代付工作佇列', to: '/admin/payouts' }]] },
  { title: '代收查詢', description: '查詢目前 API 可提供的代收訂單；帳號資訊維持遮罩。', groups: [[{ label: '開啟代收訂單列表', to: '/admin/collections' }]] },
  { title: '後續模組', description: '帳務、商戶、風控與通知功能需待後端契約及權限流程完成後開通。', groups: [['尚未開通']], muted: true },
]

function Item({ item, accent }) {
  if (typeof item === 'object') return <li><Link to={item.to}>{item.label}</Link></li>
  return <li className={accent ? 'dashboard-accent' : ''}><span>{item}</span><small>尚未開通</small></li>
}

export default function DashboardPage() {
  return <section className="dashboard-grid" aria-label="管理後台功能入口">
    {cards.map(card => <article className={`dashboard-card ${card.muted ? 'dashboard-muted' : ''}`} key={card.title}>
      <h1>{card.title}</h1>
      <div className="dashboard-card-body"><p>{card.description}</p>{card.groups.map((group, index) => <ul key={index}>{group.map((item, itemIndex) => <Item key={itemIndex} item={item} accent={card.accent && index > 0} />)}</ul>)}</div>
    </article>)}
  </section>
}

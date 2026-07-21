import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { useAuth } from '../contexts/AuthContext'

export default function AdminShell({ children, title = '代付列表', crumb = '代付列表', variant }) {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const [online, setOnline] = useState(() => typeof navigator === 'undefined' || navigator.onLine)
  const leave = async () => { await logout(); navigate('/admin/login') }
  useEffect(() => {
    const syncNetwork = () => setOnline(navigator.onLine)
    window.addEventListener('online', syncNetwork); window.addEventListener('offline', syncNetwork)
    return () => { window.removeEventListener('online', syncNetwork); window.removeEventListener('offline', syncNetwork) }
  }, [])

  if (variant === 'dashboard') return <div className="dashboard-app">
    <header className="dashboard-header"><div className="dashboard-nav"><Link className="dashboard-brand" to="/admin">營運後台</Link><span className="dashboard-user">{user?.username || '管理員'}／{user?.role || '已登入'}</span><button className="dashboard-logout" onClick={leave}>登出</button></div></header>
    <main className="dashboard-content">{children}</main>
  </div>

  return <div className={`admin-app ${menuOpen ? 'menu-open' : ''}`}>
    <aside className="sidebar" aria-label="主要導覽">
      <Link className="brand" to="/admin"><span className="brand-mark">N</span><span>營運後台</span></Link>
      <div className="profile"><strong>{user?.username || '管理員'}</strong><span>{user?.role || '已登入'}</span></div>
      <p className="nav-label">已開通功能</p>
      <nav>
        <div className="nav-section"><div className="nav-row selected"><span>▤</span>支付管理<b>⌄</b></div>
          <NavLink className={({ isActive }) => `nav-row sub ${isActive ? 'active' : ''}`} to="/admin/collections"><span>◯</span>代收列表</NavLink>
          <NavLink className={({ isActive }) => `nav-row sub ${isActive ? 'active' : ''}`} to="/admin/payouts"><span>◯</span>代付列表</NavLink>
        </div>
      </nav>
    </aside>
    <section className="workspace">
      <header className="topbar">
        <div className="top-left"><button className="icon-button" aria-label="開啟或收合選單" aria-expanded={menuOpen} onClick={() => setMenuOpen(value => !value)}>☰</button><span className={`connection ${online ? 'online' : 'offline'}`} role="status">{online ? '網路已連線' : '網路中斷，資料可能不是最新'}</span></div>
        <div className="top-right"><span className="user-role">{user?.role || '已登入'}</span><button className="logout" onClick={leave}>登出</button></div>
      </header>
      <main className="content">
        <div className="page-heading"><h1><span>💵</span>{title}</h1><div><Link to="/admin/payouts">Home</Link><span> / {crumb}</span></div></div>
        {children}
      </main>
    </section>
  </div>
}

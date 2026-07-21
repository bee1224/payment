import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'

export default function ProtectedRoute({ children }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <p className="page-loading" aria-live="polite">正在確認登入狀態…</p>
  return user ? children : <Navigate to="/admin/login" replace state={{ from: location }} />
}

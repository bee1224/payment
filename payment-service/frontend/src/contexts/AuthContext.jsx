import { createContext, useContext, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import * as auth from '../api/authApi'
import { resetSessionExpired, setCsrfToken } from '../api/httpClient'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [user, setUser] = useState()
  const [loading, setLoading] = useState(true)
  const queryClient = useQueryClient()
  const clear = () => { setCsrfToken(''); setUser(null); queryClient.clear() }
  const apply = (data) => { resetSessionExpired(); setCsrfToken(data.csrf_token); setUser(data) }

  useEffect(() => {
    const expired = () => clear()
    window.addEventListener('admin:session-expired', expired)
    auth.me().then(apply).catch(clear).finally(() => setLoading(false))
    return () => window.removeEventListener('admin:session-expired', expired)
  }, [])

  return <AuthContext.Provider value={{ user, loading, login: async payload => apply(await auth.login(payload)), logout: async () => { try { await auth.logout() } finally { clear() } } }}>{children}</AuthContext.Provider>
}

export const useAuth = () => useContext(AuthContext)

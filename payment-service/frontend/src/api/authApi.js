import { http } from './httpClient'
export const login = (payload) => http.post('/api/admin/auth/login', payload).then(r => r.data.data)
export const logout = () => http.post('/api/admin/auth/logout').then(r => r.data.data)
export const me = () => http.get('/api/admin/auth/me').then(r => r.data.data)

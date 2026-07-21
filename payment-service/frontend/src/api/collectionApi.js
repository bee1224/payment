import { http } from './httpClient'
export const listCollections = () => http.get('/api/admin/collections').then(r => r.data.data)

import axios from 'axios'
export const http = axios.create({ baseURL: import.meta.env.VITE_API_BASE_URL, withCredentials: true })
let csrfToken = ''
export const setCsrfToken = (value) => { csrfToken = value || '' }
http.interceptors.request.use((config) => { if (csrfToken && !['get','head'].includes(config.method)) config.headers['X-CSRF-Token'] = csrfToken; return config })
let sessionExpired = false
http.interceptors.response.use(response => response, error => {
  if (error.response?.status === 401 && !sessionExpired) { sessionExpired = true; window.dispatchEvent(new Event('admin:session-expired')) }
  return Promise.reject(error)
})
export const resetSessionExpired = () => { sessionExpired = false }

export const userFacingError = (error, fallback = '操作失敗，請稍後再試。') => {
  const status = error?.response?.status
  const requestID = error?.response?.data?.request_id || error?.response?.headers?.['x-request-id']
  const message = ({ 403: '你沒有執行此操作的權限。', 409: '案件已被其他人處理或狀態已變更，請重新讀取。', 429: '請求過於頻繁，請稍後再試。' }[status] || fallback)
  return requestID ? `${message} Request ID：${requestID}` : message
}

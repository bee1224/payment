import { http } from './httpClient'
export const listPayouts = (params = {}) => http.get('/api/admin/payouts', { params }).then(r => r.data.data)
export const getPayout = (payoutNo) => http.get(`/api/admin/payouts/${encodeURIComponent(payoutNo)}`).then(r => r.data.data)
export const startProcessing = (payoutNo) => http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/start-processing`).then(r => r.data.data)
export const uploadReceipt = (payoutNo,file) => { const body = new FormData(); body.append('receipt', file); return http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/receipt`, body).then(r => r.data.data) }
export const confirmSuccess = (payoutNo) => http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/confirm-success`).then(r => r.data.data)
export const markFailed = (payoutNo,reason) => http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/mark-failed`, {reason}).then(r => r.data.data)
export const cancelPayout = (payoutNo,reason) => http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/cancel`, {reason}).then(r => r.data.data)
export const receiptDownloadUrl = (payoutNo) => `/api/admin/payouts/${encodeURIComponent(payoutNo)}/receipt`
export const listCallbackAttempts = (payoutNo) => http.get(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/callback-attempts`).then(r => r.data.data)
export const retryCallback = (payoutNo) => http.post(`/api/admin/payouts/${encodeURIComponent(payoutNo)}/callback/retry`).then(r => r.data.data)

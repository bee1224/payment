export const formatAmount = (value, currency = 'TWD') => `${new Intl.NumberFormat('zh-TW', { maximumFractionDigits: 0 }).format(Number(value || 0))} ${currency}`

export const APP_TIMEZONE = import.meta.env.VITE_APP_TIMEZONE || 'Asia/Taipei'

export const formatDateTime = (value) => value ? new Intl.DateTimeFormat('zh-TW', { dateStyle: 'short', timeStyle: 'short', hour12: false, timeZone: APP_TIMEZONE }).format(new Date(value)) : '—'

export const taipeiDateRangeToUTC = (fromDate, toDate) => {
  const range = {}
  if (fromDate) range.created_from = new Date(`${fromDate}T00:00:00.000+08:00`).toISOString()
  if (toDate) range.created_to = new Date(`${toDate}T23:59:59.999+08:00`).toISOString()
  return range
}

export const maskAccount = (value) => {
  const account = String(value || '')
  if (!account) return '—'
  if (account.length <= 4) return '••••'
  return `•••• ${account.slice(-4)}`
}

export const statusMeta = (value) => {
  const status = String(value || 'PENDING').toUpperCase()
  return ({
    PENDING: ['待處理', 'neutral'],
    PROCESSING: ['處理中', 'warning'],
    PAID_PENDING_REVIEW: ['待覆核', 'warning'],
    PAID: ['已付款', 'success'],
    SUCCESS: ['成功', 'success'],
    FAILED: ['失敗', 'danger'],
    CANCELLED: ['已取消', 'muted'],
    EXPIRED: ['已逾期', 'danger'],
  }[status] || [status, 'neutral'])
}

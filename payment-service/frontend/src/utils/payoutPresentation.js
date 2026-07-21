export const payoutStatusLabel = (value) => ({ PENDING: '待處理', PROCESSING: '處理中', PAID_PENDING_REVIEW: '待覆核', SUCCESS: '成功', PAID: '成功', FAILED: '失敗', CANCELLED: '已取消' }[value] || value || '—')
export const isAllowedReceipt = (file) => Boolean(file) && ['application/pdf', 'image/jpeg', 'image/png', 'image/webp'].includes(file.type) && file.size <= 10 * 1024 * 1024

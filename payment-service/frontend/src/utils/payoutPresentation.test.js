import { describe, expect, it } from 'vitest'
import { isAllowedReceipt, payoutStatusLabel } from './payoutPresentation'

describe('payout presentation', () => {
  it('translates workflow statuses', () => { expect(payoutStatusLabel('PAID_PENDING_REVIEW')).toBe('待覆核'); expect(payoutStatusLabel('')).toBe('—') })
  it('only accepts permitted receipts up to 10MB', () => { expect(isAllowedReceipt({ type: 'application/pdf', size: 10 * 1024 * 1024 })).toBe(true); expect(isAllowedReceipt({ type: 'image/gif', size: 1 })).toBe(false); expect(isAllowedReceipt({ type: 'image/png', size: 10 * 1024 * 1024 + 1 })).toBe(false) })
})

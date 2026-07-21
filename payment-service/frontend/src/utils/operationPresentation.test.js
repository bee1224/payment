import { describe, expect, it } from 'vitest'
import { formatDateTime, taipeiDateRangeToUTC } from './operationPresentation'

describe('Taipei time presentation', () => {
  it('renders UTC values in Asia/Taipei', () => {
    expect(formatDateTime('2026-07-18T16:30:00Z')).toContain('2026/7/19')
  })

  it('converts a Taipei calendar day to UTC query bounds', () => {
    expect(taipeiDateRangeToUTC('2026-07-19', '2026-07-19')).toEqual({
      created_from: '2026-07-18T16:00:00.000Z',
      created_to: '2026-07-19T15:59:59.999Z',
    })
  })
})

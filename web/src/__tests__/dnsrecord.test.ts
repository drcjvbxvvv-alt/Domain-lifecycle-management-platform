import { describe, it, expect } from 'vitest'
import {
  checkSafety,
  validateRecord,
  planTotalChanges,
  type Plan,
  type ManagedRecord,
} from '@/types/dnsrecord'

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeRecord(overrides: Partial<ManagedRecord> = {}): ManagedRecord {
  return {
    id: 'rec-1',
    type: 'A',
    name: 'www.example.com',
    content: '1.2.3.4',
    ttl: 300,
    managed: true,
    ...overrides,
  }
}

function emptyPlan(zone = 'example.com'): Plan {
  return { zone, creates: [], updates: [], deletes: [] }
}

// ── planTotalChanges ──────────────────────────────────────────────────────────

describe('planTotalChanges', () => {
  it('returns 0 for an empty plan', () => {
    expect(planTotalChanges(emptyPlan())).toBe(0)
  })

  it('sums creates + updates + deletes', () => {
    const plan: Plan = {
      zone: 'example.com',
      creates: [{ action: 'create', after: makeRecord() }],
      updates: [{ action: 'update', before: makeRecord(), after: makeRecord() }],
      deletes: [{ action: 'delete', before: makeRecord() }],
    }
    expect(planTotalChanges(plan)).toBe(3)
  })

  it('handles multi-creates with no updates/deletes', () => {
    const plan: Plan = {
      zone: 'example.com',
      creates: [
        { action: 'create', after: makeRecord() },
        { action: 'create', after: makeRecord({ name: 'api.example.com' }) },
        { action: 'create', after: makeRecord({ name: 'mail.example.com', type: 'MX' }) },
      ],
      updates: [],
      deletes: [],
    }
    expect(planTotalChanges(plan)).toBe(3)
  })
})

// ── checkSafety ───────────────────────────────────────────────────────────────

describe('checkSafety', () => {
  it('passes an empty plan', () => {
    const result = checkSafety(emptyPlan(), 100)
    expect(result.passed).toBe(true)
    expect(result.requires_force).toBe(false)
  })

  it('passes when changes are below thresholds', () => {
    const plan: Plan = {
      zone: 'example.com',
      creates: [],
      updates: [{ action: 'update', before: makeRecord(), after: makeRecord() }], // 1/100 = 1%
      deletes: [{ action: 'delete', before: makeRecord() }],                       // 1/100 = 1%
    }
    const result = checkSafety(plan, 100)
    expect(result.passed).toBe(true)
    expect(result.update_pct).toBeCloseTo(0.01)
    expect(result.delete_pct).toBeCloseTo(0.01)
  })

  it('fails when update exceeds threshold', () => {
    // 40 updates out of 100 = 40% > default 30% threshold
    const updates = Array.from({ length: 40 }, () => ({
      action: 'update' as const,
      before: makeRecord(),
      after: makeRecord(),
    }))
    const plan: Plan = { zone: 'example.com', creates: [], updates, deletes: [] }
    const result = checkSafety(plan, 100)
    expect(result.passed).toBe(false)
    expect(result.requires_force).toBe(true)
    expect(result.reason).toContain('40%')
  })

  it('fails when delete exceeds threshold', () => {
    // 35 deletes out of 100 = 35% > default 30% threshold
    const deletes = Array.from({ length: 35 }, () => ({
      action: 'delete' as const,
      before: makeRecord(),
    }))
    const plan: Plan = { zone: 'example.com', creates: [], updates: [], deletes }
    const result = checkSafety(plan, 100)
    expect(result.passed).toBe(false)
    expect(result.requires_force).toBe(true)
    expect(result.reason).toContain('35%')
  })

  it('bypasses percentage check for small zones (< minExisting)', () => {
    // 3 deletes out of 8 = 37.5% > 30%, but zone is small → pass
    const deletes = Array.from({ length: 3 }, () => ({
      action: 'delete' as const,
      before: makeRecord(),
    }))
    const plan: Plan = { zone: 'example.com', creates: [], updates: [], deletes }
    const result = checkSafety(plan, 8) // 8 < 10 (default minExisting)
    expect(result.passed).toBe(true)
  })

  it('respects custom thresholds', () => {
    // 15 updates / 100 = 15% — normally passes at 30% but should fail at 10%
    const updates = Array.from({ length: 15 }, () => ({
      action: 'update' as const,
      before: makeRecord(),
      after: makeRecord(),
    }))
    const plan: Plan = { zone: 'example.com', creates: [], updates, deletes: [] }
    const result = checkSafety(plan, 100, { updateThreshold: 0.10 })
    expect(result.passed).toBe(false)
  })

  it('blocks root NS change with protectRootNS=true', () => {
    const nsRecord = makeRecord({ type: 'NS', name: 'example.com' })
    const plan: Plan = {
      zone: 'example.com',
      creates: [{ action: 'create', after: nsRecord }],
      updates: [],
      deletes: [],
    }
    const result = checkSafety(plan, 5) // small zone, so % check bypassed
    expect(result.passed).toBe(false)
    expect(result.root_ns_changed).toBe(true)
    expect(result.reason).toContain('NS')
  })

  it('allows root NS change when protectRootNS=false', () => {
    const nsRecord = makeRecord({ type: 'NS', name: 'example.com' })
    const plan: Plan = {
      zone: 'example.com',
      creates: [{ action: 'create', after: nsRecord }],
      updates: [],
      deletes: [],
    }
    const result = checkSafety(plan, 5, { protectRootNS: false })
    expect(result.passed).toBe(true)
  })

  it('detects root NS change via @ notation', () => {
    const nsRecord = makeRecord({ type: 'NS', name: '@' })
    const plan: Plan = {
      zone: 'example.com',
      creates: [{ action: 'create', after: nsRecord }],
      updates: [],
      deletes: [],
    }
    const result = checkSafety(plan, 5)
    expect(result.root_ns_changed).toBe(true)
  })

  it('does NOT flag NS change for a subdomain NS', () => {
    const nsRecord = makeRecord({ type: 'NS', name: 'sub.example.com' })
    const plan: Plan = {
      zone: 'example.com',
      creates: [{ action: 'create', after: nsRecord }],
      updates: [],
      deletes: [],
    }
    const result = checkSafety(plan, 5)
    expect(result.root_ns_changed).toBe(false)
    expect(result.passed).toBe(true)
  })

  it('returns correct metadata fields', () => {
    const plan: Plan = { zone: 'example.com', creates: [], updates: [], deletes: [] }
    const result = checkSafety(plan, 50)
    expect(result.existing_count).toBe(50)
    expect(result.update_threshold).toBe(0.3)
    expect(result.delete_threshold).toBe(0.3)
  })
})

// ── validateRecord ────────────────────────────────────────────────────────────

describe('validateRecord', () => {
  it('passes a valid A record', () => {
    const errors = validateRecord({ type: 'A', name: 'www.example.com', content: '1.2.3.4', ttl: 300 })
    expect(errors).toHaveLength(0)
  })

  it('fails when name is missing', () => {
    const errors = validateRecord({ type: 'A', name: '', content: '1.2.3.4', ttl: 300 })
    expect(errors.some(e => e.field === 'name')).toBe(true)
  })

  it('fails when type is missing', () => {
    const errors = validateRecord({ type: '', name: 'www.example.com', content: '1.2.3.4', ttl: 300 })
    expect(errors.some(e => e.field === 'type')).toBe(true)
  })

  it('fails when content is missing', () => {
    const errors = validateRecord({ type: 'A', name: 'www.example.com', content: '', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('fails when TTL is 0', () => {
    const errors = validateRecord({ type: 'A', name: 'www.example.com', content: '1.2.3.4', ttl: 0 })
    expect(errors.some(e => e.field === 'ttl')).toBe(true)
  })

  it('fails for invalid IPv4 in A record', () => {
    const errors = validateRecord({ type: 'A', name: 'x.example.com', content: 'not-an-ip', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('fails for out-of-range octet in A record', () => {
    const errors = validateRecord({ type: 'A', name: 'x.example.com', content: '256.1.2.3', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid AAAA record', () => {
    const errors = validateRecord({ type: 'AAAA', name: 'v6.example.com', content: '2001:db8::1', ttl: 300 })
    expect(errors).toHaveLength(0)
  })

  it('fails for invalid IPv6 in AAAA record', () => {
    const errors = validateRecord({ type: 'AAAA', name: 'v6.example.com', content: 'not::valid::ipv6!!', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid CNAME record', () => {
    const errors = validateRecord({ type: 'CNAME', name: 'www.example.com', content: 'target.example.com', ttl: 300 })
    expect(errors).toHaveLength(0)
  })

  it('fails for invalid CNAME target (spaces)', () => {
    const errors = validateRecord({ type: 'CNAME', name: 'www.example.com', content: 'not valid hostname', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid MX record with priority', () => {
    const errors = validateRecord({ type: 'MX', name: 'example.com', content: 'mail.example.com', ttl: 600, priority: 10 })
    expect(errors).toHaveLength(0)
  })

  it('fails MX record with missing priority', () => {
    const errors = validateRecord({ type: 'MX', name: 'example.com', content: 'mail.example.com', ttl: 600, priority: undefined })
    expect(errors.some(e => e.field === 'priority')).toBe(true)
  })

  it('fails MX record with invalid mail server', () => {
    const errors = validateRecord({ type: 'MX', name: 'example.com', content: 'not valid!', ttl: 600, priority: 10 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid TXT record', () => {
    const errors = validateRecord({ type: 'TXT', name: 'example.com', content: 'v=spf1 include:example.com ~all', ttl: 300 })
    expect(errors).toHaveLength(0)
  })

  it('fails TXT record exceeding 2048 chars', () => {
    const errors = validateRecord({ type: 'TXT', name: 'example.com', content: 'a'.repeat(2049), ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid NS record', () => {
    const errors = validateRecord({ type: 'NS', name: 'example.com', content: 'ns1.example.com', ttl: 3600 })
    expect(errors).toHaveLength(0)
  })

  it('fails NS record with invalid nameserver', () => {
    const errors = validateRecord({ type: 'NS', name: 'example.com', content: 'not valid!', ttl: 3600 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('passes a valid CAA record', () => {
    const errors = validateRecord({ type: 'CAA', name: 'example.com', content: '0 issue "letsencrypt.org"', ttl: 300 })
    expect(errors).toHaveLength(0)
  })

  it('fails CAA record with wrong format', () => {
    const errors = validateRecord({ type: 'CAA', name: 'example.com', content: 'letsencrypt.org', ttl: 300 })
    expect(errors.some(e => e.field === 'content')).toBe(true)
  })

  it('returns multiple errors when multiple fields are invalid', () => {
    const errors = validateRecord({ type: '', name: '', content: '', ttl: 0 })
    expect(errors.length).toBeGreaterThanOrEqual(4)
  })
})

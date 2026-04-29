import { describe, it, expect } from 'vitest'
import {
  roleLabel,
  ROLE_PARTICIPANT,
  ROLE_CREATOR,
  ROLE_DECISION_MAKER,
  ROLE_VALUES,
} from '../utils/roleLabels'

describe('roleLabel', () => {
  it('renders the canonical label for each known role', () => {
    expect(roleLabel(ROLE_PARTICIPANT)).toBe('Participant')
    expect(roleLabel(ROLE_CREATOR)).toBe('Creator')
    expect(roleLabel(ROLE_DECISION_MAKER)).toBe('Decision Maker')
  })

  it('falls back to Participant for null / empty role values', () => {
    expect(roleLabel(null)).toBe('Participant')
    expect(roleLabel(undefined)).toBe('Participant')
    expect(roleLabel('')).toBe('Participant')
  })

  it('humanises unknown role strings without crashing', () => {
    expect(roleLabel('admin')).toBe('Admin')
    expect(roleLabel('outside_observer')).toBe('Outside observer')
  })

  it('exports decision_maker in ROLE_VALUES', () => {
    expect(ROLE_VALUES).toContain('decision_maker')
  })
})

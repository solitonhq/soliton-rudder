import { describe, expect, it } from 'vitest'
import { initials } from './ClusterBadge'

describe('initials', () => {
  it('uses first letters of two words', () => expect(initials('prod-eu west')).toBe('PE'))
  it('falls back to first two chars', () => expect(initials('minikube')).toBe('MI'))
  it('handles ARN-like names', () => expect(initials('arn:aws:eks')).toBe('AA'))
})

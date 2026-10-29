import { describe, expect, it } from 'vitest'
import { detectLevel, LogStore, MAX_LOG_LINES, prettyJSON } from './logstore'

describe('detectLevel', () => {
  it('reads JSON level fields', () => {
    expect(detectLevel('{"level":"warn","msg":"x"}')).toBe('warn')
    expect(detectLevel('{"severity":"ERROR"}')).toBe('error')
  })
  it('reads text markers', () => {
    expect(detectLevel('2026-01-01 ERROR something broke')).toBe('error')
    expect(detectLevel('level=info msg=hi')).toBe('info')
    expect(detectLevel('[DEBUG] hello')).toBe('debug')
    expect(detectLevel('plain line about terrorists')).toBe('none')
  })
})

describe('prettyJSON', () => {
  it('extracts message and fields', () => {
    const p = prettyJSON('{"level":"info","msg":"started","port":8080,"ts":"x"}')!
    expect(p.msg).toBe('started')
    expect(p.fields).toEqual([['port', '8080']])
  })
  it('returns null for non-JSON', () => {
    expect(prettyJSON('hello')).toBeNull()
    expect(prettyJSON('{broken')).toBeNull()
  })
})

describe('LogStore', () => {
  it('caps the buffer', () => {
    const s = new LogStore()
    s.push({
      pod: 'p',
      container: 'c',
      lines: Array.from({ length: MAX_LOG_LINES + 10 }, (_, i) => ({ text: String(i) })),
    })
    expect(s.lines.length).toBe(MAX_LOG_LINES)
    expect(s.lines[0].text).toBe('10')
  })
})

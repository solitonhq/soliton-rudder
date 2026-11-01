import { afterEach, describe, expect, it } from 'vitest'
import { installPlainInputs } from './plainInputs'

let off: (() => void) | null = null
afterEach(() => {
  off?.()
  off = null
  document.body.innerHTML = ''
})

function focus(el: HTMLElement) {
  el.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
}

describe('plain text inputs', () => {
  it('turns autocorrect, auto-capitalisation and spell checking off when a text field gets focus', () => {
    off = installPlainInputs()
    document.body.innerHTML =
      '<input id="a"><input id="s" type="search"><textarea id="t"></textarea>'
    for (const id of ['a', 's', 't']) {
      const el = document.getElementById(id) as HTMLInputElement
      focus(el)
      expect(el.getAttribute('autocorrect')).toBe('off')
      expect(el.getAttribute('autocapitalize')).toBe('off')
      expect(el.spellcheck).toBe(false)
    }
  })

  it('leaves checkboxes, buttons and passwords alone', () => {
    off = installPlainInputs()
    document.body.innerHTML =
      '<input id="c" type="checkbox"><input id="p" type="password"><button id="b">x</button>'
    for (const id of ['c', 'p', 'b']) {
      const el = document.getElementById(id)!
      focus(el)
      expect(el.getAttribute('autocorrect')).toBeNull()
    }
  })

  it('stops after it is removed', () => {
    const remove = installPlainInputs()
    remove()
    document.body.innerHTML = '<input id="a">'
    const el = document.getElementById('a')!
    focus(el)
    expect(el.getAttribute('autocorrect')).toBeNull()
  })
})

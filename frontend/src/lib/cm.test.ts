import { searchPanelOpen } from '@codemirror/search'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { afterEach, describe, expect, it } from 'vitest'
import { yamlExtensions } from './cm'

let view: EditorView | null = null
function mount(doc = 'name: demo\nkind: Pod\nname: other\n') {
  const parent = document.createElement('div')
  document.body.appendChild(parent)
  view = new EditorView({ state: EditorState.create({ doc, extensions: yamlExtensions }), parent })
  return view
}
function press(v: EditorView, init: KeyboardEventInit) {
  const e = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  v.contentDOM.dispatchEvent(e)
  return e
}
afterEach(() => {
  view?.destroy()
  view = null
  document.body.innerHTML = ''
})

describe('YAML editor find', () => {
  it('opens the find panel with Ctrl+F and swallows the key', () => {
    const v = mount()
    const e = press(v, { key: 'f', code: 'KeyF', ctrlKey: true })
    expect(searchPanelOpen(v.state)).toBe(true)
    expect(e.defaultPrevented).toBe(true)
  })

  it('keeps the OS from autocorrecting the find box', () => {
    const v = mount()
    press(v, { key: 'f', code: 'KeyF', ctrlKey: true })
    // the panel is created during the keypress; let the update listener see it
    v.dispatch({})
    const input = v.dom.querySelector<HTMLInputElement>('.cm-search input[name=search]')!
    expect(input.getAttribute('autocorrect')).toBe('off')
    expect(input.getAttribute('autocapitalize')).toBe('off')
    expect(input.spellcheck).toBe(false)
  })

  it('renders the panel on top, inside the editor, and Escape closes it', () => {
    const v = mount()
    press(v, { key: 'f', code: 'KeyF', ctrlKey: true })
    const panel = v.dom.querySelector('.cm-panels-top .cm-search')
    expect(panel).not.toBeNull()
    expect(panel!.querySelector('input[name=search]')).not.toBeNull()
    press(v, { key: 'Escape', code: 'Escape' })
    expect(searchPanelOpen(v.state)).toBe(false)
  })
})

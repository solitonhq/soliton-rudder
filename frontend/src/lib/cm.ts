import { yaml } from '@codemirror/lang-yaml'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { Prec } from '@codemirror/state'
import {
  closeSearchPanel,
  findNext,
  findPrevious,
  openSearchPanel,
  search,
  selectNextOccurrence,
} from '@codemirror/search'
import { EditorView, keymap } from '@codemirror/view'
import { tags as t } from '@lezer/highlight'

export const cmTheme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'var(--bg)', color: 'var(--fg)', fontSize: '12px' },
  '.cm-content': { fontFamily: 'var(--font-mono)', caretColor: 'var(--accent)' },
  '.cm-gutters': { backgroundColor: 'var(--bg)', color: 'var(--fg-muted)', border: 'none' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--surface-hover)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 30%, transparent) !important',
  },
  '.cm-cursor': { borderLeftColor: 'var(--accent)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': { overflow: 'auto', userSelect: 'text' },
  // merge/diff view
  '.cm-changedLine, .cm-insertedLine': {
    backgroundColor: 'color-mix(in srgb, var(--success) 14%, transparent) !important',
  },
  '.cm-deletedChunk, .cm-deletedLine': {
    backgroundColor: 'color-mix(in srgb, var(--danger) 14%, transparent) !important',
  },
  '.cm-changedText, .cm-insertedText': {
    backgroundColor: 'color-mix(in srgb, var(--success) 35%, transparent) !important',
  },
  '.cm-deletedText': {
    backgroundColor: 'color-mix(in srgb, var(--danger) 35%, transparent) !important',
  },
  '.cm-collapsedLines': { color: 'var(--fg-muted)', backgroundColor: 'var(--surface-2)' },
  // find panel (⌘F / Ctrl+F): follows the app theme instead of CodeMirror's light default
  '.cm-panels': {
    backgroundColor: 'var(--surface)',
    color: 'var(--fg)',
    borderColor: 'var(--border)',
    fontFamily: 'var(--font-sans)',
  },
  '.cm-panels-top': { borderBottom: '1px solid var(--border)' },
  '.cm-panels-bottom': { borderTop: '1px solid var(--border)' },
  '.cm-search': { padding: '6px 28px 6px 8px', fontSize: '12px' },
  '.cm-search label': { color: 'var(--fg-muted)', marginRight: '6px' },
  '.cm-search .cm-textfield': {
    backgroundColor: 'var(--bg)',
    color: 'var(--fg)',
    border: '1px solid var(--border)',
    borderRadius: '6px',
    padding: '2px 6px',
    margin: '0 4px 0 0',
  },
  '.cm-search .cm-textfield:focus': {
    outline: '1px solid var(--accent)',
    borderColor: 'var(--accent)',
  },
  '.cm-search .cm-button': {
    backgroundImage: 'none',
    backgroundColor: 'var(--surface-2)',
    color: 'var(--fg)',
    border: '1px solid var(--border)',
    borderRadius: '6px',
    padding: '2px 8px',
    margin: '0 4px 0 0',
    cursor: 'pointer',
  },
  '.cm-search .cm-button:hover': { backgroundColor: 'var(--surface-hover)' },
  '.cm-search [name=close]': { color: 'var(--fg-muted)', top: '4px', right: '6px' },
  '.cm-searchMatch': {
    backgroundColor: 'color-mix(in srgb, var(--warning) 28%, transparent)',
    outline: '1px solid color-mix(in srgb, var(--warning) 55%, transparent)',
  },
  '.cm-searchMatch.cm-searchMatch-selected': {
    backgroundColor: 'color-mix(in srgb, var(--accent) 40%, transparent)',
    outline: '1px solid var(--accent)',
  },
  '.cm-selectionMatch': { backgroundColor: 'color-mix(in srgb, var(--accent) 18%, transparent)' },
})

export const cmHighlight = HighlightStyle.define([
  { tag: [t.propertyName, t.definition(t.propertyName)], color: 'var(--accent)' },
  { tag: [t.string, t.content], color: 'var(--success)' },
  { tag: [t.number, t.bool, t.null], color: 'var(--warning)' },
  { tag: t.comment, color: 'var(--fg-muted)', fontStyle: 'italic' },
  { tag: [t.meta, t.punctuation], color: 'var(--fg-muted)' },
])

/**
 * Find in the editor. ⌘F (Mod-F) and Ctrl+F both open it: on macOS CodeMirror's default keymap binds Ctrl+F to
 * "cursor right", so without this Ctrl+F never reached search. The panel sits on top, where the eye is.
 */
/** A find box is not prose: stop the OS from autocorrecting, capitalising or spell-checking what is typed into it. */
const plainSearchInputs = EditorView.updateListener.of((u) => {
  for (const el of u.view.dom.querySelectorAll<HTMLInputElement>(
    '.cm-search input:not([type=checkbox])',
  )) {
    if (el.getAttribute('autocorrect') === 'off') continue
    el.setAttribute('autocorrect', 'off')
    el.setAttribute('autocapitalize', 'off')
    el.setAttribute('autocomplete', 'off')
    el.spellcheck = false
  }
})

export const searchExtensions = [
  search({ top: true }),
  plainSearchInputs,
  Prec.highest(
    keymap.of([
      { key: 'Mod-f', run: openSearchPanel, scope: 'editor search-panel', preventDefault: true },
      { key: 'Ctrl-f', run: openSearchPanel, scope: 'editor search-panel', preventDefault: true },
      { key: 'Mod-g', run: findNext, scope: 'editor search-panel', preventDefault: true },
      { key: 'Shift-Mod-g', run: findPrevious, scope: 'editor search-panel', preventDefault: true },
      { key: 'F3', run: findNext, scope: 'editor search-panel', preventDefault: true },
      { key: 'Shift-F3', run: findPrevious, scope: 'editor search-panel', preventDefault: true },
      { key: 'Escape', run: closeSearchPanel, scope: 'editor search-panel' },
      { key: 'Mod-d', run: selectNextOccurrence, preventDefault: true },
    ]),
  ),
]

export const yamlExtensions = [
  yaml(),
  cmTheme,
  syntaxHighlighting(cmHighlight),
  ...searchExtensions,
]

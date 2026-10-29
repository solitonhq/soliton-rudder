import { yaml } from '@codemirror/lang-yaml'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { EditorView } from '@codemirror/view'
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
})

export const cmHighlight = HighlightStyle.define([
  { tag: [t.propertyName, t.definition(t.propertyName)], color: 'var(--accent)' },
  { tag: [t.string, t.content], color: 'var(--success)' },
  { tag: [t.number, t.bool, t.null], color: 'var(--warning)' },
  { tag: t.comment, color: 'var(--fg-muted)', fontStyle: 'italic' },
  { tag: [t.meta, t.punctuation], color: 'var(--fg-muted)' },
])

export const yamlExtensions = [yaml(), cmTheme, syntaxHighlighting(cmHighlight)]

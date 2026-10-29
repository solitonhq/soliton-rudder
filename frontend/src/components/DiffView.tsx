import { MergeView } from '@codemirror/merge'
import { EditorView } from '@codemirror/view'
import { useEffect, useRef } from 'react'
import { yamlExtensions } from '../lib/cm'

/** Side-by-side YAML diff (before → after) with unchanged regions collapsed. */
export function DiffView({ before, after }: { before: string; after: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!ref.current) return
    const ro = [...yamlExtensions, EditorView.editable.of(false), EditorView.lineWrapping]
    const mv = new MergeView({
      a: { doc: before, extensions: ro },
      b: { doc: after, extensions: ro },
      parent: ref.current,
      collapseUnchanged: { margin: 3, minSize: 6 },
      highlightChanges: true,
      gutter: true,
    })
    return () => mv.destroy()
  }, [before, after])
  return <div ref={ref} className="h-full overflow-auto" />
}

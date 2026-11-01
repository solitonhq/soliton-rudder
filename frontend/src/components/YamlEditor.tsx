import CodeMirror from '@uiw/react-codemirror'
import { yamlExtensions } from '../lib/cm'

export function YamlEditor({
  value,
  onChange,
  readOnly,
}: {
  value: string
  onChange?: (v: string) => void
  readOnly?: boolean
}) {
  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      readOnly={readOnly}
      height="100%"
      theme="none"
      extensions={yamlExtensions}
      basicSetup={{
        foldGutter: true,
        highlightActiveLine: !readOnly,
        autocompletion: false,
        searchKeymap: false, // replaced by searchExtensions in lib/cm.ts
      }}
      className="h-full"
    />
  )
}

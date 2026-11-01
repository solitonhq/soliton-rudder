import { Button, Dialog } from '../components/ui'

/** Confirms a rolling restart; shown for the one-click row action, where a stray click is easy. */
export function RestartDialog({
  name,
  kind,
  onClose,
  onRestart,
}: {
  name: string
  kind: string
  onClose: () => void
  onRestart: () => Promise<void>
}) {
  return (
    <Dialog title={`Restart ${name}?`} onClose={onClose} width={400}>
      <p className="text-sm text-muted">
        This triggers a rolling restart of the {kind}: its pods are replaced one by one. Nothing
        else changes.
      </p>
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          autoFocus
          onClick={() => {
            onClose()
            void onRestart()
          }}
        >
          Restart
        </Button>
      </div>
    </Dialog>
  )
}

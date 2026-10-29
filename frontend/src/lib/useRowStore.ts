import { useSyncExternalStore } from 'react'
import type { RowStore } from './rowstore'

/** Re-renders when the store changes (throttled to one notification per frame). */
export function useRowStoreVersion(store: RowStore): number {
  return useSyncExternalStore(store.subscribe, store.getVersion)
}

import { useEffect, useState } from 'react'
import { doctor } from './backend'

let cache: Set<string> | null = null

/** Rule ids that have automatic fixes (loaded once). */
export function useFixableRules(): Set<string> {
  const [set, setSet] = useState<Set<string>>(cache ?? new Set())
  useEffect(() => {
    if (cache) return
    let cancelled = false
    void doctor.fixable().then((ids) => {
      cache = new Set(ids)
      if (!cancelled) setSet(cache)
    })
    return () => {
      cancelled = true
    }
  }, [])
  return set
}

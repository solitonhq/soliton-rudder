import { useEffect, useRef, useState } from 'react'

export interface PollState<T> {
  data: T | null
  error: string
  loading: boolean
}

/** Runs `fetcher` immediately and every `ms`; state is reset when `key` changes. */
export function usePoll<T>(fetcher: () => Promise<T>, key: string, ms: number): PollState<T> {
  const [state, setState] = useState<PollState<T> & { key: string }>({
    data: null,
    error: '',
    loading: true,
    key,
  })
  const fetchRef = useRef(fetcher)
  fetchRef.current = fetcher

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      try {
        const data = await fetchRef.current()
        if (!cancelled) setState({ data, error: '', loading: false, key })
      } catch (e) {
        if (!cancelled)
          setState((s) => ({
            data: s.key === key ? s.data : null,
            error: e instanceof Error ? e.message : String(e),
            loading: false,
            key,
          }))
      }
    }
    void run()
    const t = ms > 0 ? setInterval(() => void run(), ms) : undefined
    return () => {
      cancelled = true
      if (t) clearInterval(t)
    }
  }, [key, ms])

  // A stale key means a fetch for the new key has not completed yet.
  return state.key === key ? state : { data: null, error: '', loading: true }
}

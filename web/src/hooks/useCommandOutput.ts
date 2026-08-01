import { useState, useEffect } from 'react'
import { API } from '../api'

export type OutputState = { status: 'loading' | 'empty' | 'error' | 'ready'; text: string }

export function useCommandOutput(commandId: string | undefined): OutputState {
  const [state, setState] = useState<OutputState>({ status: 'loading', text: '' })

  useEffect(() => {
    if (!commandId) { setState({ status: 'empty', text: '' }); return }
    setState({ status: 'loading', text: '' })
    let cancelled = false
    API.fetchOutput(commandId)
      .then(raw => {
        if (cancelled) return
        setState(raw.trim() ? { status: 'ready', text: raw } : { status: 'empty', text: '' })
      })
      .catch(() => {
        if (cancelled) return
        setState({ status: 'error', text: '' })
      })
    return () => { cancelled = true }
  }, [commandId])

  return state
}

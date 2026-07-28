import { useState, useEffect, useCallback } from 'react'
import { API } from '../api'
import type { Session } from '../api/types'
import { useServerEvents } from './useServerEvents'

export function useSessions(): Session[] {
  const [sessions, setSessions] = useState<Session[]>([])

  const fetchSessions = useCallback(() => {
    API.fetchSessions().then(data => setSessions(data ?? [])).catch(console.error)
  }, [])

  // Initial load.
  useEffect(() => { fetchSessions() }, [fetchSessions])

  // Re-fetch whenever the daemon notifies us that something changed.
  useServerEvents(useCallback(() => { fetchSessions() }, [fetchSessions]))

  return sessions
}

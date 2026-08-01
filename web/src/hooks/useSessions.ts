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

  // Re-fetch on structural changes; on command_synced do a targeted state update.
  useServerEvents(useCallback((event) => {
    if (event.type === 'command_synced') {
      const shortId = event.data.short_id as string
      setSessions(prev => prev.map(s => ({
        ...s,
        commands: s.commands.map(c => c.id === shortId ? { ...c, syncedGW: true } : c),
      })))
    } else {
      fetchSessions()
    }
  }, [fetchSessions]))

  return sessions
}

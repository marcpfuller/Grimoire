import { useState, useEffect } from 'react'
import { API } from '../api'
import type { Session } from '../api/types'

export function useSessions(): Session[] {
  const [sessions, setSessions] = useState<Session[]>([])
  useEffect(() => {
    API.fetchSessions().then(data => setSessions(data ?? [])).catch(console.error)
  }, [])
  return sessions
}

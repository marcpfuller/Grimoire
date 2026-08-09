import { useEffect, useState } from 'react'
import { API } from '../api'
import type { GwHealth } from '../api/types'

const CONNECTING_HEALTH: GwHealth = { status: 'Connecting...' }
const UNREACHABLE_HEALTH: GwHealth = { status: 'Unreachable' }

export function useGhostwriterHealth(): GwHealth {
  const [health, setHealth] = useState<GwHealth>(CONNECTING_HEALTH)

  useEffect(() => {
    const check = () => API.fetchGhostwriterHealth().then(setHealth).catch(() => setHealth(UNREACHABLE_HEALTH))
    check()
    const interval = setInterval(check, 30000)
    return () => clearInterval(interval)
  }, [])

  return health
}

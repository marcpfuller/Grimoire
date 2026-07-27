import { useState, useEffect } from 'react'
import { API } from '../api'
import type { DaemonHealth } from '../api/types'

const DEFAULT_HEALTH: DaemonHealth = {
  status: 'Connecting...',
  errors: 0,
  warnings: 0,
  version: 'unknown',
}

export function useDaemonHealth(): DaemonHealth {
  const [health, setHealth] = useState<DaemonHealth>(DEFAULT_HEALTH)

  useEffect(() => {
    const check = () => API.fetchHealth().then(setHealth).catch(console.error)
    check()
    const interval = setInterval(check, 5000)
    return () => clearInterval(interval)
  }, [])

  return health
}

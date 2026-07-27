import { useState, useEffect } from 'react'
import type { CommandMetadata } from '../api/types'
import { API } from '../api'

const DEFAULT_METADATA: CommandMetadata = {
  operator: '',
  tool: '',
  userContext: '',
  sourceIp: '',
  destinationIp: '',
  description: '',
  comments: '',
  tags: [],
}

export function useCommandMetadata(commandId: string | undefined): {
  initialData: CommandMetadata
} {
  const [initialData, setInitialData] = useState<CommandMetadata>(DEFAULT_METADATA)

  useEffect(() => {
    if (!commandId) {
      setInitialData(DEFAULT_METADATA)
      return
    }
    let cancelled = false
    API.fetchCommandMetadata(commandId)
      .then(data => { if (!cancelled) setInitialData(data) })
      .catch(() => { if (!cancelled) setInitialData(DEFAULT_METADATA) })
    return () => { cancelled = true }
  }, [commandId])

  return { initialData }
}

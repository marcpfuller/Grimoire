import { useState, useEffect } from 'react'
import type { CommandMetadata } from '../api/types'

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

// TODO: replace with API.fetchCommandMetadata(commandId) when
// POST /api/commands/:short_id/metadata is implemented in the daemon
const MOCK_METADATA: Record<string, CommandMetadata> = {
  abc0001: {
    operator: 'jsmith',
    tool: 'Rubeus.exe',
    userContext: 'CORP\\jsmith',
    sourceIp: '10.10.1.5',
    destinationIp: '10.10.1.20',
    description: '',
    comments: '',
    tags: ['att&ck:T1558'],
  },
}

export function useCommandMetadata(commandId: string | undefined): {
  initialData: CommandMetadata
} {
  const [initialData, setInitialData] = useState<CommandMetadata>(DEFAULT_METADATA)

  useEffect(() => {
    setInitialData(commandId ? (MOCK_METADATA[commandId] ?? DEFAULT_METADATA) : DEFAULT_METADATA)
  }, [commandId])

  return { initialData }
}

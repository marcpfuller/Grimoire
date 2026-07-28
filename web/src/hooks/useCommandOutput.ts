import { useState, useEffect } from 'react'
import { API } from '../api'

export function useCommandOutput(commandId: string | undefined): string {
  const [output, setOutput] = useState('')

  useEffect(() => {
    if (!commandId) { setOutput(''); return }
    API.fetchOutput(commandId)
      .then(raw => setOutput(raw))
      .catch(() => setOutput(''))
  }, [commandId])

  return output
}

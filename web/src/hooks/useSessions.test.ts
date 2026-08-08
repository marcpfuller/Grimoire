import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../api/types'
import type { ServerEvent } from './useServerEvents'
import { useSessions } from './useSessions'

const fetchSessions = vi.fn()
vi.mock('../api', () => ({
  API: { fetchSessions: (...args: unknown[]) => fetchSessions(...args) },
}))

let serverEventCallback: ((event: ServerEvent) => void) | undefined
vi.mock('./useServerEvents', () => ({
  useServerEvents: (callback: (event: ServerEvent) => void) => {
    serverEventCallback = callback
  },
}))

const session: Session = {
  id: 'pane-1',
  name: 'pane-1',
  commands: [],
}

describe('useSessions', () => {
  beforeEach(() => {
    fetchSessions.mockReset()
    serverEventCallback = undefined
  })

  it('removes a deleted session after the backend event triggers a refetch', async () => {
    fetchSessions.mockResolvedValueOnce([session]).mockResolvedValueOnce([])
    const { result } = renderHook(() => useSessions())
    await waitFor(() => expect(result.current).toEqual([session]))

    act(() => {
      serverEventCallback?.({ type: 'session_deleted', data: { pane_id: 'pane-1' } })
    })

    await waitFor(() => expect(fetchSessions).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(result.current).toEqual([]))
  })
})

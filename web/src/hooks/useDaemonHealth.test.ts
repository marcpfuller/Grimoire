import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DaemonHealth } from '../api/types'
import { useDaemonHealth } from './useDaemonHealth'

const fetchHealth = vi.fn()
vi.mock('../api', () => ({
  API: { fetchHealth: (...args: unknown[]) => fetchHealth(...args) },
}))

const connected: DaemonHealth = {
  status: 'Connected',
  errors: 0,
  warnings: 0,
  version: 'unknown',
}

describe('useDaemonHealth', () => {
  beforeEach(() => {
    fetchHealth.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts in the connecting state while the first check is pending', () => {
    fetchHealth.mockReturnValue(new Promise(() => undefined))

    const { result } = renderHook(() => useDaemonHealth())

    expect(result.current.status).toBe('Connecting...')
  })

  it('reports unreachable after a failed check', async () => {
    fetchHealth.mockRejectedValue(new Error('daemon unavailable'))

    const { result } = renderHook(() => useDaemonHealth())

    await waitFor(() => expect(result.current.status).toBe('Unreachable'))
  })

  it('keeps polling and recovers from unreachable to connected', async () => {
    vi.useFakeTimers()
    fetchHealth.mockRejectedValueOnce(new Error('daemon unavailable')).mockResolvedValueOnce(connected)

    const { result } = renderHook(() => useDaemonHealth())
    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.status).toBe('Unreachable')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })

    expect(fetchHealth).toHaveBeenCalledTimes(2)
    expect(result.current.status).toBe('Connected')
  })
})

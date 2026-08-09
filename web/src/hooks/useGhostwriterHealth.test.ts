import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { GwHealth } from '../api/types'
import { useGhostwriterHealth } from './useGhostwriterHealth'

const fetchGhostwriterHealth = vi.fn()
vi.mock('../api', () => ({
  API: { fetchGhostwriterHealth: (...args: unknown[]) => fetchGhostwriterHealth(...args) },
}))

const connected: GwHealth = { status: 'Connected' }

describe('useGhostwriterHealth', () => {
  beforeEach(() => {
    fetchGhostwriterHealth.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts in the connecting state while the first check is pending', () => {
    fetchGhostwriterHealth.mockReturnValue(new Promise(() => undefined))

    const { result } = renderHook(() => useGhostwriterHealth())

    expect(result.current.status).toBe('Connecting...')
  })

  it('keeps polling and recovers from unreachable to connected', async () => {
    vi.useFakeTimers()
    fetchGhostwriterHealth.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(connected)

    const { result } = renderHook(() => useGhostwriterHealth())
    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.status).toBe('Unreachable')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000)
    })

    expect(fetchGhostwriterHealth).toHaveBeenCalledTimes(2)
    expect(result.current.status).toBe('Connected')
  })
})

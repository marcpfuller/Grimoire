import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import type { ServerEvent } from './useServerEvents'
import { useTagsData } from './useTagsData'

const fetchTags = vi.fn()
vi.mock('../api', () => ({
    API: { fetchTags: (...args: unknown[]) => fetchTags(...args) },
}))

let serverEventCallback: ((e: ServerEvent) => void) | undefined
vi.mock('./useServerEvents', () => ({
    useServerEvents: (cb: (e: ServerEvent) => void) => {
        serverEventCallback = cb
    },
}))

describe('useTagsData', () => {
    beforeEach(() => {
        fetchTags.mockReset()
        serverEventCallback = undefined
    })

    it('fetches tags once on mount', async () => {
        fetchTags.mockResolvedValue([{ id: 't1' }])

        const { result } = renderHook(() => useTagsData())

        await waitFor(() => expect(result.current).toHaveLength(1))
        expect(fetchTags).toHaveBeenCalledTimes(1)
    })

    it.each(['tags_updated', 'command_deleted', 'session_deleted'] as const)(
      'refetches tags when a %s server event is received', async (eventType) => {
        fetchTags.mockResolvedValueOnce([]).mockResolvedValueOnce([{ id: 't1' }])

        const { result } = renderHook(() => useTagsData())
        await waitFor(() => expect(fetchTags).toHaveBeenCalledTimes(1))

        act(() => {
            serverEventCallback?.({ type: eventType, data: {} } as ServerEvent)
        })

        await waitFor(() => expect(fetchTags).toHaveBeenCalledTimes(2))
        await waitFor(() => expect(result.current).toHaveLength(1))
      },
    )

    it('ignores unrelated server events', async () => {
        fetchTags.mockResolvedValue([])

        renderHook(() => useTagsData())
        await waitFor(() => expect(fetchTags).toHaveBeenCalledTimes(1))

        act(() => {
            serverEventCallback?.({ type: 'command_started', data: {} })
        })

        // Give any accidental refetch a chance to fire before asserting it didn't.
        await new Promise((r) => setTimeout(r, 10))
        expect(fetchTags).toHaveBeenCalledTimes(1)
    })
})

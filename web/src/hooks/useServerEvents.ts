import { useEffect, useRef } from 'react'
import { getToken } from '../api/client'

export type ServerEventType =
    | 'command_started'
    | 'command_finished'
    | 'command_synced'
    | 'command_deleted'
    | 'session_registered'
    | 'session_deregistered'
    | 'session_deleted'
    | 'tags_updated'

export interface ServerEvent {
    type: ServerEventType
    data: Record<string, unknown>
}

const EVENT_TYPES: ServerEventType[] = [
    'command_started',
    'command_finished',
    'command_synced',
    'command_deleted',
    'session_registered',
    'session_deregistered',
    'session_deleted',
    'tags_updated',
]

/**
 * Subscribes to the daemon's SSE stream at /api/events.
 *
 * onEvent is called for each incoming event. The EventSource reconnects
 * automatically on network interruptions. The subscription is torn down
 * when the component unmounts.
 *
 * The bearer token is passed as a query parameter (?token=…) because the
 * browser EventSource API does not support custom request headers.
 */
export function useServerEvents(onEvent: (e: ServerEvent) => void): void {
    // Store the latest callback in a ref so we never need to re-subscribe
    // when the callback identity changes between renders.
    const callbackRef = useRef(onEvent)
    useEffect(() => {
        callbackRef.current = onEvent
    })

    useEffect(() => {
        const tok = getToken()
        const url = tok ? `/api/events?token=${encodeURIComponent(tok)}` : '/api/events'
        const es = new EventSource(url)

        const dispatch = (raw: Event, type: ServerEventType) => {
            try {
                const data = JSON.parse((raw as MessageEvent).data ?? '{}') as Record<
                    string,
                    unknown
                >
                callbackRef.current({ type, data })
            } catch {
                // Ignore malformed event payloads.
            }
        }

        for (const type of EVENT_TYPES) {
            es.addEventListener(type, (raw) => dispatch(raw, type))
        }

        return () => {
            es.close()
        }
    }, []) // stable — created once per mount, token captured at that point
}

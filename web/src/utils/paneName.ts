import type { Session } from '../api/types'

// buildPaneNameMap indexes every known session by id -> display name, so
// components that only have a raw pane id (e.g. from a Command or Tag) can
// look up the human-readable name without another API round-trip.
export function buildPaneNameMap(sessions: Session[]): Record<string, string> {
    const map: Record<string, string> = {}
    for (const session of sessions) {
        map[session.id] = session.name
    }
    return map
}

// resolvePaneName returns the display name for paneId, falling back to the
// raw id itself if it isn't present in the map (e.g. map not yet loaded).
export function resolvePaneName(paneNameById: Record<string, string>, paneId: string): string {
    return paneNameById[paneId] ?? paneId
}

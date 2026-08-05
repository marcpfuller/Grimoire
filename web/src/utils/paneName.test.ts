import { describe, it, expect } from 'vitest'
import { buildPaneNameMap, resolvePaneName } from './paneName'
import type { Session } from '../api/types'

function makeSession(id: string, name: string): Session {
    return { id, name, commands: [] }
}

// ---------------------------------------------------------------------------
// buildPaneNameMap
// ---------------------------------------------------------------------------
describe('buildPaneNameMap', () => {
    it('maps each session id to its name', () => {
        const sessions = [makeSession('pane-a1b2', 'quiet-star-a1b2'), makeSession('pane-c3d4', 'still-lake-c3d4')]
        expect(buildPaneNameMap(sessions)).toEqual({
            'pane-a1b2': 'quiet-star-a1b2',
            'pane-c3d4': 'still-lake-c3d4',
        })
    })

    it('returns an empty map for no sessions', () => {
        expect(buildPaneNameMap([])).toEqual({})
    })
})

// ---------------------------------------------------------------------------
// resolvePaneName
// ---------------------------------------------------------------------------
describe('resolvePaneName', () => {
    it('returns the mapped name when the pane id is known', () => {
        const map = { 'pane-a1b2': 'quiet-star-a1b2' }
        expect(resolvePaneName(map, 'pane-a1b2')).toBe('quiet-star-a1b2')
    })

    it('falls back to the raw pane id when unknown', () => {
        const map = { 'pane-a1b2': 'quiet-star-a1b2' }
        expect(resolvePaneName(map, 'pane-unknown')).toBe('pane-unknown')
    })

    it('falls back to the raw pane id when the map is empty', () => {
        expect(resolvePaneName({}, 'pane-a1b2')).toBe('pane-a1b2')
    })
})

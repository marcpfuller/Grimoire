import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TagList } from './TagList'
import type { Tag } from '../../api/types'

function makeTag(overrides: Partial<Tag> = {}): Tag {
    return {
        id: 'tag1',
        commandId: '42', // raw database primary key -- NOT the id space used by Command.id
        shortId: 'abc0001', // matches Command.id, used everywhere else in the app to select a command
        label: 'Reconnaissance',
        commandText: 'nmap -sC -sV 10.0.0.5',
        paneId: 'pane-a1b2',
        createdAt: '2026-01-01T00:00:00Z',
        ...overrides,
    }
}

describe('TagList', () => {
    it('selects the command by its short id, not its raw database command id, when a tag entry is clicked', () => {
        const setSelectedCmdId = vi.fn()
        render(
            <TagList tags={[makeTag()]} paneNameById={{}} selectedCmdId="" setSelectedCmdId={setSelectedCmdId} />
        )

        // Expand the "Reconnaissance" group.
        fireEvent.click(screen.getByText('Reconnaissance'))
        // Click the tag entry itself.
        fireEvent.click(screen.getByText('abc0001'))

        expect(setSelectedCmdId).toHaveBeenCalledWith('abc0001')
        expect(setSelectedCmdId).not.toHaveBeenCalledWith('42')
    })
})

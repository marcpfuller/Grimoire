import { beforeEach, describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Session } from '../../api/types'
import { SessionList } from './SessionList'

const exportCast = vi.fn()
const deleteCommand = vi.fn()
vi.mock('../../api', () => ({
  API: {
    exportCast: (...args: unknown[]) => exportCast(...args),
    deleteCommand: (...args: unknown[]) => deleteCommand(...args),
  },
}))

function makeSession(commands: Session['commands']): Session {
  return {
    id: 'pane-1',
    name: 'pane-1',
    commands,
  }
}

function makeCommand(id: string, command: string): Session['commands'][number] {
  return {
    id,
    command,
    timestamp: 'now',
    duration: '1s',
    exitCode: 0,
    paneId: 'pane-1',
    oplogId: 42,
    syncedGW: false,
  }
}

describe('SessionList', () => {
  beforeEach(() => {
    exportCast.mockReset()
    deleteCommand.mockReset()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  it('prunes export selections when refreshed sessions remove a command', async () => {
    const user = userEvent.setup()
    const command = {
      id: 'abc0001',
      command: 'whoami',
      timestamp: 'now',
      duration: '1s',
      exitCode: 0,
      paneId: 'pane-1',
      oplogId: 42,
      syncedGW: false,
    }
    const props = {
      expandedSessions: { 'pane-1': true },
      toggleSession: vi.fn(),
      selectedCmdId: '',
      setSelectedCmdId: vi.fn(),
      addToast: vi.fn(),
      gwConfigured: false,
    }
    const { rerender } = render(
      <SessionList sessions={[makeSession([command])]} {...props} />,
    )

    await user.click(screen.getByRole('checkbox'))
    expect(screen.getByRole('button', { name: /Export 1 command/ })).toBeEnabled()

    rerender(<SessionList sessions={[makeSession([])]} {...props} />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Export \.cast/ })).toBeDisabled()
    })
    expect(exportCast).not.toHaveBeenCalled()
  })

  it('deletes the commands selected by the export checkboxes', async () => {
    const user = userEvent.setup()
    const addToast = vi.fn()
    deleteCommand.mockResolvedValue(undefined)
    const commands = [makeCommand('abc0001', 'whoami'), makeCommand('abc0002', 'hostname')]

    render(
      <SessionList
        sessions={[makeSession(commands)]}
        expandedSessions={{ 'pane-1': true }}
        toggleSession={vi.fn()}
        selectedCmdId=""
        setSelectedCmdId={vi.fn()}
        addToast={addToast}
        gwConfigured
      />,
    )

    const deleteButton = screen.getByRole('button', { name: 'Delete selected commands' })
    expect(deleteButton).toBeDisabled()

    const checkboxes = screen.getAllByRole('checkbox')
    await user.click(checkboxes[0])
    await user.click(checkboxes[1])
    expect(deleteButton).toBeEnabled()

    await user.click(deleteButton)

    await waitFor(() => {
      expect(deleteCommand).toHaveBeenCalledTimes(2)
    })
    expect(deleteCommand).toHaveBeenCalledWith('abc0001')
    expect(deleteCommand).toHaveBeenCalledWith('abc0002')
    expect(window.confirm).toHaveBeenCalledWith('Delete 2 selected commands from this device? This cannot be undone.')
    expect(addToast).toHaveBeenCalledWith({
      type: 'success',
      title: 'Commands deleted',
      detail: 'Deleted 2 commands from the local database.',
    })
    await waitFor(() => expect(deleteButton).toBeDisabled())
  })

  it('keeps failed deletions selected and reports partial success', async () => {
    const user = userEvent.setup()
    const addToast = vi.fn()
    deleteCommand.mockImplementation((id: string) => (
      id === 'abc0002' ? Promise.reject(new Error('db unavailable')) : Promise.resolve()
    ))

    render(
      <SessionList
        sessions={[makeSession([makeCommand('abc0001', 'whoami'), makeCommand('abc0002', 'hostname')])]}
        expandedSessions={{ 'pane-1': true }}
        toggleSession={vi.fn()}
        selectedCmdId=""
        setSelectedCmdId={vi.fn()}
        addToast={addToast}
        gwConfigured
      />,
    )

    for (const checkbox of screen.getAllByRole('checkbox')) await user.click(checkbox)
    await user.click(screen.getByRole('button', { name: 'Delete selected commands' }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith({
      type: 'error',
      title: 'Some commands were not deleted',
      detail: '1 command remains selected.',
      shortIDs: ['abc0002'],
    }))
    expect(screen.getByRole('button', { name: /Export 1 command/ })).toBeEnabled()
  })

  it('does not delete when confirmation is cancelled', async () => {
    const user = userEvent.setup()
    vi.mocked(window.confirm).mockReturnValue(false)

    render(
      <SessionList
        sessions={[makeSession([makeCommand('abc0001', 'whoami')])]}
        expandedSessions={{ 'pane-1': true }}
        toggleSession={vi.fn()}
        selectedCmdId=""
        setSelectedCmdId={vi.fn()}
        addToast={vi.fn()}
        gwConfigured
      />,
    )

    await user.click(screen.getByRole('checkbox'))
    await user.click(screen.getByRole('button', { name: 'Delete selected commands' }))

    expect(deleteCommand).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /Export 1 command/ })).toBeEnabled()
  })
})

import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Session } from '../../api/types'
import { SessionList } from './SessionList'

const exportCast = vi.fn()
vi.mock('../../api', () => ({
  API: { exportCast: (...args: unknown[]) => exportCast(...args) },
}))

function makeSession(commands: Session['commands']): Session {
  return {
    id: 'pane-1',
    name: 'pane-1',
    commands,
  }
}

describe('SessionList', () => {
  it('prunes export selections when refreshed sessions remove a command', async () => {
    const user = userEvent.setup()
    const command = {
      id: 'abc0001',
      command: 'whoami',
      timestamp: 'now',
      duration: '1s',
      exitCode: 0,
      paneId: 'pane-1',
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
})

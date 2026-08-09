import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CommandHeader } from './CommandHeader'

function makeCommand(syncedGW: boolean) {
  return {
    id: 'abc0001',
    command: 'whoami',
    timestamp: 'now',
    duration: '1s',
    exitCode: 0,
    paneId: 'pane-1',
    oplogId: 42,
    syncedGW,
  }
}

describe('CommandHeader', () => {
  beforeEach(() => {
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('links a synced command to its Ghostwriter oplog', () => {
    render(
      <CommandHeader
        command={makeCommand(true)}
        session={null}
        ghostwriterURL="https://ghostwriter.example.com/users/admin/"
        onCopy={vi.fn()}
      />,
    )

    const link = screen.getByRole('link', { name: 'Open Ghostwriter oplog' })
    expect(link).toHaveAttribute('href', 'https://ghostwriter.example.com/oplog/42/entries')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveClass('text-vscode-success')
    expect(link.querySelector('svg')).toHaveAttribute('width', '34')
    expect(link.querySelector('svg')).toHaveAttribute('height', '34')
    expect(screen.queryByRole('button', { name: 'Copy' })).not.toBeInTheDocument()
  })

  it('shows a disabled gray ghost until the command has synced', () => {
    render(
      <CommandHeader
        command={makeCommand(false)}
        session={null}
        ghostwriterURL="https://ghostwriter.example.com"
        onCopy={vi.fn()}
      />,
    )

    expect(screen.queryByRole('link', { name: 'Open Ghostwriter oplog' })).not.toBeInTheDocument()
    const status = screen.getByTitle('Not yet synced to Ghostwriter')
    expect(status).toHaveAttribute('aria-disabled', 'true')
    expect(status).toHaveClass('text-vscode-text-muted')
    expect(screen.getByTestId('unsynced-ghost').querySelector('svg')).toHaveAttribute('width', '34')
    expect(screen.getByTestId('unsynced-ghost').querySelector('svg')).toHaveAttribute('height', '34')
  })

  it('subtly glows and moves an unsynced ghost away from the mouse', () => {
    render(
      <CommandHeader
        command={makeCommand(false)}
        session={null}
        ghostwriterURL="https://ghostwriter.example.com"
        onCopy={vi.fn()}
      />,
    )

    const status = screen.getByTitle('Not yet synced to Ghostwriter')
    vi.spyOn(status, 'getBoundingClientRect').mockReturnValue({
      x: 100,
      y: 0,
      left: 100,
      right: 140,
      top: 0,
      bottom: 24,
      width: 40,
      height: 24,
      toJSON: () => ({}),
    })
    const ghost = screen.getByTestId('unsynced-ghost')

    fireEvent.mouseEnter(status)
    fireEvent.mouseMove(status, { clientX: 130 })
    expect(ghost).toHaveStyle({ transform: 'translateX(-20px)' })
    expect(ghost.style.filter).toContain('drop-shadow')

    fireEvent.mouseMove(status, { clientX: 110 })
    expect(ghost).toHaveStyle({ transform: 'translateX(20px)' })

    fireEvent.mouseLeave(status)
    expect(ghost).toHaveStyle({ transform: 'translateX(0px)', filter: 'none' })
  })

  it('shakes, flees right, and resets after four distinct dodges', () => {
    render(
      <CommandHeader
        command={makeCommand(false)}
        session={null}
        ghostwriterURL="https://ghostwriter.example.com"
        onCopy={vi.fn()}
      />,
    )

    const status = screen.getByTitle('Not yet synced to Ghostwriter')
    vi.spyOn(status, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      width: 40,
    } as DOMRect)
    const ghost = screen.getByTestId('unsynced-ghost')

    fireEvent.mouseMove(status, { clientX: 130 })
    fireEvent.mouseMove(status, { clientX: 130 })
    expect(ghost).not.toHaveClass('shy-ghost-fury')

    fireEvent.mouseMove(status, { clientX: 110 })
    fireEvent.mouseMove(status, { clientX: 130 })
    expect(ghost).not.toHaveClass('shy-ghost-fury')

    fireEvent.mouseMove(status, { clientX: 110 })
    expect(ghost).toHaveClass('shy-ghost-fury')

    fireEvent.animationEnd(ghost)
    expect(ghost).not.toHaveClass('shy-ghost-fury')
    expect(ghost).toHaveStyle({ transform: 'translateX(0px)' })
  })

  it('keeps the unsynced ghost still when reduced motion is requested', () => {
    vi.mocked(window.matchMedia).mockReturnValue({ matches: true } as MediaQueryList)
    render(
      <CommandHeader
        command={makeCommand(false)}
        session={null}
        ghostwriterURL="https://ghostwriter.example.com"
        onCopy={vi.fn()}
      />,
    )

    const status = screen.getByTitle('Not yet synced to Ghostwriter')
    vi.spyOn(status, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      width: 40,
    } as DOMRect)

    fireEvent.mouseMove(status, { clientX: 130 })
    fireEvent.mouseMove(status, { clientX: 110 })
    fireEvent.mouseMove(status, { clientX: 130 })
    fireEvent.mouseMove(status, { clientX: 110 })

    const ghost = screen.getByTestId('unsynced-ghost')
    expect(ghost).toHaveStyle({ transform: 'translateX(0px)' })
    expect(ghost).not.toHaveClass('shy-ghost-fury')
  })
})

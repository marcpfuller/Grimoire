import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StatusBar } from './StatusBar'

describe('StatusBar', () => {
  it('shows connection statuses without presenting them as clickable', () => {
    render(
      <StatusBar
        health={{ status: 'Connected', errors: 0, warnings: 0, version: 'unknown' }}
        gwHealth={{ status: 'Unreachable' }}
      />,
    )

    const daemonStatus = screen.getByTitle('Grimoire daemon · Connected')
    expect(daemonStatus).not.toHaveClass('cursor-pointer', 'hover:bg-white/20')

    const ghostwriterStatus = screen.getByText('Ghostwriter · Unreachable')
    expect(ghostwriterStatus).not.toHaveClass('cursor-pointer', 'hover:bg-white/20')
    expect(ghostwriterStatus).toHaveAttribute('title', 'Ghostwriter · Unreachable')
  })
})

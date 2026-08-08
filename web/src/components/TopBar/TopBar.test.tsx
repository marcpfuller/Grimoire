import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import indexHtml from '../../../index.html?raw'
import tailwindConfigSource from '../../../tailwind.config.ts?raw'
import spellbookLogoSvg from '../../assets/spellbook_teal.svg?raw'
import { TopBar } from './TopBar'

describe('TopBar', () => {
  it('shows the Grimoire wordmark and spellbook logo', () => {
    render(<TopBar />)

    const heading = screen.getByRole('heading', { name: 'Grimoire' })
    expect(heading).toBeInTheDocument()
    const logo = screen.getByRole('img', { name: 'Grimoire spellbook logo' })
    expect(logo).toBeInTheDocument()
    expect(logo.tagName.toLowerCase()).toBe('svg')
    expect(logo).toHaveAttribute('width', '48')
    expect(logo.nextElementSibling).toBe(heading)
  })

  it('ships the spellbook as self-contained vector artwork', () => {
    expect(spellbookLogoSvg).toMatch(/<path\b/)
    expect(spellbookLogoSvg).not.toMatch(/<(?:image|foreignObject)\b/i)
    expect(spellbookLogoSvg).not.toContain('data:')
  })

  it('uses the app primary blue for the wordmark and logo', () => {
    render(<TopBar />)

    expect(screen.getByRole('heading', { name: 'Grimoire' })).toHaveClass('text-vscode-status')
    expect(screen.getByRole('img', { name: 'Grimoire spellbook logo' }).innerHTML).toContain('#007ACC')
    expect(spellbookLogoSvg).toContain('#007ACC')
  })

  it('uses Cinzel Decorative for the wordmark', () => {
    render(<TopBar />)

    expect(screen.getByRole('heading', { name: 'Grimoire' })).toHaveClass('text-[18px]', 'font-bold')
    expect(tailwindConfigSource).toContain("display: ['Cinzel Decorative', 'Georgia', 'serif']")
    expect(indexHtml).toContain('family=Cinzel+Decorative:wght@700')
  })

  it('uses a visible blue-slate palette for the book body', () => {
    render(<TopBar />)

    const logoMarkup = screen.getByRole('img', { name: 'Grimoire spellbook logo' }).innerHTML
    expect(logoMarkup).toContain('#4A637A')
    expect(logoMarkup).toContain('#263C52')
    expect(spellbookLogoSvg).toContain('#4A637A')
    expect(spellbookLogoSvg).not.toMatch(/#(?:111516|111617|0C1011)/)
  })
})

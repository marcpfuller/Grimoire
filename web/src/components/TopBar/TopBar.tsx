import { SpellbookLogo } from './SpellbookLogo'

export function TopBar() {
  return (
    <header className="h-14 bg-vscode-bg border-b border-vscode-border flex items-center gap-1 px-3 w-full shrink-0">
      <SpellbookLogo
        aria-label="Grimoire spellbook logo"
        width={48}
        height={48}
        className="h-12 w-12 shrink-0"
      />
      <h1 className="font-display text-[18px] font-bold leading-none tracking-[0.06em] text-vscode-status">
        Grimoire
      </h1>
    </header>
  )
}

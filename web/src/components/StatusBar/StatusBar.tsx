import type { DaemonHealth, GwHealth } from '../../api/types'
import { SpellbookIcon } from '../Icons/SpellbookIcon'
import { GhostIcon } from '../Icons/GhostIcon'

export interface StatusBarProps {
  health: DaemonHealth
  gwHealth: GwHealth
}

const DAEMON_COLOR: Record<string, string> = {
  Connected: '#4ec9b0',
  Disconnected: '#f44747',
  Unreachable: '#f44747',
}

const GW_COLOR: Record<string, string> = {
  Connected: '#4ec9b0',
  Disconnected: '#6b6b6b',
  Unreachable: '#f44747',
}

export function StatusBar({ health, gwHealth }: StatusBarProps) {
  const daemonColor = DAEMON_COLOR[health.status] ?? '#cca700'
  const gwColor = GW_COLOR[gwHealth.status] ?? '#6b6b6b'

  return (
    <footer className="h-status-h bg-vscode-status text-white flex items-center px-2 w-full z-50 shrink-0 text-[12px]">
      <div className="flex items-center h-full gap-2">
        <div
          className="inline-flex items-center gap-[4px] px-1.5 h-full"
          style={{ color: daemonColor }}
          title={`Grimoire daemon · ${health.status}`}
        >
          <SpellbookIcon color={daemonColor} />
          {health.status}
        </div>

        <div style={{ width: '1px', height: '10px', backgroundColor: '#3c3c3c' }} />

        <div
          className="inline-flex items-center gap-[4px] px-1.5 h-full"
          style={{ color: gwColor }}
          title={`Ghostwriter · ${gwHealth.status}`}
        >
          <GhostIcon color={gwColor} />
          Ghostwriter · {gwHealth.status}
        </div>
      </div>
    </footer>
  )
}

import type { Command, Session, Toast } from '../../api/types'
import { API } from '../../api'
import { useCommandOutput } from '../../hooks/useCommandOutput'
import { CommandHeader } from './CommandHeader'
import { TerminalOutput } from './TerminalOutput'
import { MetadataEditor } from './MetadataEditor'

export interface CommandDetailProps {
  command: Command | null
  session: Session | null
  addToast: (toast: Omit<Toast, 'id'>) => void
}

export function CommandDetail({ command, session, addToast }: CommandDetailProps) {
  const output = useCommandOutput(command?.id)

  if (!command) {
    return (
      <div className="flex-1 flex items-center justify-center text-vscode-text-muted">
        Select a command to view details
      </div>
    )
  }

  const handleCopy = () => navigator.clipboard.writeText(command.command)

  const handleExport = () => {
    API.exportCast(command.id)
      .then(() => addToast({ type: 'success', title: 'Export successful', detail: `Exported ${command.id}.cast` }))
      .catch(() => addToast({ type: 'error', title: 'Export failed', detail: `Could not export ${command.id}` }))
  }

  return (
    <div className="flex-1 flex flex-col min-w-0 bg-vscode-bg">
      {/* Tab bar */}
      <div className="h-tab-h bg-vscode-tab-bg flex items-end shrink-0 overflow-x-auto">
        <div className="flex items-center h-[35px] px-3 bg-vscode-tab-active border-t border-t-vscode-accent text-white cursor-default min-w-[120px] max-w-[200px] relative group">
          <span className="material-symbols-outlined text-[14px] mr-2 text-vscode-accent">code</span>
          <span className="truncate">{command.id}</span>
          <span className="material-symbols-outlined text-[14px] ml-auto opacity-0 group-hover:opacity-100 hover:bg-vscode-hover rounded cursor-pointer">close</span>
        </div>
        <div className="flex items-center h-[35px] px-3 bg-vscode-tab-bg text-vscode-text-muted hover:bg-vscode-hover cursor-pointer min-w-[120px] max-w-[200px] relative group border-r border-vscode-border">
          <span className="material-symbols-outlined text-[14px] mr-2">terminal</span>
          <span className="truncate">Terminal – {session?.name ?? command.paneId}</span>
          <span className="material-symbols-outlined text-[14px] ml-auto opacity-0 group-hover:opacity-100 hover:bg-vscode-active rounded cursor-pointer">close</span>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 md:p-6 flex flex-col gap-6">
        <div className="flex flex-col gap-4 border-b border-vscode-border pb-4">
          <CommandHeader command={command} onCopy={handleCopy} onExport={handleExport} />
        </div>
        <TerminalOutput output={output} />
        <MetadataEditor commandId={command.id} addToast={addToast} />
      </div>
    </div>
  )
}

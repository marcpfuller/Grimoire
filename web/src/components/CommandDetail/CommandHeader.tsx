import type { Command, Session } from '../../api/types'

export interface CommandHeaderProps {
  command: Command
  session: Session | null
  onCopy: () => void
}

export function CommandHeader({ command, session, onCopy }: CommandHeaderProps) {
  const isSuccess = command.exitCode === 0
  const exitStatusClass = isSuccess
    ? 'bg-vscode-success-bg text-vscode-success border-[#2e5230]'
    : 'bg-[#4d1f1c] text-[#ff8484] border-[#662925]'
  const exitIcon = isSuccess ? 'check_circle' : 'error'

  return (
    <div className="command-header flex flex-col md:flex-row justify-between gap-4">
      <div className="command-header-info flex flex-col gap-2">
        <span className="command-header-session flex items-center gap-1 text-[12px] text-vscode-text-muted">
          <span className="material-symbols-outlined text-[14px]">terminal</span>
          Terminal – {session?.name ?? command.paneId}
        </span>
        <div className="flex items-center gap-2">
          <span className="font-semibold text-vscode-text-muted">ID: {command.id}</span>
          <div className="px-2 py-0.5 bg-[#1e1e1e] border border-vscode-border text-vscode-string font-code text-[12px] flex items-center rounded">
            {command.command}
            <span onClick={onCopy} className="material-symbols-outlined text-[14px] ml-2 text-vscode-text-muted hover:text-white cursor-pointer" title="Copy">
              content_copy
            </span>
          </div>
        </div>
        <div className="command-header-meta flex flex-wrap items-center gap-3 text-[12px] text-vscode-text-muted">
          <span className="flex items-center gap-1"><span className="material-symbols-outlined text-[14px]">schedule</span> {command.timestamp}</span>
          <span className="flex items-center gap-1"><span className="material-symbols-outlined text-[14px]">timer</span> {command.duration}</span>
          <div className={`flex items-center gap-1 px-1.5 rounded-sm border ${exitStatusClass}`}>
            <span className="material-symbols-outlined text-[14px]">{exitIcon}</span> Exit: {command.exitCode}
          </div>
        </div>
      </div>

      <div className="command-header-actions flex flex-wrap items-center gap-2">
        <button onClick={onCopy} className="bg-vscode-hover hover:bg-vscode-active border border-vscode-border px-3 py-1.5 rounded-sm font-semibold transition-colors flex items-center gap-1">
          <span className="material-symbols-outlined">content_copy</span> Copy
        </button>
      </div>
    </div>
  )
}

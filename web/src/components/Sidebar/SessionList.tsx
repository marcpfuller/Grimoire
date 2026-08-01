import { useState } from 'react'
import type { Session, Toast } from '../../api/types'
import { API } from '../../api'

export interface SessionListProps {
  sessions: Session[]
  expandedSessions: Record<string, boolean>
  toggleSession: (id: string) => void
  selectedCmdId: string
  setSelectedCmdId: (id: string) => void
  addToast: (toast: Omit<Toast, 'id'>) => void
  gwConfigured: boolean
}

export function SessionList({
  sessions,
  expandedSessions,
  toggleSession,
  selectedCmdId,
  setSelectedCmdId,
  addToast,
  gwConfigured,
}: SessionListProps) {
  const [checkedIds, setCheckedIds] = useState<Set<string>>(new Set())
  const [exporting, setExporting] = useState(false)

  const toggleCheck = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setCheckedIds(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const handleExport = () => {
    if (checkedIds.size === 0 || exporting) return
    setExporting(true)
    API.exportCast([...checkedIds])
      .then(res => {
        addToast({ type: 'success', title: 'Export successful', detail: res.path })
        setCheckedIds(new Set())
      })
      .catch((err: unknown) => addToast({
        type: 'error',
        title: 'Export failed',
        detail: err instanceof Error ? err.message : 'Could not export selected commands',
      }))
      .finally(() => setExporting(false))
  }

  return (
    <div className="session-list flex flex-col flex-1 min-h-0">
      <div className="session-list-scroll flex-1 overflow-y-auto">
      {sessions.map(session => (
        <div key={session.id} className="session-item flex flex-col">
          <div
            className="session-item-header flex items-center px-2 py-1 cursor-pointer hover:bg-vscode-hover w-full select-none"
            onClick={() => toggleSession(session.id)}
          >
            <span className={`material-symbols-outlined text-[16px] mr-1 text-vscode-text-muted transition-transform duration-150 ${expandedSessions[session.id] ? 'rotate-90' : ''}`}>
              chevron_right
            </span>
            <span className="material-symbols-outlined text-[16px] mr-2 text-vscode-text-muted">folder</span>
            <span className="truncate">{session.name}</span>
          </div>

          {expandedSessions[session.id] && (
            <div className="session-item-commands flex flex-col">
              {session.commands.map(cmd => {
                const isChecked = checkedIds.has(cmd.id)
                return (
                  <div
                    key={cmd.id}
                    className={`command-item group flex items-center pl-8 pr-2 py-1 cursor-pointer w-full ${selectedCmdId === cmd.id ? 'command-item--active bg-vscode-active text-white' : 'text-vscode-text hover:bg-vscode-hover hover:text-white'}`}
                    onClick={() => setSelectedCmdId(cmd.id)}
                  >
                    {/* Checkbox — visible on hover or when checked; code icon otherwise */}
                    <span
                      className={`shrink-0 mr-2 flex items-center justify-center w-[14px] h-[14px] ${isChecked ? '' : 'opacity-0 group-hover:opacity-100'}`}
                      onClick={e => toggleCheck(cmd.id, e)}
                      title={isChecked ? 'Deselect' : 'Select for export'}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => {/* handled by onClick above */}}
                        className="w-[13px] h-[13px] accent-vscode-accent cursor-pointer"
                      />
                    </span>
                    {/* Code icon — hidden when checkbox is visible */}
                    <span className={`material-symbols-outlined text-[14px] mr-1 text-vscode-accent shrink-0 ${isChecked ? 'hidden' : 'group-hover:hidden'}`}>code</span>
                    {/* Sync status dot — always visible when code icon is visible */}
                    <span
                      className={`shrink-0 w-2 h-2 rounded-full mr-1.5 ${isChecked ? 'hidden' : 'group-hover:hidden'} ${
                        !gwConfigured
                          ? 'bg-vscode-text-muted'
                          : cmd.syncedGW
                            ? 'bg-vscode-success'
                            : 'bg-red-500'
                      }`}
                      title={!gwConfigured ? 'Ghostwriter not configured' : cmd.syncedGW ? 'Synced to Ghostwriter' : 'Not yet synced'}
                    />
                    <span className="truncate font-code text-[12px] opacity-90">
                      {cmd.id}: {cmd.command.split(' ')[0]}
                    </span>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      ))}
      </div>

      {/* Export button — always visible at bottom of session list */}
      <div className="session-list-export shrink-0 px-3 py-2 border-t border-vscode-border">
        <button
          onClick={handleExport}
          disabled={checkedIds.size === 0 || exporting}
          className={`w-full flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-sm text-[12px] font-semibold transition-colors border ${
            checkedIds.size > 0
              ? 'bg-vscode-accent hover:bg-[#1177bb] text-white border-vscode-accent'
              : 'bg-vscode-hover text-vscode-text-muted border-vscode-border cursor-default'
          }`}
        >
          <span className="material-symbols-outlined text-[14px]">
            {exporting ? 'hourglass_empty' : 'download'}
          </span>
          {exporting
            ? 'Exporting…'
            : checkedIds.size > 0
              ? `Export ${checkedIds.size} command${checkedIds.size > 1 ? 's' : ''} as .cast`
              : 'Export .cast'}
        </button>
      </div>
    </div>
  )
}

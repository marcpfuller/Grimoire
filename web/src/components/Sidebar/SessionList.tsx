import type { Session } from '../../api/types'

export interface SessionListProps {
  sessions: Session[]
  expandedSessions: Record<string, boolean>
  toggleSession: (id: string) => void
  selectedCmdId: string
  setSelectedCmdId: (id: string) => void
}

export function SessionList({
  sessions,
  expandedSessions,
  toggleSession,
  selectedCmdId,
  setSelectedCmdId,
}: SessionListProps) {
  return (
    <div className="flex-1 overflow-y-auto">
      {sessions.map(session => (
        <div key={session.id} className="flex flex-col">
          <div
            className="flex items-center px-2 py-1 cursor-pointer hover:bg-vscode-hover w-full select-none"
            onClick={() => toggleSession(session.id)}
          >
            <span className={`material-symbols-outlined text-[16px] mr-1 text-vscode-text-muted transition-transform duration-150 ${expandedSessions[session.id] ? 'rotate-90' : ''}`}>
              chevron_right
            </span>
            <span className="material-symbols-outlined text-[16px] mr-2 text-vscode-text-muted">folder</span>
            <span className="truncate">{session.name}</span>
          </div>

          {expandedSessions[session.id] && (
            <div className="flex flex-col">
              {session.commands.map(cmd => (
                <div
                  key={cmd.id}
                  className={`flex items-center pl-8 pr-2 py-1 cursor-pointer w-full ${selectedCmdId === cmd.id ? 'bg-vscode-active text-white' : 'text-vscode-text hover:bg-vscode-hover hover:text-white'}`}
                  onClick={() => setSelectedCmdId(cmd.id)}
                >
                  <span className="material-symbols-outlined text-[14px] mr-2 text-vscode-accent shrink-0">code</span>
                  <span className="truncate font-code text-[12px] opacity-90">
                    {cmd.id}: {cmd.command.split(' ')[0]}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

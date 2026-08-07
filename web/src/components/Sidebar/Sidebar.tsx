import type { ActiveView, Session, Tag, Command, AppConfig, SelectedSetting, Toast } from '../../api/types'
import { SessionList } from './SessionList'
import { TagList } from './TagList'
import { SearchPanel } from './SearchPanel'
import { SettingsNav } from './SettingsNav'

export interface SidebarProps {
  activeView: ActiveView
  sessions: Session[]
  expandedSessions: Record<string, boolean>
  toggleSession: (id: string) => void
  selectedCmdId: string
  setSelectedCmdId: (id: string) => void
  tags: Tag[]
  allCommands: Command[]
  paneNameById: Record<string, string>
  config: AppConfig | null
  selectedSetting: SelectedSetting | null
  setSelectedSetting: (s: SelectedSetting) => void
  addToast: (toast: Omit<Toast, 'id'>) => void
}

export function Sidebar({
  activeView,
  sessions,
  expandedSessions,
  toggleSession,
  selectedCmdId,
  setSelectedCmdId,
  tags,
  allCommands,
  paneNameById,
  config,
  selectedSetting,
  setSelectedSetting,
  addToast,
}: SidebarProps) {
  return (
    <div className="sidebar w-sidebar-w bg-vscode-sidebar flex flex-col shrink-0 border-r border-vscode-border z-10">
      {activeView !== 'sessions' && (
        <div className="sidebar-section-header px-4 py-2 text-[11px] font-bold uppercase text-vscode-text-muted tracking-wider bg-vscode-sidebar">
          {activeView.toUpperCase()}
        </div>
      )}

      {activeView === 'sessions' && (
        <SessionList
          sessions={sessions}
          expandedSessions={expandedSessions}
          toggleSession={toggleSession}
          selectedCmdId={selectedCmdId}
          setSelectedCmdId={setSelectedCmdId}
          addToast={addToast}
          gwConfigured={Boolean(config?.ghostwriter?.url)}
        />
      )}
      {activeView === 'tags' && (
        <TagList tags={tags} paneNameById={paneNameById} selectedCmdId={selectedCmdId} setSelectedCmdId={setSelectedCmdId} />
      )}
      {activeView === 'search' && (
        <SearchPanel
          allCommands={allCommands}
          paneNameById={paneNameById}
          selectedCmdId={selectedCmdId}
          setSelectedCmdId={setSelectedCmdId}
        />
      )}
      {activeView === 'settings' && (
        <SettingsNav config={config} selectedSetting={selectedSetting} setSelectedSetting={setSelectedSetting} />
      )}
    </div>
  )
}

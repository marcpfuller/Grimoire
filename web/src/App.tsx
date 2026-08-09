import { useState, useMemo } from 'react'
import type { ActiveView, SelectedSetting } from './api/types'
import { ActivityBar } from './components/ActivityBar/ActivityBar'
import { Sidebar } from './components/Sidebar/Sidebar'
import { CommandDetail } from './components/CommandDetail/CommandDetail'
import { SettingsEditor } from './components/Settings/SettingsEditor'
import { StatusBar } from './components/StatusBar/StatusBar'
import { ToastContainer } from './components/Toast/ToastContainer'
import { TopBar } from './components/TopBar/TopBar'
import { useDaemonHealth } from './hooks/useDaemonHealth'
import { useGhostwriterHealth } from './hooks/useGhostwriterHealth'
import { useSessions } from './hooks/useSessions'
import { useTagsData } from './hooks/useTagsData'
import { useConfigData } from './hooks/useConfigData'
import { useToasts } from './hooks/useToasts'
import { buildPaneNameMap } from './utils/paneName'

export default function App() {
  const [activeView, setActiveView] = useState<ActiveView>('sessions')
  const [expandedSessions, setExpandedSessions] = useState<Record<string, boolean>>({ 'pane-a1b2': true })
  const [selectedCmdId, setSelectedCmdId] = useState<string>('abc0001')
  const [selectedSetting, setSelectedSetting] = useState<SelectedSetting | null>(null)

  const health = useDaemonHealth()
  const gwHealth = useGhostwriterHealth()
  const sessions = useSessions()
  const tags = useTagsData()
  const allCommands = useMemo(() => sessions.flatMap(s => s.commands), [sessions])
  const paneNameById = useMemo(() => buildPaneNameMap(sessions), [sessions])
  const { config, updateConfig } = useConfigData()
  const ghostwriterURL = typeof config?.ghostwriter?.url === 'string' ? config.ghostwriter.url : ''
  const { toasts, removeToast, addToast } = useToasts()

  const toggleSession = (id: string) =>
    setExpandedSessions(prev => ({ ...prev, [id]: !prev[id] }))

  const selectedSession = useMemo(
    () => sessions.find(s => s.commands.some(c => c.id === selectedCmdId)) ?? null,
    [selectedCmdId, sessions]
  )

  const selectedCommand = useMemo(() => {
    for (const s of sessions) {
      const cmd = s.commands.find(c => c.id === selectedCmdId)
      if (cmd) return cmd
    }
    return null
  }, [selectedCmdId, sessions])

  return (
    <div className="app flex flex-col h-screen overflow-hidden">
      <TopBar />
      <div className="app-body flex flex-1 overflow-hidden">
        <ActivityBar activeView={activeView} setActiveView={setActiveView} />
        <Sidebar
          activeView={activeView}
          sessions={sessions}
          expandedSessions={expandedSessions}
          toggleSession={toggleSession}
          selectedCmdId={selectedCmdId}
          setSelectedCmdId={setSelectedCmdId}
          tags={tags}
          allCommands={allCommands}
          paneNameById={paneNameById}
          config={config}
          selectedSetting={selectedSetting}
          setSelectedSetting={setSelectedSetting}
          addToast={addToast}
        />
        {activeView === 'settings' ? (
          <SettingsEditor selectedSetting={selectedSetting} updateConfig={updateConfig} />
        ) : (
          <CommandDetail
            command={selectedCommand}
            session={selectedSession}
            ghostwriterURL={ghostwriterURL}
            addToast={addToast}
          />
        )}
      </div>
      <StatusBar health={health} gwHealth={gwHealth} />
      <ToastContainer toasts={toasts} removeToast={removeToast} />
    </div>
  )
}

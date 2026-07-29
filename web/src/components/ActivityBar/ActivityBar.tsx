import type { ActiveView } from '../../api/types'
import { ActivityBarItem } from './ActivityBarItem'

export interface ActivityBarProps {
  activeView: ActiveView
  setActiveView: (view: ActiveView) => void
}

export function ActivityBar({ activeView, setActiveView }: ActivityBarProps) {
  return (
    <div className="activity-bar w-activity-w bg-vscode-activity flex flex-col items-center py-2 shrink-0 border-r border-vscode-border z-20">
      <ActivityBarItem id="sessions" icon="terminal" title="Sessions" activeView={activeView} setActiveView={setActiveView} />
      <ActivityBarItem id="tags"     icon="label"    title="Tags"     activeView={activeView} setActiveView={setActiveView} />
      <ActivityBarItem id="search"   icon="search"   title="Search"   activeView={activeView} setActiveView={setActiveView} />
      <ActivityBarItem id="settings" icon="settings" title="Settings" activeView={activeView} setActiveView={setActiveView} />
    </div>
  )
}

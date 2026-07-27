import type { ActiveView } from '../../api/types'

export interface ActivityBarItemProps {
  id: ActiveView
  icon: string
  title: string
  activeView: ActiveView
  setActiveView: (view: ActiveView) => void
}

export function ActivityBarItem({ id, icon, title, activeView, setActiveView }: ActivityBarItemProps) {
  const isActive = activeView === id
  const stateClass = isActive
    ? 'border-vscode-accent text-white'
    : 'border-transparent text-vscode-text-muted hover:text-white'

  return (
    <div
      onClick={() => setActiveView(id)}
      className={`w-full flex justify-center py-3 cursor-pointer duration-150 border-l-2 ${stateClass} ${id === 'settings' ? 'mt-auto' : ''}`}
      title={title}
    >
      <span className="material-symbols-outlined text-[24px]">{icon}</span>
    </div>
  )
}

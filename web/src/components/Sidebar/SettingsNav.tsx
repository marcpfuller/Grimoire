import { useState, useEffect } from 'react'
import type { AppConfig, SelectedSetting, ConfigValue } from '../../api/types'
import { maskSensitive } from '../../utils/metadata'

export interface SettingsNavProps {
  config: AppConfig | null
  selectedSetting: SelectedSetting | null
  setSelectedSetting: (s: SelectedSetting) => void
}

export function SettingsNav({ config, selectedSetting, setSelectedSetting }: SettingsNavProps) {
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})

  useEffect(() => {
    if (!config) return
    const initial: Record<string, boolean> = {}
    Object.keys(config).forEach(k => { initial[k] = true })
    setExpandedGroups(initial)
  }, [config])

  const toggleGroup = (group: string) =>
    setExpandedGroups(prev => ({ ...prev, [group]: !prev[group] }))

  if (!config) return null

  return (
    <div className="flex-1 overflow-y-auto">
      {Object.entries(config).map(([group, settings]) => (
        <div key={group} className="flex flex-col">
          <div
            className="flex items-center px-2 py-1 cursor-pointer hover:bg-vscode-hover w-full select-none uppercase text-[11px] font-bold text-vscode-text-muted"
            onClick={() => toggleGroup(group)}
          >
            <span className={`material-symbols-outlined text-[16px] mr-1 transition-transform duration-150 ${expandedGroups[group] ? 'rotate-90' : ''}`}>
              chevron_right
            </span>
            {group}
          </div>

          {expandedGroups[group] && (
            <div className="flex flex-col mb-2">
              {Object.entries(settings).map(([key, val]) => {
                const isSelected = selectedSetting?.group === group && selectedSetting?.key === key
                return (
                  <div
                    key={key}
                    className={`flex flex-col pl-6 pr-2 py-1.5 cursor-pointer w-full border-l-[3px] ${isSelected ? 'border-vscode-accent bg-vscode-active text-white' : 'border-transparent text-vscode-text hover:bg-vscode-hover hover:text-white'}`}
                    onClick={() => setSelectedSetting({ group, key, value: val as ConfigValue })}
                  >
                    <div className="font-code text-[12px]">{key}</div>
                    <div className="text-[11px] text-vscode-text-muted truncate mt-0.5 font-code">
                      {maskSensitive(key, val as ConfigValue)}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

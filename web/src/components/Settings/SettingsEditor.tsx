import { useState, useEffect } from 'react'
import type { SelectedSetting, ConfigValue } from '../../api/types'
import { maskSensitive } from '../../utils/metadata'

export interface SettingsEditorProps {
  selectedSetting: SelectedSetting | null
  updateConfig: (group: string, key: string, value: ConfigValue) => Promise<void>
}

export function SettingsEditor({ selectedSetting, updateConfig }: SettingsEditorProps) {
  const [editValue, setEditValue] = useState('')

  const isSensitive = Boolean(
    selectedSetting &&
    (selectedSetting.key.toLowerCase().includes('token') ||
      selectedSetting.key.toLowerCase().includes('secret') ||
      selectedSetting.key.toLowerCase().includes('password'))
  )

  useEffect(() => {
    if (!selectedSetting) return
    setEditValue(isSensitive ? '' : String(selectedSetting.value))
  }, [selectedSetting, isSensitive])

  if (!selectedSetting) {
    return (
      <div className="flex-1 flex items-center justify-center text-vscode-text-muted">
        Select a setting to edit
      </div>
    )
  }

  const handleApply = () => {
    if (isSensitive && editValue === '') return
    let parsed: ConfigValue = editValue
    if (typeof selectedSetting.value === 'boolean') parsed = editValue === 'true'
    else if (typeof selectedSetting.value === 'number') parsed = Number(editValue)
    void updateConfig(selectedSetting.group, selectedSetting.key, parsed)
  }

  const handleCancel = () => setEditValue(isSensitive ? '' : String(selectedSetting.value))

  return (
    <div className="flex-1 flex flex-col min-w-0 bg-vscode-bg">
      <div className="h-tab-h bg-vscode-tab-bg flex items-end shrink-0">
        <div className="flex items-center h-[35px] px-3 bg-vscode-tab-active border-t border-t-vscode-accent text-white min-w-[120px] max-w-[300px]">
          <span className="material-symbols-outlined text-[14px] mr-2 text-vscode-accent">settings</span>
          <span className="truncate">Settings Editor</span>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4 md:p-8 flex flex-col max-w-3xl">
        <div className="flex flex-col gap-6">
          <div>
            <div className="text-[14px] text-vscode-text-muted mb-2 flex items-center gap-2">
              <span className="uppercase">{selectedSetting.group}</span>
              <span className="material-symbols-outlined text-[14px]">chevron_right</span>
              <span className="font-code text-vscode-text">{selectedSetting.key}</span>
            </div>
            <div className="text-[12px] text-vscode-text-muted mb-4 flex items-center gap-2 bg-[#2a2d2e] px-3 py-2 rounded-sm border border-vscode-border">
              <span className="material-symbols-outlined text-[16px] text-[#ffcc00]">warning</span>
              <span>Some changes restart the daemon sync loop. Active shell sessions are not affected.</span>
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-[12px] font-semibold text-vscode-text">Current Value</label>
            <div className="p-3 bg-[#1e1e1e] border border-vscode-border rounded-sm font-code text-[13px] text-vscode-string">
              {maskSensitive(selectedSetting.key, selectedSetting.value)}
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-[12px] font-semibold text-vscode-text">New Value</label>
            {typeof selectedSetting.value === 'boolean' ? (
              <select
                className="w-full bg-[#3c3c3c] text-[#cccccc] border border-vscode-border focus:border-vscode-accent focus:ring-0 focus:outline-none rounded-sm px-3 py-2 text-[13px] font-code"
                value={editValue}
                onChange={e => setEditValue(e.target.value)}
              >
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            ) : (
              <input
                type={isSensitive ? 'password' : 'text'}
                className="w-full bg-[#3c3c3c] text-[#cccccc] border border-vscode-border focus:border-vscode-accent focus:ring-0 focus:outline-none rounded-sm px-3 py-2 text-[13px] font-code"
                value={editValue}
                onChange={e => setEditValue(e.target.value)}
                placeholder={isSensitive ? 'leave blank to keep current' : ''}
              />
            )}
          </div>

          <div className="flex items-center gap-3 mt-4 pt-4 border-t border-vscode-border">
            <button onClick={handleApply} className="bg-vscode-accent hover:bg-[#1177bb] text-white border border-vscode-accent px-4 py-1.5 rounded-sm font-semibold transition-colors">
              Apply
            </button>
            <button onClick={handleCancel} className="bg-transparent hover:bg-vscode-hover text-vscode-text border border-vscode-border px-4 py-1.5 rounded-sm font-semibold transition-colors">
              Cancel
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

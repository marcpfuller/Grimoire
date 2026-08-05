import { useState, useMemo } from 'react'
import type { Command } from '../../api/types'
import { highlightText } from '../../utils/text'
import { resolvePaneName } from '../../utils/paneName'

export interface SearchPanelProps {
  allCommands: Command[]
  paneNameById: Record<string, string>
  selectedCmdId: string
  setSelectedCmdId: (id: string) => void
}

export function SearchPanel({ allCommands, paneNameById, selectedCmdId, setSelectedCmdId }: SearchPanelProps) {
  const [searchQuery, setSearchQuery] = useState('')

  const filteredCommands = useMemo(() => {
    if (!searchQuery) return []
    const q = searchQuery.toLowerCase()
    return allCommands.filter(
      cmd =>
        cmd.command.toLowerCase().includes(q) ||
        cmd.id.toLowerCase().includes(q) ||
        resolvePaneName(paneNameById, cmd.paneId).toLowerCase().includes(q)
    )
  }, [searchQuery, allCommands, paneNameById])

  return (
    <div className="flex flex-col flex-1 min-h-0">
      <div className="px-3 pb-2 bg-vscode-sidebar border-b border-vscode-border mb-2">
        <input
          type="text"
          placeholder="Search commands..."
          className="w-full bg-[#3c3c3c] text-[#cccccc] border border-transparent focus:border-[#007acc] focus:ring-0 focus:outline-none rounded-sm px-2 py-1 text-[12px] font-code"
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
        />
      </div>
      <div className="flex-1 overflow-y-auto">
        {filteredCommands.map(cmd => (
          <div
            key={cmd.id}
            className={`flex items-center px-3 py-2 cursor-pointer w-full border-b border-vscode-border/50 ${selectedCmdId === cmd.id ? 'bg-vscode-active text-white' : 'text-vscode-text hover:bg-vscode-hover hover:text-white'}`}
            onClick={() => setSelectedCmdId(cmd.id)}
          >
            <div className="flex flex-col w-full min-w-0">
              <div className="flex items-center justify-between w-full mb-1">
                <span className="font-code text-[11px] text-vscode-accent">{highlightText(cmd.id, searchQuery)}</span>
                <span className="text-[10px] text-vscode-text-muted">
                  {highlightText(resolvePaneName(paneNameById, cmd.paneId), searchQuery)}
                </span>
              </div>
              <span className="truncate font-code text-[11px] opacity-80">{highlightText(cmd.command, searchQuery)}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

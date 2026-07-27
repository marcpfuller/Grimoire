import { useState, useMemo } from 'react'
import type { Tag } from '../../api/types'

export interface TagListProps {
  tags: Tag[]
  selectedCmdId: string
  setSelectedCmdId: (id: string) => void
}

export function TagList({ tags, selectedCmdId, setSelectedCmdId }: TagListProps) {
  const [expandedTags, setExpandedTags] = useState<Record<string, boolean>>({})

  const toggleTag = (label: string) =>
    setExpandedTags(prev => ({ ...prev, [label]: !prev[label] }))

  const groupedTags = useMemo(() => {
    const grouped: Record<string, Tag[]> = {}
    tags.forEach(tag => {
      if (!grouped[tag.label]) grouped[tag.label] = []
      grouped[tag.label].push(tag)
    })
    return grouped
  }, [tags])

  return (
    <div className="flex-1 overflow-y-auto">
      {Object.entries(groupedTags).map(([label, items]) => (
        <div key={label} className="flex flex-col">
          <div
            className="flex items-center px-2 py-1 cursor-pointer hover:bg-vscode-hover w-full select-none"
            onClick={() => toggleTag(label)}
          >
            <span className={`material-symbols-outlined text-[16px] mr-1 text-vscode-text-muted transition-transform duration-150 ${expandedTags[label] ? 'rotate-90' : ''}`}>
              chevron_right
            </span>
            <span className="material-symbols-outlined text-[16px] mr-2 text-[#DCDCAA]">label</span>
            <span className="truncate">{label}</span>
            <span className="ml-auto text-[10px] bg-[#3c3c3c] px-1.5 rounded-sm">{items.length}</span>
          </div>

          {expandedTags[label] && (
            <div className="flex flex-col">
              {items.map(tag => (
                <div
                  key={tag.id}
                  className={`flex items-center pl-8 pr-2 py-1.5 cursor-pointer w-full ${selectedCmdId === tag.commandId ? 'bg-vscode-active text-white' : 'text-vscode-text hover:bg-vscode-hover hover:text-white'}`}
                  onClick={() => setSelectedCmdId(tag.commandId)}
                >
                  <div className="flex flex-col w-full min-w-0">
                    <div className="flex items-center justify-between w-full">
                      <span className="font-code text-[11px] text-vscode-accent">{tag.shortId}</span>
                      <span className="text-[10px] text-vscode-text-muted">{tag.paneId}</span>
                    </div>
                    <span className="truncate font-code text-[11px] opacity-80 mt-0.5">
                      {tag.commandText.length > 50 ? `${tag.commandText.substring(0, 50)}...` : tag.commandText}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

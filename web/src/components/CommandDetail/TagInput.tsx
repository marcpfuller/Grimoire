import { useState } from 'react'

export interface TagInputProps {
  tags: string[]
  stagedTags: string[]
  removedTags: string[]
  toggleRemoveTag: (tag: string) => void
  removeStagedTag: (tag: string) => void
  setStagedTags: (tags: string[]) => void
  editingField: string | null
  setEditingField: (field: string | null) => void
}

export function TagInput({
  tags,
  stagedTags,
  removedTags,
  toggleRemoveTag,
  removeStagedTag,
  setStagedTags,
  editingField,
  setEditingField,
}: TagInputProps) {
  const [tagInput, setTagInput] = useState('')

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && tagInput.trim()) {
      setStagedTags([...stagedTags, tagInput.trim()])
      setTagInput('')
    }
  }

  return (
    <div className="flex items-start min-h-[24px]">
      <div className="w-[140px] text-right pr-4 text-[#6b6b6b] shrink-0 pt-[3px]">Tags</div>
      <div className="flex-1 flex flex-wrap gap-2 pt-[2px] items-center min-w-0">
        {tags.map(tag => (
          <span
            key={tag}
            onClick={() => toggleRemoveTag(tag)}
            className={`bg-[#2d2d2d] border border-[#3c3c3c] rounded-[3px] text-[11px] font-code px-[6px] py-[2px] text-[#d4d4d4] flex items-center gap-1 cursor-pointer select-none transition-opacity ${removedTags.includes(tag) ? 'opacity-40 line-through' : 'hover:bg-[#3c3c3c]'}`}
          >
            {tag}
            <span className="material-symbols-outlined text-[10px]">
              {removedTags.includes(tag) ? 'undo' : 'close'}
            </span>
          </span>
        ))}
        {stagedTags.map((tag, i) => (
          <span
            key={`staged-${i}`}
            onClick={() => removeStagedTag(tag)}
            className="bg-[#2d2d2d] border border-dashed border-[#007acc] rounded-[3px] text-[11px] font-code px-[6px] py-[2px] text-[#d4d4d4] flex items-center gap-1 cursor-pointer select-none hover:bg-[#3c3c3c]"
          >
            {tag}
            <span className="material-symbols-outlined text-[10px]">close</span>
          </span>
        ))}
        {editingField === 'tags' ? (
          <input
            autoFocus
            type="text"
            placeholder="Add tag (Enter)..."
            className="bg-[#1a1a1a] text-[#d4d4d4] border border-[#007acc] outline-none px-1 py-[1px] text-[11px] font-ui rounded-sm w-[140px] h-[22px]"
            value={tagInput}
            onChange={e => setTagInput(e.target.value)}
            onBlur={() => { if (!tagInput) setEditingField(null) }}
            onKeyDown={handleKeyDown}
          />
        ) : (
          <div
            className="px-1 py-[1px] text-[11px] text-[#3c3c3c] italic cursor-text h-[22px] flex items-center"
            onClick={() => setEditingField('tags')}
          >
            click to add tag
          </div>
        )}
      </div>
    </div>
  )
}

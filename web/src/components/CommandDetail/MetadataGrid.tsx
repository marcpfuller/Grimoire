import type { CommandMetadata } from '../../api/types'
import type { TagInputProps } from './TagInput'
import { TagInput } from './TagInput'
import { displayValue } from '../../utils/metadata'

export interface MetadataGridProps {
  draft: CommandMetadata
  setDraft: (draft: CommandMetadata) => void
  editingField: string | null
  setEditingField: (field: string | null) => void
  tagsProps: TagInputProps
}

type TextField = keyof Omit<CommandMetadata, 'tags'>

export function MetadataGrid({ draft, setDraft, editingField, setEditingField, tagsProps }: MetadataGridProps) {
  const renderField = (key: TextField, label: string, type: 'text' | 'textarea' = 'text') => {
    const isEditing = editingField === key
    const value = draft[key]

    return (
      <div key={key} className={`flex items-start ${type === 'textarea' ? 'min-h-[48px]' : 'min-h-[24px]'}`}>
        <div className="w-[140px] text-right pr-4 text-[#6b6b6b] shrink-0 pt-[3px]">{label}</div>
        <div className="flex-1 flex min-w-0" onClick={() => !isEditing && setEditingField(key)}>
          {isEditing ? (
            type === 'textarea' ? (
              <textarea
                autoFocus
                className="w-full bg-[#1a1a1a] text-[#d4d4d4] border border-[#007acc] outline-none px-1 py-0.5 text-[13px] font-ui rounded-sm resize-none h-[44px]"
                value={value}
                onChange={e => setDraft({ ...draft, [key]: e.target.value })}
                onBlur={() => setEditingField(null)}
              />
            ) : (
              <input
                autoFocus
                type="text"
                className="w-full bg-[#1a1a1a] text-[#d4d4d4] border border-[#007acc] outline-none px-1 py-0.5 text-[13px] font-ui rounded-sm h-[22px]"
                value={value}
                onChange={e => setDraft({ ...draft, [key]: e.target.value })}
                onBlur={() => setEditingField(null)}
                onKeyDown={e => e.key === 'Enter' && setEditingField(null)}
              />
            )
          ) : (
            <div className={`w-full h-full px-1 py-[3px] cursor-text text-[13px] ${!value ? 'text-[#3c3c3c] italic' : 'text-[#d4d4d4]'}`}>
              {displayValue(value)}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-[2px] text-[13px]">
      {renderField('operator',      'Operator')}
      {renderField('tool',          'Tool')}
      {renderField('userContext',   'User Context')}
      {renderField('sourceIp',      'Source IP')}
      {renderField('destinationIp', 'Destination IP')}
      {renderField('description',   'Description', 'textarea')}
      {renderField('comments',      'Comments',    'textarea')}
      <TagInput {...tagsProps} />
    </div>
  )
}

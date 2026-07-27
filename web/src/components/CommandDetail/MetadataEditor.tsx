import { useState, useEffect } from 'react'
import type { Toast } from '../../api/types'
import { API } from '../../api'
import { useCommandMetadata } from '../../hooks/useCommandMetadata'
import { isDirty, applyPatch } from '../../utils/metadata'
import { MetadataGrid } from './MetadataGrid'

export interface MetadataEditorProps {
  commandId: string | undefined
  addToast: (toast: Omit<Toast, 'id'>) => void
}

export function MetadataEditor({ commandId, addToast }: MetadataEditorProps) {
  const { initialData } = useCommandMetadata(commandId)
  const [saved, setSaved] = useState(initialData)
  const [draft, setDraft] = useState(initialData)
  const [editingField, setEditingField] = useState<string | null>(null)
  const [removedTags, setRemovedTags] = useState<string[]>([])
  const [stagedTags, setStagedTags] = useState<string[]>([])

  // Reset all state when the selected command changes
  useEffect(() => {
    setSaved(initialData)
    setDraft(initialData)
    setRemovedTags([])
    setStagedTags([])
    setEditingField(null)
  }, [initialData])

  const dirty = isDirty(saved, draft, removedTags, stagedTags)

  const handleApply = async () => {
    if (!commandId) return
    try {
      await API.patchCommandMetadata(commandId, {
        operator: draft.operator || undefined,
        tool: draft.tool || undefined,
        userContext: draft.userContext || undefined,
        sourceIp: draft.sourceIp || undefined,
        destinationIp: draft.destinationIp || undefined,
        description: draft.description || undefined,
        comments: draft.comments || undefined,
        addTags: stagedTags.length > 0 ? stagedTags : undefined,
        removeTags: removedTags.length > 0 ? removedTags : undefined,
      })
      const newSaved = applyPatch(draft, removedTags, stagedTags)
      setSaved(newSaved)
      setDraft(newSaved)
      setRemovedTags([])
      setStagedTags([])
      setEditingField(null)
    } catch {
      addToast({ type: 'error', title: 'Save failed', detail: 'Could not update metadata' })
    }
  }

  const handleCancel = () => {
    setDraft(saved)
    setRemovedTags([])
    setStagedTags([])
    setEditingField(null)
  }

  const toggleRemoveTag = (tag: string) =>
    setRemovedTags(prev => prev.includes(tag) ? prev.filter(t => t !== tag) : [...prev, tag])

  const removeStagedTag = (tag: string) =>
    setStagedTags(prev => prev.filter(t => t !== tag))

  return (
    <div className="flex flex-col mt-6">
      <div className="text-[10px] uppercase tracking-[1px] text-[#6b6b6b] mb-2 font-bold">
        METADATA
      </div>

      <MetadataGrid
        draft={draft}
        setDraft={setDraft}
        editingField={editingField}
        setEditingField={setEditingField}
        tagsProps={{ tags: draft.tags, stagedTags, removedTags, toggleRemoveTag, removeStagedTag, setStagedTags, editingField, setEditingField }}
      />

      {dirty && (
        <div className="flex justify-end gap-3 mt-4 pt-4 border-t border-vscode-border/50">
          <button
            onClick={handleCancel}
            className="px-4 py-1.5 text-[12px] font-semibold text-vscode-text-muted hover:text-white transition-colors rounded-sm border border-transparent hover:border-vscode-border"
          >
            Cancel
          </button>
          <button
            onClick={handleApply}
            className="px-4 py-1.5 text-[12px] font-semibold bg-[#0e639c] hover:bg-[#1177bb] text-white rounded-sm transition-colors border border-[#0e639c]"
          >
            Apply
          </button>
        </div>
      )}
    </div>
  )
}

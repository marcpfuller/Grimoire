import type { CommandMetadata, ConfigValue } from '../api/types'

export function isDirty(
  saved: CommandMetadata,
  draft: CommandMetadata,
  removedTags: string[],
  stagedTags: string[]
): boolean {
  return (
    JSON.stringify(saved) !== JSON.stringify(draft) ||
    removedTags.length > 0 ||
    stagedTags.length > 0
  )
}

export function applyPatch(
  draft: CommandMetadata,
  removedTags: string[],
  stagedTags: string[]
): CommandMetadata {
  const newTags = draft.tags.filter(t => !removedTags.includes(t)).concat(stagedTags)
  return { ...draft, tags: newTags }
}

export function displayValue(value: string): string {
  if (!value || value === '-') return 'click to edit'
  return value
}

// TODO: replace hard-coded key detection with a config-driven sensitive field list
export function maskSensitive(key: string, value: ConfigValue): string {
  if (typeof value !== 'string') return String(value)
  const lowerKey = key.toLowerCase()
  if (
    lowerKey.includes('token') ||
    lowerKey.includes('secret') ||
    lowerKey.includes('password') ||
    lowerKey.includes('_key') ||
    lowerKey.includes('credential')
  ) {
    return '*****'
  }
  return value
}

import { useState, useEffect, useCallback } from 'react'
import { API } from '../api'
import type { Tag } from '../api/types'
import { useServerEvents } from './useServerEvents'

export function useTagsData(): Tag[] {
  const [tags, setTags] = useState<Tag[]>([])

  const fetchTags = useCallback(() => {
    API.fetchTags().then(data => setTags(data ?? [])).catch(console.error)
  }, [])

  // Initial load.
  useEffect(() => { fetchTags() }, [fetchTags])

  // Refetch whenever a tag is added or removed anywhere (UI or CLI).
  useServerEvents(useCallback((event) => {
    if (event.type === 'tags_updated') {
      fetchTags()
    }
  }, [fetchTags]))

  return tags
}

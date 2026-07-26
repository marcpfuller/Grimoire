import { useState, useEffect } from 'react'
import { API } from '../api'
import type { Tag } from '../api/types'

export function useTagsData(): Tag[] {
  const [tags, setTags] = useState<Tag[]>([])
  useEffect(() => {
    API.fetchTags().then(data => setTags(data ?? [])).catch(console.error)
  }, [])
  return tags
}

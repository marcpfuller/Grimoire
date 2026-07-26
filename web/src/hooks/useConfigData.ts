import { useState, useEffect, useCallback } from 'react'
import { API } from '../api'
import type { AppConfig, ConfigValue } from '../api/types'

export function useConfigData(): {
  config: AppConfig | null
  updateConfig: (group: string, key: string, value: ConfigValue) => Promise<void>
} {
  const [config, setConfig] = useState<AppConfig | null>(null)

  useEffect(() => {
    API.fetchConfig().then(setConfig).catch(console.error)
  }, [])

  const updateConfig = useCallback(
    async (group: string, key: string, value: ConfigValue) => {
      if (!config) return
      const updated: AppConfig = { ...config, [group]: { ...config[group], [key]: value } }
      setConfig(updated)
      await API.updateConfig(updated)
    },
    [config]
  )

  return { config, updateConfig }
}

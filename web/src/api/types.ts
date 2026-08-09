// Re-export generated types so the rest of the app imports from one place.
// To regenerate: make typegen  (runs tygo against internal/api/response.go)
export type {
  CommandResponse as Command,
  SessionResponse as Session,
  TagResponse as Tag,
  HealthResponse as DaemonHealth,
  GhostwriterHealthResponse as GwHealth,
  MetadataResponse as CommandMetadata,
  MetadataUpdateRequest,
} from './generated'

// UI-only types (no Go equivalent needed)

export type ActiveView = 'sessions' | 'tags' | 'search' | 'settings'

export type ConfigValue = string | number | boolean | string[]

export interface AppConfig {
  [group: string]: Record<string, ConfigValue>
}

export interface Toast {
  id: string
  type: 'error' | 'warn' | 'info' | 'success'
  title: string
  detail?: string
  shortIDs?: string[]
}

export interface SelectedSetting {
  group: string
  key: string
  value: ConfigValue
}

export interface ApiClient {
  fetchHealth: () => Promise<import('./generated').HealthResponse>
  fetchGhostwriterHealth: () => Promise<import('./generated').GhostwriterHealthResponse>
  fetchConfig: () => Promise<AppConfig>
  updateConfig: (config: AppConfig) => Promise<AppConfig>
  exportCast: (ids: string[]) => Promise<{ success: boolean; path: string }>
  deleteCommand: (shortId: string) => Promise<void>
  fetchSessions: () => Promise<import('./generated').SessionResponse[]>
  fetchTags: () => Promise<import('./generated').TagResponse[]>
  fetchOutput: (commandId: string) => Promise<string>
  fetchCommandMetadata: (commandId: string) => Promise<import('./generated').MetadataResponse>
  patchCommandMetadata: (commandId: string, req: import('./generated').MetadataUpdateRequest) => Promise<void>
  syncCommand: (shortId: string) => Promise<void>
}

import type { ApiClient, DaemonHealth, AppConfig, Session, Tag } from './types'
import type { MetadataResponse, MetadataUpdateRequest } from './generated'

export const RealAPI: ApiClient = {
  fetchHealth: () => fetch('/api/health').then(res => res.json() as Promise<DaemonHealth>),
  fetchConfig: () => fetch('/api/config').then(res => res.json() as Promise<AppConfig>),
  updateConfig: config =>
    fetch('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    }).then(res => res.json() as Promise<AppConfig>),
  exportCast: id =>
    fetch('/api/cast', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    }).then(res => res.json() as Promise<{ success: boolean; path: string }>),
  fetchSessions: () => fetch('/api/sessions').then(res => res.json() as Promise<Session[]>),
  fetchTags: () => fetch('/api/tags').then(res => res.json() as Promise<Tag[]>),
  fetchOutput: commandId => fetch(`/api/commands/${commandId}/output`).then(res => res.text()),
  fetchCommandMetadata: commandId =>
    fetch(`/api/commands/${commandId}/metadata`).then(res => res.json() as Promise<MetadataResponse>),
  patchCommandMetadata: (commandId, req: MetadataUpdateRequest) =>
    fetch(`/api/commands/${commandId}/metadata`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    }).then(res => {
      if (!res.ok) throw new Error(`metadata patch failed: ${res.status}`)
    }),
}

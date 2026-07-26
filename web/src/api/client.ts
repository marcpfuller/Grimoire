import type { ApiClient, DaemonHealth, AppConfig, Session, Tag } from './types'

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
    }).then(res => res.json() as Promise<{ success: boolean }>),
  fetchSessions: () => fetch('/api/sessions').then(res => res.json() as Promise<Session[]>),
  fetchTags: () => fetch('/api/tags').then(res => res.json() as Promise<Tag[]>),
  fetchOutput: commandId => fetch(`/api/commands/${commandId}/output`).then(res => res.text()),
}

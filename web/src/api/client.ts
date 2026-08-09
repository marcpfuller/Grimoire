import type { ApiClient, DaemonHealth, GwHealth, AppConfig, Session, Tag } from './types'
import type { MetadataResponse, MetadataUpdateRequest } from './generated'

/** Returns the bearer token injected by the daemon into index.html, or an
 *  empty string when running outside a live daemon (e.g. unit tests). */
export function getToken(): string {
    const tok = (window as unknown as Record<string, unknown>).__GRIMOIRE_TOKEN__
    return typeof tok === 'string' ? tok : ''
}

/** Build auth headers. Only the Authorization header is added — Content-Type
 *  is left to the caller so it can be overridden or omitted for GET requests. */
function authHeader(): Record<string, string> {
    const tok = getToken()
    return tok ? { Authorization: `Bearer ${tok}` } : {}
}

export const RealAPI: ApiClient = {
  fetchHealth: async () => {
    const res = await fetch('/api/health', { headers: authHeader() })
    if (!res.ok) throw new Error(`health check failed: ${res.status}`)
    return { status: 'Connected', errors: 0, warnings: 0, version: 'unknown' } satisfies DaemonHealth
  },
  fetchGhostwriterHealth: async () => {
    const res = await fetch('/api/ghostwriter/health', { headers: authHeader() })
    if (!res.ok) return { status: 'Unreachable' } satisfies GwHealth
    return res.json() as Promise<GwHealth>
  },
  fetchConfig: () =>
    fetch('/api/config', { headers: authHeader() }).then(res => res.json() as Promise<AppConfig>),
  updateConfig: config =>
    fetch('/api/config', {
      method: 'POST',
      headers: { ...authHeader(), 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    }).then(res => res.json() as Promise<AppConfig>),
  exportCast: ids =>
    fetch('/api/cast', {
      method: 'POST',
      headers: { ...authHeader(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    }).then(async res => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText })) as { error?: string }
        throw new Error(body.error ?? res.statusText)
      }
      return res.json() as Promise<{ success: boolean; path: string }>
    }),
  deleteCommand: shortId =>
    fetch(`/api/commands/${encodeURIComponent(shortId)}`, {
      method: 'DELETE',
      headers: authHeader(),
    }).then(async res => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText })) as { error?: string }
        throw new Error(body.error ?? res.statusText)
      }
    }),
  fetchSessions: () =>
    fetch('/api/sessions', { headers: authHeader() }).then(res => res.json() as Promise<Session[]>),
  fetchTags: () =>
    fetch('/api/tags', { headers: authHeader() }).then(res => res.json() as Promise<Tag[]>),
  fetchOutput: commandId =>
    fetch(`/api/commands/${commandId}/output`, { headers: authHeader() }).then(res => {
      if (!res.ok) throw new Error(`output fetch failed: ${res.status}`)
      return res.text()
    }),
  fetchCommandMetadata: commandId =>
    fetch(`/api/commands/${commandId}/metadata`, { headers: authHeader() }).then(
      res => res.json() as Promise<MetadataResponse>,
    ),
  patchCommandMetadata: (commandId, req: MetadataUpdateRequest) =>
    fetch(`/api/commands/${commandId}/metadata`, {
      method: 'PATCH',
      headers: { ...authHeader(), 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    }).then(res => {
      if (!res.ok) throw new Error(`metadata patch failed: ${res.status}`)
    }),
  syncCommand: shortId =>
    fetch('/api/sync', {
      method: 'POST',
      headers: { ...authHeader(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ short_id: shortId }),
    }).then(res => {
      if (!res.ok) return res.json().then(e => { throw new Error(e.error ?? `sync failed: ${res.status}`) })
    }),
}

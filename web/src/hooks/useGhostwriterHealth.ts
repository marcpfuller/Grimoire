import type { GwHealth } from '../api/types'

// TODO: implement real health check against GET /api/ghostwriter/health
export function useGhostwriterHealth(): GwHealth {
  return { status: 'Connected' }
}

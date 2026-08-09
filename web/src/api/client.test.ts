import { afterEach, describe, expect, it, vi } from 'vitest'
import { RealAPI } from './client'

describe('RealAPI.fetchHealth', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('treats a bodyless successful response as connected', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 200 })))

    await expect(RealAPI.fetchHealth()).resolves.toEqual({
      status: 'Connected',
      errors: 0,
      warnings: 0,
      version: 'unknown',
    })
  })

  it('rejects an unsuccessful health response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 503 })))

    await expect(RealAPI.fetchHealth()).rejects.toThrow('health check failed: 503')
  })
})

describe('RealAPI.fetchGhostwriterHealth', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the daemon-proxied Ghostwriter status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ status: 'Connected' }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )))

    await expect(RealAPI.fetchGhostwriterHealth()).resolves.toEqual({ status: 'Connected' })
  })

  it('maps an unsuccessful local response to unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 503 })))

    await expect(RealAPI.fetchGhostwriterHealth()).resolves.toEqual({ status: 'Unreachable' })
  })
})

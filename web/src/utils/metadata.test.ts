import { describe, it, expect } from 'vitest'
import { maskSensitive, isDirty, displayValue } from './metadata'
import type { CommandMetadata } from '../api/types'

// ---------------------------------------------------------------------------
// maskSensitive
// ---------------------------------------------------------------------------
describe('maskSensitive', () => {
    // --- sensitive string fields ---

    it('masks a field named "token"', () => {
        expect(maskSensitive('token', 'abc123')).toBe('*****')
    })

    it('masks a field whose name contains "token"', () => {
        expect(maskSensitive('api_token', 'abc123')).toBe('*****')
    })

    it('masks a field named "password"', () => {
        expect(maskSensitive('password', 'hunter2')).toBe('*****')
    })

    it('masks a compound field that contains "password"', () => {
        expect(maskSensitive('db_password', 'hunter2')).toBe('*****')
    })

    it('masks regardless of key casing', () => {
        expect(maskSensitive('PASSWORD', 'hunter2')).toBe('*****')
        expect(maskSensitive('Api_Token', 'tok_xxx')).toBe('*****')
    })

    it('masks a field containing "secret"', () => {
        expect(maskSensitive('client_secret', 'xyz')).toBe('*****')
    })

    it('masks a field ending in _key (api_key)', () => {
        expect(maskSensitive('api_key', 'sk-abc123')).toBe('*****')
    })

    it('masks a field ending in _key (private_key)', () => {
        expect(maskSensitive('private_key', '-----BEGIN RSA...')).toBe('*****')
    })

    it('masks a field containing "credential"', () => {
        expect(maskSensitive('aws_credential', 'AKIA...')).toBe('*****')
    })

    it('always returns a fixed-width mask regardless of value length', () => {
        expect(maskSensitive('token', 'x')).toBe('*****')
        expect(maskSensitive('token', 'a-very-long-secret-value-that-should-stay-hidden')).toBe('*****')
    })

    // --- non-sensitive string fields ---

    it('does not mask a non-sensitive string field', () => {
        expect(maskSensitive('hostname', 'example.com')).toBe('example.com')
    })

    it('does not mask "url"', () => {
        expect(maskSensitive('url', 'https://ghostwriter.local')).toBe('https://ghostwriter.local')
    })

    it('does not mask "username"', () => {
        expect(maskSensitive('username', 'admin')).toBe('admin')
    })

    it('does not mask an empty non-sensitive field', () => {
        expect(maskSensitive('hostname', '')).toBe('')
    })

    // false-positive guards — words that *contain* a sensitive substring but shouldn't mask
    it('does not mask "keyboard_shortcut" (contains "key" but not "_key")', () => {
        expect(maskSensitive('keyboard_shortcut', 'ctrl+s')).toBe('ctrl+s')
    })

    it('does not mask "token_count" when value is a plain non-sensitive number', () => {
        // token_count contains "token" — this WILL mask, which is acceptable:
        // the field name is indistinguishable from a sensitive token field.
        // Document the known false-positive so future refactors know it exists.
        expect(maskSensitive('token_count', '42')).toBe('*****')
    })

    // --- non-string values (booleans, numbers) ---

    it('does not mask a boolean value even when the key looks sensitive', () => {
        expect(maskSensitive('require_password', true)).toBe('true')
    })

    it('does not mask a boolean false value', () => {
        expect(maskSensitive('enable_token_auth', false)).toBe('false')
    })

    it('does not mask a numeric value even when the key looks sensitive', () => {
        expect(maskSensitive('token_expiry_seconds', 3600)).toBe('3600')
    })

    it('does not mask a zero numeric value', () => {
        expect(maskSensitive('token_count', 0)).toBe('0')
    })
})

// ---------------------------------------------------------------------------
// isDirty
// ---------------------------------------------------------------------------
const baseMeta: CommandMetadata = {
    operator: 'alice',
    tool: 'nmap',
    userContext: '',
    sourceIp: '',
    destinationIp: '',
    description: '',
    comments: '',
    tags: [],
}

describe('isDirty', () => {
    it('returns false when nothing has changed', () => {
        expect(isDirty(baseMeta, { ...baseMeta }, [], [])).toBe(false)
    })

    it('returns true when a field differs', () => {
        expect(isDirty(baseMeta, { ...baseMeta, operator: 'bob' }, [], [])).toBe(true)
    })

    it('returns true when removedTags is non-empty', () => {
        expect(isDirty(baseMeta, { ...baseMeta }, ['recon'], [])).toBe(true)
    })

    it('returns true when stagedTags is non-empty', () => {
        expect(isDirty(baseMeta, { ...baseMeta }, [], ['lateral-movement'])).toBe(true)
    })
})

// ---------------------------------------------------------------------------
// displayValue
// ---------------------------------------------------------------------------
describe('displayValue', () => {
    it('returns placeholder for empty string', () => {
        expect(displayValue('')).toBe('click to edit')
    })

    it('returns placeholder for dash sentinel', () => {
        expect(displayValue('-')).toBe('click to edit')
    })

    it('returns the value when set', () => {
        expect(displayValue('nmap')).toBe('nmap')
    })
})

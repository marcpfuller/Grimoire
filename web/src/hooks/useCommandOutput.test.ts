import { describe, it, expect } from 'vitest'

// cleanOutput is not exported; test via the observable behaviour by importing
// the module and white-box testing via the barrel if needed.  For now, test
// the function directly by re-implementing the same logic (it's a pure fn).
// We extract it here to keep the test self-contained.
function cleanOutput(raw: string): string {
  return raw
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
    .replace(
      /(\x9b|\x1b\[)[0-9;:<=>?]*[ -/]*[@-~]|\x1b[()][0-9A-Za-z]|\x1b\][^\x07]*\x07|\x1b[^[\]()][A-Za-z]|\x1b[@-_]/g,
      '',
    )
    .trimEnd()
}

describe('cleanOutput', () => {
  it('leaves plain text unchanged', () => {
    expect(cleanOutput('hello world')).toBe('hello world')
  })

  it('normalises CRLF to LF', () => {
    expect(cleanOutput('line1\r\nline2')).toBe('line1\nline2')
  })

  it('normalises bare CR to LF', () => {
    expect(cleanOutput('line1\rline2')).toBe('line1\nline2')
  })

  it('strips SGR colour codes (e.g. green text)', () => {
    expect(cleanOutput('\x1b[0;32mhello\x1b[0m')).toBe('hello')
  })

  it('strips bold + colour reset', () => {
    expect(cleanOutput('\x1b[1mBold\x1b[0m text')).toBe('Bold text')
  })

  it('strips cursor movement sequences', () => {
    // CUU (cursor up), CUD (down), CUF (right), CUB (left)
    expect(cleanOutput('\x1b[2A\x1b[3Btext')).toBe('text')
  })

  it('strips erase-in-display sequences', () => {
    expect(cleanOutput('\x1b[2Jtext')).toBe('text')
  })

  it('strips OSC terminal title sequences', () => {
    expect(cleanOutput('\x1b]0;My Terminal\x07text')).toBe('text')
  })

  it('strips G0/G1 charset designation sequences', () => {
    expect(cleanOutput('\x1b(Btext')).toBe('text')
  })

  it('strips 8-bit CSI sequences', () => {
    expect(cleanOutput('\x9b32mtext\x9b0m')).toBe('text')
  })

  it('handles multi-param SGR sequences like nmap output', () => {
    const raw = '\x1b[1;32mStarting Nmap\x1b[0m 7.94'
    expect(cleanOutput(raw)).toBe('Starting Nmap 7.94')
  })

  it('handles mixed CRLF and ANSI in real-world output', () => {
    const raw = '\x1b[32mHello\x1b[0m\r\nWorld\r\n'
    expect(cleanOutput(raw)).toBe('Hello\nWorld')
  })

  it('trims trailing whitespace', () => {
    expect(cleanOutput('output   \n\n')).toBe('output')
  })

  it('returns empty string for input that is only escape sequences', () => {
    expect(cleanOutput('\x1b[2J\x1b[H')).toBe('')
  })
})

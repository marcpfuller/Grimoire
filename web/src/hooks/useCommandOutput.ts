import { useState, useEffect } from 'react'
import { API } from '../api'

/**
 * Client-side display shim — strips ANSI/VT escape sequences and normalises
 * carriage returns so raw PTY bytes are readable in a plain <div>.
 *
 * This is intentionally minimal. Once the Go `scrub` package is implemented
 * it will handle stripping (and redaction) server-side, at which point this
 * function can be removed and the hook can call setOutput(raw) directly.
 *
 * Handles:
 *   - CSI sequences  \x1b[ … m|G|K|H|F|A|B|C|D|J|P|X|@|r|l|h
 *   - OSC sequences  \x1b] … \x07  (terminal title / colour palette)
 *   - G0/G1 charset  \x1b( or \x1b)
 *   - Other two-byte  \x1b + 0x40-0x5F
 *   - 8-bit CSI      \x9b … (equivalent of \x1b[)
 *   - \r\n → \n  and  bare \r → \n
 */
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

export function useCommandOutput(commandId: string | undefined): string {
  const [output, setOutput] = useState('')

  useEffect(() => {
    if (!commandId) { setOutput(''); return }
    API.fetchOutput(commandId)
      .then(raw => setOutput(cleanOutput(raw)))
      .catch(() => setOutput(''))
  }, [commandId])

  return output
}

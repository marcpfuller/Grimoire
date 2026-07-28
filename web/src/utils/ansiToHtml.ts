/**
 * ansiToHtml converts a string containing ANSI SGR escape sequences into an
 * HTML string where each styled run is wrapped in a <span style="...">.
 *
 * Only SGR sequences (\x1b[...m and the 8-bit equivalent \x9b...m) are
 * processed; all other escape sequences have already been removed server-side
 * by scrub.Strip. Text content is HTML-escaped before insertion, making the
 * output safe to inject via dangerouslySetInnerHTML.
 *
 * Supported SGR codes:
 *   0          – reset
 *   1/3/4/9    – bold / italic / underline / strikethrough
 *   22/23/24/29 – cancel bold / italic / underline / strikethrough
 *   30–37      – standard foreground colours
 *   39         – default foreground
 *   40–47      – standard background colours
 *   49         – default background
 *   90–97      – bright foreground colours
 *   100–107    – bright background colours
 *   38;5;N     – 256-colour foreground
 *   48;5;N     – 256-colour background
 *   38;2;R;G;B – true-colour foreground
 *   48;2;R;G;B – true-colour background
 */

// VS Code dark terminal colour palette (matches the UI theme).
const ANSI_16: readonly string[] = [
    '#000000', // 0  black
    '#cd3131', // 1  red
    '#0dbc79', // 2  green
    '#e5e510', // 3  yellow
    '#2472c8', // 4  blue
    '#bc3fbc', // 5  magenta
    '#11a8cd', // 6  cyan
    '#e5e5e5', // 7  white
    '#666666', // 8  bright black
    '#f14c4c', // 9  bright red
    '#23d18b', // 10 bright green
    '#f5f543', // 11 bright yellow
    '#3b8eea', // 12 bright blue
    '#d670d6', // 13 bright magenta
    '#29b8db', // 14 bright cyan
    '#ffffff', // 15 bright white
]

function color256(n: number): string {
    if (n < 16) return ANSI_16[n]
    if (n > 231) {
        const v = 8 + (n - 232) * 10
        return `rgb(${v},${v},${v})`
    }
    const i = n - 16
    const b = i % 6
    const g = Math.floor(i / 6) % 6
    const r = Math.floor(i / 36)
    const toVal = (c: number): number => (c === 0 ? 0 : 55 + c * 40)
    return `rgb(${toVal(r)},${toVal(g)},${toVal(b)})`
}

interface SGRState {
    fg: string | null
    bg: string | null
    bold: boolean
    italic: boolean
    underline: boolean
    strikethrough: boolean
}

function emptyState(): SGRState {
    return { fg: null, bg: null, bold: false, italic: false, underline: false, strikethrough: false }
}

/** Applies a single SGR code (or multi-code sequence starting at idx) to state.
 *  Returns the next index to process. */
function applyCode(codes: number[], idx: number, state: SGRState): number {
    const code = codes[idx]
    switch (code) {
        case 0:
            Object.assign(state, emptyState())
            break
        case 1:
            state.bold = true
            break
        case 3:
            state.italic = true
            break
        case 4:
            state.underline = true
            break
        case 9:
            state.strikethrough = true
            break
        case 22:
            state.bold = false
            break
        case 23:
            state.italic = false
            break
        case 24:
            state.underline = false
            break
        case 29:
            state.strikethrough = false
            break
        case 39:
            state.fg = null
            break
        case 49:
            state.bg = null
            break
        case 38:
            if (codes[idx + 1] === 5 && codes[idx + 2] !== undefined) {
                state.fg = color256(codes[idx + 2])
                return idx + 3
            }
            if (codes[idx + 1] === 2 && codes[idx + 4] !== undefined) {
                state.fg = `rgb(${codes[idx + 2]},${codes[idx + 3]},${codes[idx + 4]})`
                return idx + 5
            }
            break
        case 48:
            if (codes[idx + 1] === 5 && codes[idx + 2] !== undefined) {
                state.bg = color256(codes[idx + 2])
                return idx + 3
            }
            if (codes[idx + 1] === 2 && codes[idx + 4] !== undefined) {
                state.bg = `rgb(${codes[idx + 2]},${codes[idx + 3]},${codes[idx + 4]})`
                return idx + 5
            }
            break
        default:
            if (code >= 30 && code <= 37) {
                state.fg = ANSI_16[code - 30]
            } else if (code >= 40 && code <= 47) {
                state.bg = ANSI_16[code - 40]
            } else if (code >= 90 && code <= 97) {
                state.fg = ANSI_16[code - 90 + 8]
            } else if (code >= 100 && code <= 107) {
                state.bg = ANSI_16[code - 100 + 8]
            }
    }
    return idx + 1
}

function stateToStyle(s: SGRState): string {
    const parts: string[] = []
    if (s.fg) parts.push(`color:${s.fg}`)
    if (s.bg) parts.push(`background-color:${s.bg}`)
    if (s.bold) parts.push('font-weight:bold')
    if (s.italic) parts.push('font-style:italic')
    const deco: string[] = []
    if (s.underline) deco.push('underline')
    if (s.strikethrough) deco.push('line-through')
    if (deco.length > 0) parts.push(`text-decoration:${deco.join(' ')}`)
    return parts.join(';')
}

function escapeHtml(s: string): string {
    return s
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
}

const CSI_SGR = /\x1b\[([0-9;]*)m|\x9b([0-9;]*)m/g

export function ansiToHtml(text: string): string {
    let html = ''
    let last = 0
    const state: SGRState = emptyState()

    CSI_SGR.lastIndex = 0
    let m: RegExpExecArray | null
    while ((m = CSI_SGR.exec(text)) !== null) {
        if (m.index > last) {
            const chunk = escapeHtml(text.slice(last, m.index))
            const style = stateToStyle(state)
            html += style ? `<span style="${style}">${chunk}</span>` : chunk
        }
        last = m.index + m[0].length

        const param = m[1] ?? m[2] ?? ''
        const codes = param.length > 0 ? param.split(';').map(Number) : [0]
        let i = 0
        while (i < codes.length) {
            i = applyCode(codes, i, state)
        }
    }

    if (last < text.length) {
        const chunk = escapeHtml(text.slice(last))
        const style = stateToStyle(state)
        html += style ? `<span style="${style}">${chunk}</span>` : chunk
    }

    return html
}

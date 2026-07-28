import { describe, it, expect } from 'vitest'
import { ansiToHtml } from './ansiToHtml'

describe('ansiToHtml', () => {
    it('passes through plain text unchanged', () => {
        expect(ansiToHtml('hello world')).toBe('hello world')
    })

    it('returns empty string for empty input', () => {
        expect(ansiToHtml('')).toBe('')
    })

    it('HTML-escapes < > & " in plain text', () => {
        expect(ansiToHtml('<b>&"x"</b>')).toBe('&lt;b&gt;&amp;&quot;x&quot;&lt;/b&gt;')
    })

    it('HTML-escapes text inside a coloured span', () => {
        expect(ansiToHtml('\x1b[32m<ok>\x1b[0m')).toBe(
            '<span style="color:#0dbc79">&lt;ok&gt;</span>'
        )
    })

    it('wraps standard foreground colour in a span', () => {
        // \x1b[34m = blue
        expect(ansiToHtml('\x1b[34mapi\x1b[0m')).toBe(
            '<span style="color:#2472c8">api</span>'
        )
    })

    it('wraps bright foreground colour in a span', () => {
        // \x1b[91m = bright red
        expect(ansiToHtml('\x1b[91merror\x1b[0m')).toBe(
            '<span style="color:#f14c4c">error</span>'
        )
    })

    it('reset (\\x1b[0m) closes style — trailing text has no span when default', () => {
        const result = ansiToHtml('\x1b[31mred\x1b[0m plain')
        expect(result).toBe('<span style="color:#cd3131">red</span> plain')
    })

    it('handles compound reset sequence 39;49 (default fg + bg)', () => {
        // This is what ls emits: [34mname[39;49m[0m
        const result = ansiToHtml('\x1b[34mname\x1b[39;49m\x1b[0m')
        expect(result).toBe('<span style="color:#2472c8">name</span>')
    })

    it('ls-style directory listing: blue dirs, red executables', () => {
        const input = '\x1b[34mapi\x1b[39;49m\x1b[0m plain \x1b[31mscript.sh\x1b[39;49m\x1b[0m'
        const result = ansiToHtml(input)
        expect(result).toBe(
            '<span style="color:#2472c8">api</span> plain <span style="color:#cd3131">script.sh</span>'
        )
    })

    it('applies bold', () => {
        expect(ansiToHtml('\x1b[1mBOLD\x1b[0m')).toBe('<span style="font-weight:bold">BOLD</span>')
    })

    it('applies italic', () => {
        expect(ansiToHtml('\x1b[3mitalic\x1b[0m')).toBe('<span style="font-style:italic">italic</span>')
    })

    it('applies underline', () => {
        expect(ansiToHtml('\x1b[4mline\x1b[0m')).toBe(
            '<span style="text-decoration:underline">line</span>'
        )
    })

    it('applies strikethrough', () => {
        expect(ansiToHtml('\x1b[9mstrike\x1b[0m')).toBe(
            '<span style="text-decoration:line-through">strike</span>'
        )
    })

    it('combines bold and colour in same span', () => {
        expect(ansiToHtml('\x1b[1;32mok\x1b[0m')).toBe(
            '<span style="color:#0dbc79;font-weight:bold">ok</span>'
        )
    })

    it('256-colour foreground (38;5;N)', () => {
        // 38;5;196 = colour index 196 = bright red in the 6x6x6 cube
        const result = ansiToHtml('\x1b[38;5;196mtext\x1b[0m')
        expect(result).toContain('<span style="color:')
        expect(result).toContain('text</span>')
    })

    it('256-colour foreground index < 16 maps to standard palette', () => {
        // 38;5;4 = ANSI_16[4] = blue (#2472c8)
        expect(ansiToHtml('\x1b[38;5;4mblue\x1b[0m')).toBe(
            '<span style="color:#2472c8">blue</span>'
        )
    })

    it('256-colour grayscale (232–255)', () => {
        // 38;5;232 = darkest gray, rgb(8,8,8)
        expect(ansiToHtml('\x1b[38;5;232mg\x1b[0m')).toBe(
            '<span style="color:rgb(8,8,8)">g</span>'
        )
    })

    it('true-colour foreground (38;2;R;G;B)', () => {
        expect(ansiToHtml('\x1b[38;2;255;100;50mtext\x1b[0m')).toBe(
            '<span style="color:rgb(255,100,50)">text</span>'
        )
    })

    it('true-colour background (48;2;R;G;B)', () => {
        expect(ansiToHtml('\x1b[48;2;0;128;0mtext\x1b[0m')).toBe(
            '<span style="background-color:rgb(0,128,0)">text</span>'
        )
    })

    it('8-bit CSI SGR (\\x9b...m) treated identically to \\x1b[...m', () => {
        // \x9b32m = 8-bit CSI green
        expect(ansiToHtml('\x9b32mhi\x9b0m')).toBe(
            '<span style="color:#0dbc79">hi</span>'
        )
    })

    it('plain text with no escapes is not wrapped in a span', () => {
        const result = ansiToHtml('no escapes here')
        expect(result).not.toContain('<span')
    })

    it('handles multiple consecutive SGR sequences without text between them', () => {
        // \x1b[1m\x1b[32m → bold + green, applied before any text
        expect(ansiToHtml('\x1b[1m\x1b[32mtext\x1b[0m')).toBe(
            '<span style="color:#0dbc79;font-weight:bold">text</span>'
        )
    })

    it('is idempotent on already-plain output', () => {
        const plain = 'just text\nmore text'
        expect(ansiToHtml(plain)).toBe(plain)
    })
})

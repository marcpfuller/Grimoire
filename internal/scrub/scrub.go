package scrub

// Strip removes VT/ANSI terminal control sequences that affect cursor position,
// screen state, OSC directives, charset selection, and similar, while preserving
// SGR sequences (colours and text attributes such as bold, underline, etc.) so
// that the returned string looks the same as what the operator saw in their
// terminal.
//
// CRLF pairs (\r\n) are normalised to LF. Bare CR bytes are retained so that
// progress-bar overwrite patterns are preserved. No other whitespace
// normalisation is performed.
//
// Strip is stateless; pass the full concatenated output in a single call.
func Strip(b []byte) string {
	type state int
	const (
		stNormal  state = iota
		stEsc           // consumed \x1b — waiting for sequence classifier
		stCSI           // inside CSI parameter/intermediate bytes (\x1b[ or \x9b)
		stOSC           // inside OSC string (\x1b] or \x9d) — consume until BEL or ST
		stOSCEsc        // inside OSC, saw \x1b — waiting for \ to confirm ST
		stStr           // inside DCS/SOS/PM/APC string (\x1bP/X/^/_)
		stStrEsc        // inside DCS etc., saw \x1b — waiting for \ to confirm ST
		stCharset        // after \x1b( / ) / * / + — consume one designator byte
	)

	out := make([]byte, 0, len(b))
	cur := stNormal

	// csiBuf accumulates the current CSI sequence including its introducer so
	// that we can emit the whole sequence if it turns out to be SGR.
	var csiBuf []byte

	for i := 0; i < len(b); i++ {
		c := b[i]

		switch cur {
		case stNormal:
			switch {
			case c == 0x1b:
				cur = stEsc
			case c == 0x9b: // 8-bit CSI (C1 equivalent of \x1b[)
				csiBuf = []byte{c}
				cur = stCSI
			case c == 0x9d: // 8-bit OSC (C1 equivalent of \x1b])
				cur = stOSC
			case c >= 0x80 && c <= 0x9f: // other C1 controls — discard
				// nothing
			case c == '\r':
				// Normalise CRLF → LF; keep bare CR for overwrite patterns.
				if i+1 < len(b) && b[i+1] == '\n' {
					out = append(out, '\n')
					i++ // consume the LF
				} else {
					out = append(out, '\r')
				}
			default:
				out = append(out, c)
			}

		case stEsc:
			switch c {
			case '[': // CSI introducer
				csiBuf = []byte{0x1b, '['}
				cur = stCSI
			case ']': // OSC
				cur = stOSC
			case 'P', 'X', '^', '_': // DCS / SOS / PM / APC
				cur = stStr
			case '(', ')', '*', '+': // charset designation
				cur = stCharset
			default:
				// Two-byte Fe/Fp/Ft escape (e.g. \x1bM reverse-index, \x1b= keypad).
				// Discard both bytes; already consumed \x1b, c is the second.
				cur = stNormal
			}

		case stCSI:
			csiBuf = append(csiBuf, c)
			// Parameter bytes: 0x30–0x3F. Intermediate bytes: 0x20–0x2F.
			// Final byte: 0x40–0x7E — this byte determines the command type.
			if c >= 0x40 && c <= 0x7e {
				if c == 'm' { // SGR — preserve colours and text attributes
					out = append(out, csiBuf...)
				}
				// All other CSI commands (movement, erase, etc.) are discarded.
				csiBuf = nil
				cur = stNormal
			}

		case stOSC:
			// OSC terminates with BEL (\x07) or String Terminator (\x1b\).
			switch c {
			case 0x07:
				cur = stNormal
			case 0x1b:
				cur = stOSCEsc
			}
			// else: consume and discard

		case stOSCEsc:
			// Any byte after \x1b inside OSC ends the string (ST = \x1b\).
			cur = stNormal

		case stStr:
			// DCS/SOS/PM/APC strings terminate with ST (\x1b\).
			if c == 0x1b {
				cur = stStrEsc
			}
			// else: consume and discard

		case stStrEsc:
			// Any byte after \x1b inside a string sequence ends it.
			cur = stNormal

		case stCharset:
			// Consume the single designator byte and discard.
			cur = stNormal
		}
	}
	// Any incomplete sequence at end-of-input is silently discarded (csiBuf dropped).

	return string(out)
}

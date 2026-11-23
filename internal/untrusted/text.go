// Package untrusted makes text from a VM safe to show on the host.
//
// Anything a guest reports (process names, log lines, container names) may be
// written by a hostile VM. Box only displays such values and never acts on
// them; this package removes what could act on the viewer instead, such as
// terminal escape sequences that move the cursor, retitle the window or
// write to the clipboard.
package untrusted

import (
	"strings"
	"unicode/utf8"
)

// MaxLine bounds a single displayed line, in bytes, before cleaning.
const MaxLine = 16 << 10

// Text returns s with escape sequences and control characters removed.
// Newlines and tabs are kept; invalid UTF-8 becomes U+FFFD.
func Text(s string) string {
	return clean(s, true)
}

// Line is Text for a single line: newlines and tabs become spaces, and the
// input is cut to MaxLine bytes first.
func Line(s string) string {
	if len(s) > MaxLine {
		s = s[:MaxLine]
	}
	return clean(s, false)
}

func clean(s string, keepLayout bool) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			out.WriteRune(utf8.RuneError)
		case r == 0x1b: // ESC: drop the whole sequence
			i += escapeLength(s[i:])
			continue
		case r == '\n' || r == '\t':
			if keepLayout {
				out.WriteRune(r)
			} else {
				out.WriteByte(' ')
			}
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Other C0 controls, DEL and C1 controls (0x9b is a one-byte CSI).
		case r == 0x2028 || r == 0x2029:
			out.WriteByte(' ')
		default:
			out.WriteString(s[i : i+size])
		}
		i += size
	}
	return out.String()
}

// escapeLength returns how many bytes of s, which starts with ESC, form one
// escape sequence. Unterminated sequences run to the end of s.
func escapeLength(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[': // CSI: parameters and intermediates, then a final byte 0x40–0x7e
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS: end at BEL or ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default: // two-byte sequences, and nF sequences with intermediates
		i := 1
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			i++
		}
		return i
	}
}

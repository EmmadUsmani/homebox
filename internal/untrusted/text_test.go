package untrusted

import (
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "hello, world", "hello, world"},
		{"layout kept", "a\tb\nc", "a\tb\nc"},
		{"colour", "\x1b[31mred\x1b[0m", "red"},
		{"cursor move", "a\x1b[2;5Hb", "ab"},
		{"window title BEL", "\x1b]0;pwned\x07ok", "ok"},
		{"clipboard ST", "\x1b]52;c;ZXZpbA==\x1b\\ok", "ok"},
		{"DCS", "\x1bPq#0;2;0;0;0\x1b\\ok", "ok"},
		{"two-byte", "a\x1bcb", "ab"},
		{"charset nF", "a\x1b(Bb", "ab"},
		{"C1 CSI", "a\u009b31mb", "a31mb"},
		{"controls", "a\x00\x07\x08\rb\x7f", "ab"},
		{"unterminated OSC", "ok\x1b]0;never ends", "ok"},
		{"lone ESC", "ok\x1b", "ok"},
		{"invalid utf8", "a\xffb", "a�b"},
		{"unicode kept", "héllo → 世界", "héllo → 世界"},
		{"line separators", "a b", "a b"},
	} {
		if got := Text(tc.in); got != tc.want {
			t.Errorf("%s: Text(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("a\tb\nc\x1b[1m"); got != "a b c" {
		t.Errorf("Line = %q", got)
	}
	if got := Line(strings.Repeat("x", MaxLine+10)); len(got) != MaxLine {
		t.Errorf("Line length = %d, want %d", len(got), MaxLine)
	}
}

func FuzzText(f *testing.F) {
	f.Add("\x1b]0;t\x07x\x1b[1mY")
	f.Fuzz(func(t *testing.T, in string) {
		out := Text(in)
		for _, r := range out {
			if r == 0x1b || (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
				t.Fatalf("Text(%q) kept control %U", in, r)
			}
		}
	})
}

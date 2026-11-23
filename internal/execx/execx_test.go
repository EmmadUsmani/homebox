package execx

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestStderrTail(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writes []string
	}{
		{"small writes", []string{"first", " second", " third"}},
		{"wrap across writes", []string{strings.Repeat("a", stderrTailLimit-2), "bcdef", "last"}},
		{"exact limit", []string{"old", strings.Repeat("a", stderrTailLimit)}},
		{"one huge write", []string{"old", strings.Repeat("a", 2*stderrTailLimit) + "end"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tail stderrTail
			for _, chunk := range tc.writes {
				n, err := io.WriteString(&tail, chunk)
				if err != nil || n != len(chunk) {
					t.Fatalf("write = %d, %v", n, err)
				}
			}
			all := strings.Join(tc.writes, "")
			want := all[max(0, len(all)-stderrTailLimit):]
			if got := string(tail.Bytes()); got != want {
				t.Fatalf("incorrect stderr tail (got %d bytes, want %d)", len(got), len(want))
			}
		})
	}
}

func TestExitErrorRetainsStderrTail(t *testing.T) {
	const runChild = "HOMEBOX_TEST_STDERR_CHILD"
	want := strings.Repeat("x", stderrTailLimit-4) + "last"
	full := "dropped-prefix" + want
	if os.Getenv(runChild) == "1" {
		_, _ = io.WriteString(os.Stderr, full)
		os.Exit(23)
	}
	for _, redirected := range []bool{false, true} {
		var streamed bytes.Buffer
		cmd := Cmd{
			Name: os.Args[0], Args: []string{"-test.run=^TestExitErrorRetainsStderrTail$"},
			Env: []string{runChild + "=1"},
		}
		if redirected {
			cmd.Stderr = &streamed
		}
		result, err := (OS{}).Run(t.Context(), cmd)
		var exit *ExitError
		if !errors.As(err, &exit) || result.ExitCode != 23 || string(result.Stderr) != want ||
			exit.Stderr != want || !strings.HasSuffix(exit.Error(), want) {
			t.Fatalf("redirected=%v: exit=%d, captured=%d bytes, error=%T", redirected, result.ExitCode, len(result.Stderr), err)
		}
		if redirected && streamed.String() != full {
			t.Fatal("redirected stderr did not receive the full output")
		}
	}
}

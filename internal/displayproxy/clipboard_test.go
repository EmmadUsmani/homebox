package displayproxy

import (
	"io"
	"strings"
	"testing"
)

func TestClipboardOutputBoundsSSHStream(t *testing.T) {
	var output clipboardOutput
	// Hide strings.Reader.WriteTo so io.Copy exercises the generic stream path.
	stream := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", ClipboardLimit*2))}
	n, err := io.Copy(&output, stream)
	if err != nil || n != ClipboardLimit*2 {
		t.Fatalf("copy = (%d, %v)", n, err)
	}
	if got := output.buffer.Len(); got != ClipboardLimit+1 {
		t.Fatalf("buffered %d bytes, want %d", got, ClipboardLimit+1)
	}
	if err := checkClipboardText(output.buffer.String()); err == nil {
		t.Fatal("oversized clipboard text passed validation")
	}
}

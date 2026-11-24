package displayproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

const ClipboardLimit = 1 << 20

const guestClipboard = "test -x /usr/local/lib/homebox-desktop/clipboard || exit 42; exec /usr/local/lib/homebox-desktop/clipboard "

func PasteClipboard(ctx context.Context, client *ssh.Client, value string) error {
	if err := checkClipboardText(value); err != nil {
		return err
	}
	return clipboardCommand(ctx, client, "paste", strings.NewReader(value), nil)
}

func CopyClipboard(ctx context.Context, client *ssh.Client) (string, error) {
	var output clipboardOutput
	if err := clipboardCommand(ctx, client, "copy", nil, &output); err != nil {
		return "", err
	}
	value := output.buffer.String()
	if err := checkClipboardText(value); err != nil {
		return "", err
	}
	return value, nil
}

func checkClipboardText(value string) error {
	if len(value) > ClipboardLimit {
		return errors.New("Clipboard text exceeds 1 MiB")
	}
	if !utf8.ValidString(value) {
		return errors.New("Clipboard does not contain valid UTF-8 text")
	}
	return nil
}

func clipboardCommand(ctx context.Context, client *ssh.Client, action string, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return clipboardError(runSSHCommand(ctx, client, guestClipboard+action, input, output, nil))
}

func clipboardError(err error) error {
	var exit *ssh.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitStatus() == 42:
		return errors.New("Update the guest desktop layer with box desktop NAME --install")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Clipboard transfer timed out")
	default:
		return errors.New("Could not transfer clipboard text")
	}
}

type clipboardOutput struct {
	buffer bytes.Buffer
}

func (out *clipboardOutput) Write(p []byte) (int, error) {
	remaining := ClipboardLimit + 1 - out.buffer.Len()
	_, _ = out.buffer.Write(p[:min(len(p), remaining)])
	return len(p), nil
}

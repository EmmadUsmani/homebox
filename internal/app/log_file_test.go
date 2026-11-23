package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

func TestFollowLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "following.log")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	lines := make(chan string, 10)
	done := make(chan error, 1)
	firstMissingPoll := make(chan struct{})
	go func() {
		polls := 0
		done <- streamLogFile(ctx, func(context.Context) (string, error) {
			polls++
			if polls == 2 {
				close(firstMissingPoll)
			}
			return path, nil
		}, LogOptions{Follow: true, Lines: 5}, func(line string, _ bool) error {
			lines <- line
			return nil
		})
	}()
	// Wait until the watcher is running, then let the path appear.
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("file watcher did not start")
	case <-firstMissingPoll:
	}
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("follow line = %q, want %q", got, want)
			}
		case err := <-done:
			t.Fatalf("follow stopped: %v", err)
		case <-ctx.Done():
			t.Fatalf("missing follow line %q", want)
		}
	}
	writeLog(t, path, "created\npartial")
	expect("created")
	select {
	case line := <-lines:
		t.Fatalf("emitted partial line %q", line)
	case <-time.After(2 * logPollInterval):
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(file, " line\nappended\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	expect("partial line")
	expect("appended")
	writeLog(t, path, "truncated\n")
	expect("truncated")
	writeLog(t, path, "rewritten and larger than the old file\n")
	expect("rewritten and larger than the old file")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, "rotated\n")
	expect("rotated")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("follow cancellation = %v", err)
	}
}

func TestTailLargeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Leave a large sparse prefix. Walking forward would read a gigabyte.
	if _, err := file.Seek(1<<30, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(file, "\nignored\n"+strings.Repeat("x", untrusted.MaxLine+1)+"\nlast partial")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	var lines []string
	var truncated []bool
	err = streamLogFile(t.Context(), func(context.Context) (string, error) { return path, nil },
		LogOptions{Lines: 2}, func(line string, clipped bool) error {
			lines = append(lines, line)
			truncated = append(truncated, clipped)
			return nil
		})
	if err != nil || len(lines) != 2 || len(lines[0]) != untrusted.MaxLine || !truncated[0] ||
		lines[1] != "last partial" || truncated[1] {
		t.Fatalf("tail failed: got %d lines, truncation %v, error %v", len(lines), truncated, err)
	}
}

func writeLog(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

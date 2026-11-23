package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

const logPollInterval = 250 * time.Millisecond

// logFramer bounds the memory used for an incoming line, even when the writer
// never sends a newline. Only the first MaxLine bytes are retained.
type logFramer struct {
	pending   []byte
	truncated bool
	emit      func(string, bool) error
}

func (f *logFramer) Write(data []byte) (int, error) {
	for offset := 0; offset < len(data); {
		rest := data[offset:]
		length := bytes.IndexByte(rest, '\n')
		complete := length >= 0
		if !complete {
			length = len(rest)
		}
		keep := min(length, untrusted.MaxLine-len(f.pending))
		f.pending = append(f.pending, rest[:keep]...)
		f.truncated = f.truncated || keep < length
		offset += length
		if complete {
			offset++
			if err := f.finish(); err != nil {
				return offset, err
			}
		}
	}
	return len(data), nil
}

func (f *logFramer) finish() error {
	err := f.emit(string(f.pending), f.truncated)
	f.pending = f.pending[:0]
	f.truncated = false
	return err
}

// tailOffset finds the start of the last count lines without reading the
// preceding lines. An unterminated final line counts toward count.
func tailOffset(ctx context.Context, file *os.File, size int64, count int) (int64, error) {
	if count == 0 {
		return size, nil
	}
	var block [8 << 10]byte
	for end := size; end > 0; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		start := max(0, end-int64(len(block)))
		if _, err := file.ReadAt(block[:end-start], start); err != nil {
			return 0, err
		}
		for i := end - start - 1; i >= 0; i-- {
			if block[i] == '\n' && start+i != size-1 {
				count--
				if count == 0 {
					return start + i + 1, nil
				}
			}
		}
		end = start
	}
	return 0, nil
}

type logFileReader struct {
	file   *os.File
	info   os.FileInfo
	offset int64
	anchor []byte
	framer logFramer
}

func (r *logFileReader) drain(ctx context.Context, size int64) error {
	// Compare the bytes just before the offset to detect a truncate-and-
	// rewrite that has already grown past the old size between polls.
	var check [64]byte
	if size < r.offset || (len(r.anchor) > 0 &&
		(!readAtMatches(r.file, r.offset-int64(len(r.anchor)), check[:len(r.anchor)], r.anchor))) {
		r.offset, r.anchor = 0, nil
		r.framer.pending, r.framer.truncated = nil, false
	}
	var block [8 << 10]byte
	for r.offset < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.file.ReadAt(block[:min(int64(len(block)), size-r.offset)], r.offset)
		if n > 0 {
			if _, writeErr := r.framer.Write(block[:n]); writeErr != nil {
				return writeErr
			}
			r.offset += int64(n)
			r.anchor = append(r.anchor[:0], block[max(0, n-len(check)):n]...)
		}
		if err == io.EOF {
			break // The writer may have truncated the file during this read.
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func readAtMatches(file *os.File, offset int64, got, want []byte) bool {
	_, err := file.ReadAt(got, offset)
	return err == nil && bytes.Equal(got, want)
}

func streamLogFile(ctx context.Context, path func(context.Context) (string, error), options LogOptions, emit func(string, bool) error) error {
	var reader *logFileReader
	defer func() {
		if reader != nil {
			reader.file.Close()
		}
	}()
	count := options.Lines
	poll := func() error {
		filename, err := path(ctx)
		if err != nil {
			return err
		}
		info, err := os.Stat(filename)
		missing := filename == "" || os.IsNotExist(err)
		if err != nil && !missing {
			return err
		}
		if reader != nil {
			current, err := reader.file.Stat()
			if err != nil {
				return err
			}
			if err := reader.drain(ctx, current.Size()); err != nil {
				return err
			}
			if missing || os.SameFile(reader.info, info) {
				return nil
			}
			reader.file.Close()
			reader = nil
		}
		if missing {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("log path %q is not a regular file", filename)
		}
		file, err := os.Open(filename)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		reader = &logFileReader{file: file, framer: logFramer{emit: emit}}
		reader.info, err = file.Stat()
		if err != nil {
			return err
		}
		size := reader.info.Size()
		if count >= 0 {
			reader.offset, err = tailOffset(ctx, file, size, count)
			if err == io.EOF && options.Follow {
				reader.offset = 0 // Truncated before the initial tail finished.
			} else if err != nil {
				return err
			}
			if count == 0 && size > 0 {
				reader.anchor = make([]byte, min(size, 64))
				if _, err := file.ReadAt(reader.anchor, size-int64(len(reader.anchor))); err != nil {
					reader.anchor = nil
				}
			}
		}
		return reader.drain(ctx, size)
	}
	if err := poll(); err != nil {
		return err
	}
	if !options.Follow {
		if reader != nil && len(reader.framer.pending) > 0 {
			return reader.framer.finish()
		}
		return nil
	}
	count = -1 // Files created or rotated after subscription are read from the start.
	ticker := time.NewTicker(logPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := poll(); err != nil {
				return err
			}
		}
	}
}

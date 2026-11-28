// Package diskcopy copies VM disks while preserving sparse regions.
package diskcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
)

// File clones a disk when possible, falling back to a sparse copy.
func File(ctx context.Context, source, destination string) error {
	return FileWithFallback(ctx, source, destination, nil)
}

// FileWithFallback calls onFallback before starting a sparse copy.
func FileWithFallback(ctx context.Context, source, destination string, onFallback func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if clone(source, destination) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if onFallback != nil {
		onFallback()
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyBuffer(SparseWriter{output}, ContextReader{ctx, input}, make([]byte, 1<<20))
	err = errors.Join(copyErr, output.Close())
	if err != nil {
		_ = os.Remove(destination)
	}
	return err
}

// ContextReader checks cancellation between reads.
type ContextReader struct {
	Context context.Context
	Reader  io.Reader
}

func (r ContextReader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// SparseWriter represents all-zero writes as holes, including at EOF.
type SparseWriter struct {
	File *os.File
}

var zeros [1 << 20]byte

func (w SparseWriter) Write(p []byte) (int, error) {
	if len(p) > len(zeros) || !bytes.Equal(p, zeros[:len(p)]) {
		return w.File.Write(p)
	}
	end, err := w.File.Seek(int64(len(p)), io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if err := w.File.Truncate(end); err != nil {
		return 0, err
	}
	return len(p), nil
}

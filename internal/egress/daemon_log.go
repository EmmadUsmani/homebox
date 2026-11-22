package egress

import (
	"fmt"
	"io"
	"os"
	"sync"
)

const daemonLogLimit = 10 << 20

// OpenDaemonLog opens an append log capped at 10 MiB with one rotated backup.
func OpenDaemonLog(path string) (io.WriteCloser, error) {
	return openDaemonLog(path, daemonLogLimit)
}

type daemonLog struct {
	mu    sync.Mutex
	path  string
	limit int64
	size  int64
	file  *os.File
}

func openDaemonLog(path string, limit int64) (*daemonLog, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	log := &daemonLog{path: path, limit: limit, size: info.Size(), file: file}
	if log.size > limit {
		// A log left by an older version can already exceed the rotation limit.
		if err := file.Truncate(limit); err != nil {
			file.Close()
			return nil, err
		}
		log.size = limit
	}
	return log, nil
}

func (l *daemonLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return 0, os.ErrClosed
	}
	size := int64(len(data))
	if size > l.limit {
		return 0, fmt.Errorf("log record exceeds %d bytes", l.limit)
	}
	if size > l.limit-l.size {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := l.file.Write(data)
	l.size += int64(written)
	return written, err
}

func (l *daemonLog) rotate() error {
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return err
	}
	next, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// Restore the active path when possible so future writes can retry.
		if restoreErr := os.Rename(l.path+".1", l.path); restoreErr != nil {
			l.file.Close()
			l.file = nil
		}
		return err
	}
	l.file.Close()
	l.file, l.size = next, 0
	return nil
}

func (l *daemonLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return os.ErrClosed
	}
	err := l.file.Close()
	l.file = nil
	return err
}

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

func (log *daemonLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return 0, os.ErrClosed
	}
	size := int64(len(data))
	if size > log.limit {
		return 0, fmt.Errorf("log record exceeds %d bytes", log.limit)
	}
	if size > log.limit-log.size {
		if err := log.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := log.file.Write(data)
	log.size += int64(written)
	return written, err
}

func (log *daemonLog) rotate() error {
	if err := os.Rename(log.path, log.path+".1"); err != nil {
		return err
	}
	next, err := os.OpenFile(log.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// Restore the active path when possible so future writes can retry.
		if restoreErr := os.Rename(log.path+".1", log.path); restoreErr != nil {
			log.file.Close()
			log.file = nil
		}
		return err
	}
	log.file.Close()
	log.file, log.size = next, 0
	return nil
}

func (log *daemonLog) Close() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return os.ErrClosed
	}
	err := log.file.Close()
	log.file = nil
	return err
}

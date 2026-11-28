package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// AutostartRun restores the marked boxes independently, using the normal start path.
func (s *Service) AutostartRun(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return errors.New("autostart must run as the box owner")
	}
	if runtime.GOOS == "darwin" {
		// Open the log after dropping to the owner through launchd's UserName.
		// Replace the previous run so unattended logs do not accumulate.
		path := filepath.Join(s.Store.Root, "autostart.log")
		if err := os.MkdirAll(s.Store.Root, 0700); err != nil {
			return err
		}
		log, err := os.CreateTemp(s.Store.Root, ".autostart-log-*")
		if err != nil {
			return err
		}
		defer log.Close()
		defer os.Remove(log.Name())
		if err := os.Rename(log.Name(), path); err != nil {
			return err
		}
		service := *s
		service.Progress = io.MultiWriter(os.Stderr, log)
		return service.restoreMarked(ctx)
	}
	return s.restoreMarked(ctx)
}

func (s *Service) restoreMarked(ctx context.Context) error {
	s.progress("Autostart: scanning box records at %s", time.Now().Format(time.RFC3339))
	names, err := s.Store.BoxNames()
	if err != nil {
		s.progress("Autostart: %v", err)
		return err
	}
	var failures []error
	for _, name := range names {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		err := s.Store.WithLock(func() error {
			// Recheck the record under the lock in case box stop cleared the mark.
			return s.startRecorded(ctx, name, true)
		})
		if err != nil {
			s.progress("Autostart: %s failed: %v", name, err)
			failures = append(failures, fmt.Errorf("autostart %s: %w", name, err))
		}
	}
	s.progress("Autostart: finished with %d failure(s)", len(failures))
	return errors.Join(failures...)
}

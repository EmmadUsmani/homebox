package app

import (
	"context"
	"errors"

	"github.com/EmmadUsmani/homebox/internal/sshx"
)

// Repin replaces the saved pins for a running box after the caller reviews
// fingerprints. The state lock keeps the reviewed keys and saved keys together.
func (s *Service) Repin(ctx context.Context, name string, confirm func(old, new []string) (bool, error)) error {
	if confirm == nil {
		return errors.New("re-pinning requires explicit confirmation")
	}
	return s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		endpoint, keys, err := s.readSSHPins(ctx, name)
		if err != nil {
			return err
		}
		oldFingerprints, err := fingerprints(box.SSHHostKeys)
		if err != nil {
			return err
		}
		newFingerprints, err := fingerprints(keys)
		if err != nil {
			return err
		}
		approved, err := confirm(oldFingerprints, newFingerprints)
		if err != nil || !approved {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.saveSSHPins(&box, endpoint, keys)
	})
}

func fingerprints(keys []string) ([]string, error) {
	fingerprints := make([]string, len(keys))
	for index, key := range keys {
		fingerprint, err := sshx.Fingerprint(key)
		if err != nil {
			return nil, err
		}
		fingerprints[index] = fingerprint
	}
	return fingerprints, nil
}

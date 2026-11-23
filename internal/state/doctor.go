package state

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

type DoctorResult struct {
	CheckedAt     time.Time `json:"checked_at"`
	Passed        int       `json:"passed"`
	Failed        int       `json:"failed"`
	Skipped       int       `json:"skipped"`
	FailingChecks []string  `json:"failing_checks,omitempty"`
}

func (s *Store) LoadDoctorResult(name string) (*DoctorResult, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	var result DoctorResult
	err := readJSON(filepath.Join(s.BoxDir(name), "doctor.json"), &result)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) SaveDoctorResult(box Box, result DoctorResult) error {
	return s.WithLock(func() error {
		current, err := s.Load(box.Name)
		if err != nil {
			return err
		}
		if !current.CreatedAt.Equal(box.CreatedAt) {
			return errors.New("box was replaced during doctor")
		}
		previous, err := s.LoadDoctorResult(box.Name)
		if err == nil && previous != nil && previous.CheckedAt.After(result.CheckedAt) {
			return nil
		}
		return writeJSON(filepath.Join(s.BoxDir(box.Name), "doctor.json"), result)
	})
}

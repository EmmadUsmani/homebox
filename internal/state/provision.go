package state

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ProvisionRun records the latest provisioning attempt for a box. The process
// running it saves the record when it starts, at each step and when it ends;
// readers such as box status treat a run with no FinishedAt whose PID is gone
// as interrupted.
type ProvisionRun struct {
	PID        int        `json:"pid"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Step       string     `json:"step,omitempty"`  // the current step, or the one that failed
	Error      string     `json:"error,omitempty"` // set when the run failed
	Log        string     `json:"log,omitempty"`   // file name in ProvisionDir
}

// ProvisionDir holds a box's provisioning run record and per-run logs.
func (s *Store) ProvisionDir(name string) string {
	return filepath.Join(s.BoxDir(name), "provision")
}

// LoadProvisionRun reads the latest run. ok is false when no run was recorded.
func (s *Store) LoadProvisionRun(name string) (run ProvisionRun, ok bool, err error) {
	if err := ValidateName(name); err != nil {
		return run, false, err
	}
	err = readJSON(filepath.Join(s.ProvisionDir(name), "last.json"), &run)
	if errors.Is(err, os.ErrNotExist) {
		return ProvisionRun{}, false, nil
	}
	return run, err == nil, err
}

// SaveProvisionRun atomically replaces the latest run record.
func (s *Store) SaveProvisionRun(name string, run ProvisionRun) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.ProvisionDir(name), "last.json"), run)
}

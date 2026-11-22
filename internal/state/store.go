// Package state persists host settings and box records.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Box describes a VM managed by box.
type Box struct {
	Name          string           `json:"name"`
	Profile       string           `json:"profile"`
	Driver        string           `json:"driver"`
	Index         int              `json:"index"`
	CPUs          int              `json:"cpus"`
	MemoryGiB     int              `json:"memory_gib"`
	DiskGiB       int              `json:"disk_gib"`
	User          string           `json:"user"`
	SSHPort       int              `json:"ssh_port"`
	Forwards      []vm.PortForward `json:"forwards"`
	Provisioned   bool             `json:"provisioned"`
	ProvisionedAt *time.Time       `json:"provisioned_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	ProxyURL      string           `json:"proxy_url,omitempty"`
	NoProxy       string           `json:"no_proxy,omitempty"`
	DNS           string           `json:"dns,omitempty"`
}

// Host holds the host-wide settings for VM creation and provisioning.
type Host struct {
	LAN            bool     `json:"lan"`
	AuthorizedKeys []string `json:"authorized_keys"`
}

// DefaultRoot resolves HOMEBOX_HOME or the default state directory.
func DefaultRoot() (string, error) {
	if root := os.Getenv("HOMEBOX_HOME"); root != "" {
		return filepath.Abs(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".homebox"), nil
}

// Store reads and writes state beneath Root.
type Store struct {
	Root string
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

// ValidateName checks that name can be used as a box identifier.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid box name %q: use 1–31 lowercase letters, digits or hyphens, starting with a letter", name)
	}
	return nil
}

func (s *Store) boxesDir() string {
	return filepath.Join(s.Root, "boxes")
}

// BoxDir returns the state directory for a box. Callers validate the name
// before accessing files there.
func (s *Store) BoxDir(name string) string {
	return filepath.Join(s.boxesDir(), name)
}

func (s *Store) boxPath(name string) string {
	return filepath.Join(s.BoxDir(name), "box.json")
}

// Load reads a named box record.
func (s *Store) Load(name string) (Box, error) {
	var box Box
	if err := ValidateName(name); err != nil {
		return box, err
	}
	err := readJSON(s.boxPath(name), &box)
	if err == nil && box.Name != name {
		err = fmt.Errorf("box record for %q contains name %q", name, box.Name)
	}
	return box, err
}

// Save atomically replaces a box record.
func (s *Store) Save(box Box) error {
	if err := ValidateName(box.Name); err != nil {
		return err
	}
	return writeJSON(s.boxPath(box.Name), box)
}

// Delete removes a box record. Runtime files are owned by their respective components.
func (s *Store) Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	err := os.Remove(s.boxPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// List returns records in name order.
func (s *Store) List() ([]Box, error) {
	entries, err := os.ReadDir(s.boxesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var boxes []Box
	for _, entry := range entries {
		if entry.IsDir() {
			box, err := s.Load(entry.Name())
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			boxes = append(boxes, box)
		}
	}
	return boxes, nil
}

// NextIndex returns the lowest unused index. Hold WithLock through the subsequent Save.
// HOMEBOX_FIRST_INDEX lets tests reserve a different port range beside real VMs.
func (s *Store) NextIndex() (int, error) {
	first := 1
	if value := os.Getenv("HOMEBOX_FIRST_INDEX"); value != "" {
		var err error
		first, err = strconv.Atoi(value)
		if err != nil || first < 1 || first > MaxIndex {
			return 0, errors.New("HOMEBOX_FIRST_INDEX must be an integer from 1 to 99")
		}
	}
	boxes, err := s.List()
	if err != nil {
		return 0, err
	}
	used := make(map[int]bool, len(boxes))
	for _, box := range boxes {
		used[box.Index] = true
	}
	for index := first; index <= MaxIndex; index++ {
		if !used[index] {
			return index, nil
		}
	}
	return 0, fmt.Errorf("all box indices from %d through %d are in use", first, MaxIndex)
}

// LoadHost reads host settings, defaulting to loopback and no extra keys.
func (s *Store) LoadHost() (Host, error) {
	var host Host
	err := readJSON(filepath.Join(s.Root, "host.json"), &host)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return host, err
}

// SaveHost atomically replaces host settings.
func (s *Store) SaveHost(host Host) error {
	return writeJSON(filepath.Join(s.Root, "host.json"), host)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

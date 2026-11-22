package state

import "path/filepath"

// SSHConfigPath is the generated OpenSSH configuration file.
func (s *Store) SSHConfigPath() string {
	return filepath.Join(s.Root, "ssh", "config")
}

// SSHKnownHostsPath is the generated OpenSSH host-key file.
func (s *Store) SSHKnownHostsPath() string {
	return filepath.Join(s.Root, "ssh", "known_hosts")
}

// SaveSSH publishes each file atomically. Publish pins before exposing aliases.
func (s *Store) SaveSSH(config, knownHosts string) error {
	if err := WritePrivateFile(s.SSHKnownHostsPath(), []byte(knownHosts)); err != nil {
		return err
	}
	return WritePrivateFile(s.SSHConfigPath(), []byte(config))
}

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type remoteSettings struct {
	RemoteHosts []string        `json:"remoteHosts,omitempty"`
	Window      json.RawMessage `json:"window,omitempty"`
}

func readRemoteSettings(path string) (remoteSettings, error) {
	var settings remoteSettings
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(data, &settings)
	return settings, err
}

func (settings remoteSettings) save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(settings); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

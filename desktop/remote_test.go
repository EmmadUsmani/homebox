package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRemoteHostInventory(t *testing.T) {
	input := `# Laptop export
Host=alpha ALPHA beta * !exclude
  HostName dev.example.test # comment
Match host ignored
  HostName ignored.example.test
HOST delta
  HostName delta.example.test
Host -bad @invalid
`
	hosts, err := parseRemoteHosts(strings.NewReader(input))
	want := []RemoteHost{
		{ID: "alpha", Name: "alpha", Host: "dev.example.test"},
		{ID: "beta", Name: "beta", Host: "dev.example.test"},
		{ID: "delta", Name: "delta", Host: "delta.example.test"},
	}
	if err != nil || !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %+v, error = %v", hosts, err)
	}
}

func TestRemoteSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app", "settings.json")
	remote := &Remote{settingsPath: path, inventoryPath: filepath.Join(dir, "missing")}
	if err := (remoteSettings{Window: json.RawMessage(`{"width":1280}`)}).save(path); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, alias := range []string{"one", "two", "three", "one"} {
		group.Go(func() {
			if err := remote.AddHost(alias); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if err := remote.AddHost("-o"); err == nil {
		t.Fatal("accepted an invalid alias")
	}
	settings, err := readRemoteSettings(path)
	if err != nil || len(settings.RemoteHosts) != 3 || string(settings.Window) != `{"width":1280}` {
		t.Fatalf("settings = %+v, error = %v", settings, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("settings mode: %v", info.Mode().Perm())
	}
	items, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(items) != 1 {
		t.Fatalf("settings directory = %v, error = %v", items, err)
	}
}

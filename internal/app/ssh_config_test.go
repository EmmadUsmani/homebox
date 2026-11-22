package app

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestSSHPinsLifecycle(t *testing.T) {
	service, _, ssh, _ := fixture(t)
	ctx := t.Context()
	const name = "main"
	if err := service.New(ctx, NewOptions{Name: name}); err != nil {
		t.Fatal(err)
	}
	expectedPins := "homebox-main " + fixtureHostKey + "\n"
	record, err := service.Store.Load(name)
	if err != nil || !slices.Equal(record.SSHHostKeys, []string{fixtureHostKey}) || record.SSHIdentityFile != "/test/driver-id" {
		t.Fatalf("stored SSH settings = %+v, %v", record, err)
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	configPath := service.Store.SSHConfigPath()
	pinsPath := service.Store.SSHKnownHostsPath()
	config := read(configPath)
	if !strings.Contains(config, "HostName 127.0.0.1\n") ||
		!strings.Contains(config, `IdentityFile "/test/driver-id"`+"\n") ||
		read(pinsPath) != expectedPins {
		t.Fatalf("unexpected generated SSH files: %q, %q", config, read(pinsPath))
	}
	service.LocalHostName = nil
	if pins, err := service.SSHConfig(ctx, SSHConfigOptions{KnownHosts: true}); err != nil || pins != expectedPins {
		t.Fatalf("exported pins = %q, %v", pins, err)
	}
	if line, err := service.SSHConfig(ctx, SSHConfigOptions{Include: true}); err != nil || line != "Include \""+configPath+"\"\n" {
		t.Fatalf("exported Include = %q, %v", line, err)
	}
	ssh.hostKeys = []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB"}
	if err := service.Start(ctx, name); err == nil || !strings.Contains(err.Error(), "host keys changed") {
		t.Fatalf("changed pins accepted: %v", err)
	}
	if read(pinsPath) != expectedPins {
		t.Fatal("changed pins replaced known_hosts")
	}
	if err := service.Stop(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ports(ctx, name, PortOptions{Set: []string{"9000:30000"}}); err != nil {
		t.Fatal(err)
	}
	if read(configPath) != config {
		t.Fatal("port update did not regenerate the local SSH config")
	}
	if err := service.Remove(ctx, name); err != nil {
		t.Fatal(err)
	}
	if read(configPath) != "" || read(pinsPath) != "" {
		t.Fatal("removed VM still has local SSH settings")
	}
}

func TestStartPinsAnExistingBox(t *testing.T) {
	service, _, _, _ := fixture(t)
	ctx := t.Context()
	if err := service.New(ctx, NewOptions{Name: "legacy"}); err != nil {
		t.Fatal(err)
	}
	record, err := service.Store.Load("legacy")
	if err != nil {
		t.Fatal(err)
	}
	record.SSHHostKeys, record.SSHIdentityFile = nil, ""
	if err := service.Store.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SSHConfig(ctx, SSHConfigOptions{KnownHosts: true}); err == nil || !strings.Contains(err.Error(), "box start legacy") {
		t.Fatalf("export of legacy box = %v", err)
	}
	if err := service.Start(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
	pins, err := service.SSHConfig(ctx, SSHConfigOptions{KnownHosts: true})
	if err != nil || pins != "homebox-legacy "+fixtureHostKey+"\n" {
		t.Fatalf("legacy pins = %q, %v", pins, err)
	}
}

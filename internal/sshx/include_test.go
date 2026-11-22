package sshx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	generated := filepath.Join(home, "state with spaces", "ssh", "config")
	line, err := IncludeLine(generated)
	if err != nil || line != "Include \""+generated+"\"\n" {
		t.Fatalf("Include line = %q, %v", line, err)
	}

	dir := filepath.Join(home, ".ssh")
	config := filepath.Join(dir, "config")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := "# existing settings\nHost personal\n  User builder"
	if err := os.WriteFile(config, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := InstallInclude(generated); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(config)
	if err != nil || string(got) != line+original {
		t.Fatalf("installed config = %q, %v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "config.homebox-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	got, err = os.ReadFile(backups[0])
	if err != nil || string(got) != original {
		t.Fatalf("backup = %q, %v", got, err)
	}

	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := InstallInclude(generated); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(config)
	if err != nil || string(got) != line {
		t.Fatalf("new config = %q, %v", got, err)
	}
}

func TestLocalSSHGoldens(t *testing.T) {
	entries := []ConfigEntry{{
		Name: "main", Host: "127.0.0.1", Port: 2201, User: "builder",
		IdentityFile: "/test state/driver/id", HostKeys: []string{testHostKey},
	}}
	config, err := RenderConfig(entries, "/test state/ssh/known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := RenderKnownHosts(entries)
	if err != nil {
		t.Fatal(err)
	}
	for path, got := range map[string]string{"local-config.golden": config, "known-hosts.golden": pins} {
		want, err := os.ReadFile(filepath.Join("testdata", path))
		if err != nil || got != string(want) {
			t.Fatalf("%s = %q, want %q (%v)", path, got, want, err)
		}
	}
}

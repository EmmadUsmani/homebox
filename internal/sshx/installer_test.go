package sshx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstaller(t *testing.T) {
	entry := ConfigEntry{Name: "main", Host: "example.test", Port: 2201, User: "developer", HostKeys: []string{testHostKey}}
	render := func(t *testing.T, entry ConfigEntry) string {
		t.Helper()
		script, err := RenderInstaller([]ConfigEntry{entry})
		if err != nil {
			t.Fatal(err)
		}
		return script
	}
	script := render(t, entry)
	t.Run("shellcheck", func(t *testing.T) {
		if _, err := exec.LookPath("shellcheck"); err != nil {
			t.Skip("shellcheck unavailable")
		}
		cmd := exec.CommandContext(t.Context(), "shellcheck", "--shell=sh", "-")
		cmd.Stdin = strings.NewReader(script)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shellcheck: %v\n%s", err, output)
		}
	})
	for _, shell := range []string{"sh", "dash"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s unavailable", shell)
			}
			for _, existing := range []string{"absent", "regular", "symlink"} {
				t.Run(existing, func(t *testing.T) {
					home := t.TempDir()
					sshDir := filepath.Join(home, ".ssh")
					config := filepath.Join(sshDir, "config")
					var original string
					if existing != "absent" {
						if err := os.Mkdir(sshDir, 0700); err != nil {
							t.Fatal(err)
						}
						original = "Host *\n  ForwardAgent no"
						if existing == "symlink" {
							if err := os.Symlink("../dotfiles-config", config); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(filepath.Join(home, "dotfiles-config"), []byte(original), 0600); err != nil {
								t.Fatal(err)
							}
						} else if err := os.WriteFile(config, []byte(original), 0600); err != nil {
							t.Fatal(err)
						}
					}

					run := func(script string) {
						t.Helper()
						cmd := exec.CommandContext(t.Context(), shell)
						cmd.Env = append(os.Environ(), "HOME="+home)
						cmd.Stdin = strings.NewReader(script)
						if output, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("installer: %v\n%s", err, output)
						}
					}
					run(script)
					run(script)
					changed := entry
					changed.Host = "new.example.test"
					changed.HostKeys = []string{strings.TrimSuffix(testHostKey, "A") + "B"}
					run(render(t, changed))
					assertFile := func(path, want string) {
						t.Helper()
						data, err := os.ReadFile(path)
						if err != nil || string(data) != want {
							t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
						}
						info, err := os.Stat(path)
						if err != nil || info.Mode().Perm() != 0600 {
							t.Fatalf("expected a private file at %s: %v, %v", path, info, err)
						}
					}
					assertFile(config, "Include ~/.ssh/homebox_config\n"+original)
					wantConfig, err := RenderConfig([]ConfigEntry{changed}, "~/.ssh/homebox_known_hosts")
					if err != nil {
						t.Fatal(err)
					}
					assertFile(filepath.Join(sshDir, "homebox_config"), wantConfig)
					assertFile(filepath.Join(sshDir, "homebox_known_hosts"), "homebox-main "+changed.HostKeys[0]+"\n")
					backups, err := filepath.Glob(filepath.Join(sshDir, "config.homebox-backup.*"))
					if err != nil || (existing == "absent" && len(backups) != 0) || (existing != "absent" && len(backups) != 1) {
						t.Fatalf("unexpected backups: %v, %v", backups, err)
					}
					if len(backups) == 1 {
						assertFile(backups[0], original)
					}
					if existing == "symlink" {
						if target, err := os.Readlink(config); err != nil || target != "../dotfiles-config" {
							t.Fatalf("config symlink = %q, %v", target, err)
						}
					}
				})
			}
		})
	}
}

func TestInstallerRejectsUnsafeInputs(t *testing.T) {
	entry := ConfigEntry{Name: "main", Host: "example.test", Port: 2201, User: "developer", HostKeys: []string{testHostKey}}
	script, err := RenderInstaller([]ConfigEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	entry.Host = "example.test\nHOMEBOX_SSH_EOF"
	if _, err := RenderInstaller([]ConfigEntry{entry}); err == nil {
		t.Fatal("installer accepted an injected delimiter")
	}
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	run := func(script string) error {
		cmd := exec.CommandContext(t.Context(), "sh")
		cmd.Stdin = strings.NewReader(script)
		cmd.Env = append(os.Environ(), "HOME="+home)
		return cmd.Run()
	}

	truncated, _, _ := strings.Cut(script, "HOMEBOX_SSH_EOF\n")
	if err := run(truncated); err == nil {
		t.Fatal("incomplete script succeeded")
	}
	if _, err := os.Stat(sshDir); !os.IsNotExist(err) {
		t.Fatalf("incomplete script touched the SSH directory: %v", err)
	}

	if err := os.Mkdir(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(sshDir, "config")); err != nil {
		t.Fatal(err)
	}
	if err := run(script); err == nil {
		t.Fatal("installer accepted a dangling config symlink")
	}
	if names, err := os.ReadDir(sshDir); err != nil || len(names) != 1 || names[0].Name() != "config" {
		t.Fatalf("failed install left files behind: %v, %v", names, err)
	}
}

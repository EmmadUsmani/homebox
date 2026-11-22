package sshx

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

var endpoint = vm.Endpoint{Host: "127.0.0.1", Port: 2201, User: "developer", IdentityFile: "test identity"}

func TestRunScript(t *testing.T) {
	fake := &execx.Fake{}
	exit := &execx.ExitError{ExitCode: 23}
	fake.On("ssh ", "", exit)
	client := Client{Runner: fake}
	script := []byte("echo hello\n")
	err := client.RunScript(t.Context(), endpoint, script, map[string]string{"Z": "a'b; $x", "A": "a b"}, true, io.Discard, io.Discard)
	if !errors.Is(err, exit) || !strings.Contains(err.Error(), "23") {
		t.Fatalf("exit error = %v", err)
	}
	call := fake.Calls[0]
	want := append([]string{"-F", "none", "-i", "test identity", "-p", "2201", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + os.DevNull, "-o", "LogLevel=ERROR", "-o", "BatchMode=yes", "-o", "ForwardAgent=no",
		"--", "developer@127.0.0.1"}, `'sudo' '-n' 'env' 'A=a b' 'Z=a'"'"'b; $x' 'sh' '-s'`)
	if !slices.Equal(call.Args, want) || call.Stdout != io.Discard || call.Stderr != io.Discard {
		t.Fatalf("ssh invocation = %+v", call)
	}
	data, err := io.ReadAll(call.Stdin)
	if err != nil || string(data) != string(script) {
		t.Fatalf("stdin = %q, %v", data, err)
	}
	if err := client.RunScript(t.Context(), endpoint, nil, map[string]string{"X;": ""}, false, nil, nil); err == nil {
		t.Fatal("accepted an invalid environment name")
	}
}

func TestRunAndInteractive(t *testing.T) {
	fake := &execx.Fake{}
	ep := endpoint
	ep.KnownHosts = "test-known-hosts"
	client := Client{Runner: fake, SSH: os.Args[0]}
	if _, err := client.Run(t.Context(), ep, "printf", "%s", "$(literal)"); err != nil {
		t.Fatal(err)
	}
	args := fake.Calls[0].Args
	if args[len(args)-1] != "'printf' '%s' '$(literal)'" || !slices.Contains(args, "UserKnownHostsFile=test-known-hosts") {
		t.Fatalf("argv = %v", args)
	}
	client.replaceProcess = func(path string, args, _ []string) error {
		want := []string{"-t", "--", "developer@127.0.0.1", "'echo' 'hello world'"}
		if path != os.Args[0] || !slices.Equal(args[len(args)-len(want):], want) {
			t.Fatalf("exec = %q %v", path, args)
		}
		return nil
	}
	if err := client.Interactive(ep, []string{"echo", "hello world"}); err != nil {
		t.Fatal(err)
	}
	ep.Host = "192.0.2.1"
	if _, err := client.Run(t.Context(), ep, "true"); err == nil || len(fake.Calls) != 1 {
		t.Fatalf("remote endpoint accepted: %v", err)
	}
}

func TestAuthorizeKeysScript(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	path := filepath.Join(dir, "authorized_keys")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ssh-ed25519 EXISTING legacy comment"), 0644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		fake := &execx.Fake{}
		client := Client{Runner: fake}
		if err := client.AuthorizeKeys(t.Context(), endpoint, []string{"ssh-ed25519 EXISTING", "ssh-ed25519 NEW comment's literal $value"}); err != nil {
			t.Fatal(err)
		}
		_, err := (execx.OS{}).Run(t.Context(), execx.Cmd{
			Name: "sh", Args: []string{"-s"}, Env: []string{"HOME=" + home},
			Stdin: fake.Calls[0].Stdin, Stderr: os.Stderr,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	want := "ssh-ed25519 EXISTING legacy comment\nssh-ed25519 NEW comment's literal $value\n"
	if err != nil || string(data) != want {
		t.Fatalf("authorized_keys = %q, %v", data, err)
	}
	for path, mode := range map[string]os.FileMode{path: 0600, dir: 0700} {
		if stat, err := os.Stat(path); err != nil || stat.Mode().Perm() != mode {
			t.Fatalf("mode for %s = %v, %v", path, stat, err)
		}
	}
}

func TestConfigGoldenAndLocalHost(t *testing.T) {
	want, err := os.ReadFile("testdata/config.golden")
	if err != nil {
		t.Fatal(err)
	}
	got := RenderConfig([]ConfigEntry{
		{Name: "alpha", Host: "vm-host.example", Port: 2201, User: "developer"},
		{Name: "beta", Host: "192.0.2.1", Port: 2202, User: "builder"},
	})
	if got != string(want) {
		t.Fatalf("config = %q, want %q", got, want)
	}
	fake := &execx.Fake{}
	fake.On("scutil --get LocalHostName", "test-mac\n", nil)
	host, err := (Client{Runner: fake}).LocalHostName(context.Background())
	if err != nil || host != "test-mac.local" {
		t.Fatalf("local host = %q, %v", host, err)
	}
}

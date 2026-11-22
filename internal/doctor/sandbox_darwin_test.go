package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/confine"
)

func TestProbeSandboxRequiresFileDenials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profileDir := t.TempDir()
	stateDir := t.TempDir()
	runDir := t.TempDir()
	for _, rules := range []confine.Rules{
		{Home: home, RuntimeDir: stateDir, LimaHome: stateDir, Executable: "/usr/bin/true"},
		{Daemon: true, RuntimeDir: runDir, Executable: "/usr/bin/true", BindTCP: []confine.Bind{{Port: 23001}}},
	} {
		if _, err := confine.Prepare(rules, profileDir); err != nil {
			t.Fatal(err)
		}
	}

	checks := (Host{}).ProbeSandbox(t.Context(), profileDir)
	if len(checks) != 4 {
		t.Fatalf("expected four checks, got %+v", checks)
	}
	for _, check := range checks {
		if !check.OK {
			t.Errorf("expected denial, got %+v", check)
		}
	}

	daemon := filepath.Join(profileDir, confine.DaemonProfileName)
	if err := os.WriteFile(daemon, []byte("(version 1) (allow default)"), 0600); err != nil {
		t.Fatal(err)
	}
	checks = (Host{}).ProbeSandbox(t.Context(), profileDir)
	for _, check := range checks[2:] {
		if check.OK {
			t.Errorf("permissive profile passed: %+v", check)
		}
	}
	files, err := os.ReadDir(home)
	if err != nil || len(files) != 0 {
		t.Fatalf("canary cleanup left %v, err %v", files, err)
	}
}

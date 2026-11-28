package autostart

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	j := job{root: `/home/owner/scratch %h "test"`, executable: `/home/owner/box $test`, path: "/usr/local/bin:/usr/bin"}
	for _, want := range []string{
		"Wants=network-online.target\nAfter=network-online.target\n",
		"Type=oneshot\nRemainAfterExit=yes\nKillMode=process\n",
		`Environment="HOMEBOX_HOME=/home/owner/scratch %%h \"test\""` + "\n",
		`Environment="PATH=/usr/local/bin:/usr/bin"` + "\n",
		`ExecStart=":/home/owner/box $test" autostart run` + "\n",
		"WantedBy=default.target\n",
	} {
		if !strings.Contains(j.unitFile(), want) {
			t.Errorf("unit is missing %q", want)
		}
	}
}

func TestLaunchDaemonPlist(t *testing.T) {
	j := job{
		owner: "account", root: "/Users/account/scratch <&>",
		home: "/Users/account", executable: "/Users/account/bin & tools/box",
		path: "/opt/homebrew/bin:/usr/bin",
	}
	plist := j.plist("com.homebox.autostart.501", false)
	decoder := xml.NewDecoder(strings.NewReader(plist))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok && (start.Name.Local == "key" || start.Name.Local == "string") {
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	if got, want := strings.Join(values, "|"), strings.Join([]string{
		"Label", "com.homebox.autostart.501", "UserName", j.owner, "ProgramArguments", j.executable, "autostart", "run",
		"EnvironmentVariables", "HOMEBOX_HOME", j.root, "HOME", j.home, "PATH", j.path, "RunAtLoad", "AbandonProcessGroup",
	}, "|"); got != want {
		t.Errorf("plist values = %q, want %q", got, want)
	}
	if strings.Count(plist, "<true/>") != 2 {
		t.Error("expected both launchd options to be enabled")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/path/to/owner's box"); got != `'/path/to/owner'\''s box'` {
		t.Fatalf("shell quote = %s", got)
	}
}

func TestLaunchAgentUsesLoginAccount(t *testing.T) {
	j := job{owner: "account", root: "/Users/account/.homebox", executable: "/usr/local/bin/box"}
	if plist := j.plist("com.homebox.autostart.501", true); strings.Contains(plist, "<key>UserName</key>") ||
		!strings.Contains(plist, "<key>RunAtLoad</key><true/>") {
		t.Fatalf("unexpected LaunchAgent plist: %s", plist)
	}
}

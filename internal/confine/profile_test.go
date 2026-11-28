package confine

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden profiles")

func TestRenderHostAgent(t *testing.T) {
	assertProfile(t, "hostagent", Rules{
		Home: "/Users/fixture", LimaHome: "/state/lima", Executable: "/opt/tools/limactl",
		RuntimeDir:      `/tmp/instance "quoted"\path`,
		executableAlias: "/opt/bin/limactl",
		GatewaySocket:   "/state/boxes/fixture/run/net.sock",
		ConnectTCP:      []int{32201},
		BindTCP:         []Bind{{Port: 32201}, {Port: 10100, LAN: true}},
	})
}

func TestRenderDaemon(t *testing.T) {
	assertProfile(t, "vmnet", Rules{
		Daemon: true, RuntimeDir: "/state/boxes/fixture/run", Executable: "/opt/tools/box",
		GatewaySocket: "/state/boxes/fixture/run/net.sock",
		HostPIDFile:   "/state/lima/fixture/ha.pid",
		BindTCP:       []Bind{{Port: 2201, LAN: true}},
	})
}

func assertProfile(t *testing.T, name string, rules Rules) {
	t.Helper()
	profile, err := render(rules)
	if err != nil {
		t.Fatal(err)
	}
	file := "testdata/" + name + ".golden"
	if *update {
		if err := os.WriteFile(file, []byte(profile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if profile != string(golden) {
		t.Fatalf("profile differs from golden:\n%s", profile)
	}
}

func TestInvalidRules(t *testing.T) {
	dir := "/tmp/instance"
	for _, rules := range []Rules{
		{RuntimeDir: "relative"},
		{RuntimeDir: dir + "\n"},
		{RuntimeDir: dir, ConnectTCP: []int{0}},
		{RuntimeDir: dir, ConnectUDP: []int{-1}},
		{RuntimeDir: dir, BindTCP: []Bind{{Port: 65536}}},
	} {
		if _, err := render(rules); err == nil {
			t.Errorf("accepted invalid rules: %+v", rules)
		}
	}
}

func TestDarwinGuestAgentReadScope(t *testing.T) {
	rules := Rules{
		Home: "/Users/fixture", LimaHome: "/state/lima", RuntimeDir: "/state/lima/mac",
		Executable: "/tools/lima/bin/limactl", executableAlias: "/opt/bin/limactl",
	}
	ubuntu, err := render(rules)
	if err != nil {
		t.Fatal(err)
	}
	rules.DarwinGuest = true
	mac, err := render(rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/tools/lima/share/lima", "/opt/share/lima"} {
		if spec := `(literal "` + dir + `/lima-guestagent.Darwin-aarch64.gz")`; strings.Contains(ubuntu, spec) || !strings.Contains(mac, spec) {
			t.Errorf("Darwin agent access is not restricted to macOS guests: %s", spec)
		}
		if strings.Contains(mac, `(subpath "`+dir+`")`) {
			t.Errorf("recursive read access to %s", dir)
		}
	}
}

package confine

import (
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden profiles")

func TestRenderHostAgent(t *testing.T) {
	assertProfile(t, "hostagent", Rules{
		Home: "/Users/fixture", LimaHome: "/state/lima", Executable: "/opt/tools/limactl",
		RuntimeDir: `/tmp/instance "quoted"\path`,
		ConnectTCP: []int{32201, 23001, 23101},
		ConnectUDP: []int{23101},
		BindTCP:    []Bind{{Port: 32201}, {Port: 10100, LAN: true}},
	})
}

func TestRenderDaemon(t *testing.T) {
	assertProfile(t, "vmnet", Rules{
		Daemon: true, RuntimeDir: "/state/boxes/fixture/run", Executable: "/opt/tools/box",
		BindTCP: []Bind{{Port: 23001}, {Port: 23101}, {Port: 2201, LAN: true}},
		BindUDP: []Bind{{Port: 23101}},
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

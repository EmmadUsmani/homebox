package isolation

import (
	"reflect"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/confine"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

func TestRulesAllowOnlyTheVMsOwnPorts(t *testing.T) {
	box := state.Box{Name: "main", Index: 3, Forwards: []vm.PortForward{
		{GuestPort: 8000, HostPort: 10300, HostIP: "127.0.0.1"},
		{GuestPort: 8001, HostPort: 10301, HostIP: "0.0.0.0"},
	}}
	got := rules(box, vm.Instance{RuntimeDir: "/lima/main"})
	want := confine.Rules{
		RuntimeDir: "/lima/main",
		ConnectTCP: []int{32203, 23003},
		ConnectUDP: []int{23103},
		BindTCP:    []confine.Bind{{Port: 32203}, {Port: 10300}, {Port: 10301, LAN: true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %+v, want %+v", got, want)
	}
}

func TestDaemonConfigPublishesSSHOnTheLANOnlyWhenAsked(t *testing.T) {
	host := &Host{Store: &state.Store{Root: "/state"}}
	box := state.Box{Name: "main", Index: 1, SSHPort: 2201}
	for _, lan := range []bool{false, true} {
		config := host.daemonConfig(box, lan, "/state/boxes/main/run")
		wantIP := map[bool]string{false: "127.0.0.1", true: "0.0.0.0"}[lan]
		if config.SSHPublish.IP != wantIP || config.SSHPublish.Port != 2201 || config.SSHTarget != 32201 ||
			config.ProxyPort != 23001 || config.DNSPort != 23101 {
			t.Fatalf("lan=%v: config = %+v", lan, config)
		}
	}
}

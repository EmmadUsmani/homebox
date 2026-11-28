package confine

import (
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"testing"
)

func TestQEMULaunchOnlyAcceptsHomeboxNetwork(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "gateway")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Net: "unix", Name: socket})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fd, err := conn.File()
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	const port = 32284
	args := []string{"-netdev", qemuUserNetdev(port), "-netdev", fmt.Sprintf("socket,id=net1,fd=%d", fd.Fd())}
	rules := Rules{ConnectTCP: []int{port}, GatewaySocket: socket}
	valid := slices.Clone(args)
	if err := restrictQEMU(valid, rules); err != nil {
		t.Fatal(err)
	}
	if valid[1] != RestrictedQEMUSLIRP(port) || valid[3] != args[3] {
		t.Fatalf("unexpected QEMU arguments %q", valid)
	}
	for name, invalid := range map[string][]string{
		"missing gateway":    args[:2],
		"unapproved forward": {"-netdev", qemuUserNetdev(port + 1), args[2], args[3]},
		"extra backend":      append(slices.Clone(args), "-netdev", "user,id=outside"),
		"legacy backend":     append(slices.Clone(args), "-nic", "user"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := restrictQEMU(slices.Clone(invalid), rules); err == nil {
				t.Fatalf("accepted QEMU arguments %q", invalid)
			}
		})
	}
	rules.GatewaySocket += ".wrong"
	if err := restrictQEMU(slices.Clone(args), rules); err == nil {
		t.Fatal("accepted a descriptor connected to the wrong socket")
	}
}

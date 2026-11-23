//go:build darwin || linux

package vmnet

import (
	"net"
	"net/netip"
	"os"
	"testing"
)

func TestListenerOwnership(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	target := listener.Addr().(*net.TCPAddr).AddrPort()
	process := hostProcess{pid: os.Getpid(), start: processStart(os.Getpid())}
	originalID := process.listenerID(target)
	if originalID == 0 {
		t.Fatal("could not verify current process owns listener")
	}
	reused := hostProcess{pid: process.pid, start: process.start + 1}
	if reused.listenerID(target) != 0 {
		t.Fatal("accepted a reused PID")
	}
	listener.Close()
	if process.listenerID(target) != 0 {
		t.Fatal("verified a closed listener")
	}
	replacement, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(target))
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if id := process.listenerID(target); id == 0 || id == originalID {
		t.Fatalf("replacement listener ID = %d, original = %d", id, originalID)
	}
}

func BenchmarkListenerOwnership(b *testing.B) {
	listener, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()
	target := listener.Addr().(*net.TCPAddr).AddrPort()
	process := hostProcess{pid: os.Getpid(), start: processStart(os.Getpid())}
	b.ResetTimer()
	for range b.N {
		if process.listenerID(target) == 0 {
			b.Fatal("could not verify listener owner")
		}
	}
}

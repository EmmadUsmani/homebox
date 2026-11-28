package vmnet

import (
	"context"
	"errors"
	"net"
)

func (hostProcess) unixDial(string) func(context.Context) (net.Conn, error) {
	return func(context.Context) (net.Conn, error) {
		return nil, errors.New("Unix SSH target is unavailable on macOS")
	}
}

func resolveLaunchProcess(string) (hostProcess, error) {
	return hostProcess{}, errors.New("QEMU launch identity is unavailable on macOS")
}

func (hostProcess) matchesPIDFD() bool { return false }

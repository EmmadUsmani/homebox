package sshx

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

const testHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestReadHostKeys(t *testing.T) {
	ep := endpoint
	fake := &execx.Fake{}
	client := Client{Runner: fake}
	if _, err := client.HostKeys(t.Context(), ep); err == nil || len(fake.Calls) != 0 {
		t.Fatalf("unpinned endpoint accepted: %v", err)
	}
	ep.KnownHosts = "/test/driver-pins"
	fake.On("ssh ", testHostKey+" comment\n"+testHostKey+" duplicate\n", nil)
	keys, err := client.HostKeys(t.Context(), ep)
	if err != nil || !slices.Equal(keys, []string{testHostKey}) {
		t.Fatalf("host keys = %q, %v", keys, err)
	}
	args := fake.Calls[0].Args
	if !slices.Contains(args, "StrictHostKeyChecking=yes") ||
		!slices.Contains(args, `UserKnownHostsFile="/test/driver-pins"`) ||
		!strings.Contains(args[len(args)-1], "/etc/ssh/ssh_host_*_key.pub") {
		t.Fatalf("host key collection argv = %q", args)
	}
	for _, output := range []string{"", "ssh-ed25519 invalid", testHostKey + "\n@cert-authority * " + testHostKey} {
		fake.Responses = nil
		fake.On("ssh ", output, nil)
		if _, err := client.HostKeys(t.Context(), ep); err == nil {
			t.Fatalf("accepted invalid key output %q", output)
		}
	}
	failure := errors.New("host key verification failed")
	fake.Responses = nil
	fake.On("ssh ", testHostKey, failure)
	if _, err := client.HostKeys(t.Context(), ep); !errors.Is(err, failure) {
		t.Fatalf("SSH failure = %v", err)
	}
}

package doctor

import "github.com/EmmadUsmani/homebox/internal/vmnet"

// ProbeSSHRelay checks whether the live VM host process owns the SSH target.
func (h Host) ProbeSSHRelay(pidFile string, port int) Check {
	return Check{
		Name:   "ssh-relay-target-owner",
		OK:     vmnet.OwnsSSHTarget(pidFile, port),
		Detail: "could not verify the live VM host process owns the driver SSH listener",
	}
}

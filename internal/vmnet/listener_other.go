//go:build !darwin && !linux

package vmnet

func OwnsSSHTarget(pidFile string, port int) bool { return false }

// Package guest loads guest profiles: which image a VM boots and the scripts
// that provision it.
//
// A profile is a folder under the repo's guest/ directory (embedded into the
// box binary). Its profile.yaml names the images and an ordered list of
// provisioning steps; steps may use scripts from guest/common, which hold the
// distro-independent tool layer. Scripts are POSIX sh. How a profile installs
// packages or runs services is its own business; box only runs the steps in
// order over SSH and checks each exit code.
package guest

import "github.com/EmmadUsmani/homebox/internal/vm"

// Step is one provisioning script, run in the guest over SSH.
type Step struct {
	Name   string // short label shown while provisioning, e.g. "base packages"
	Script string // path inside the guest tree, e.g. "ubuntu/base.sh"
	Root   bool   // run with sudo
}

// Profile is a guest distro plus how to provision it.
type Profile struct {
	Name        string
	Description string
	OS          string
	BootScript  string `yaml:"bootScript"`
	Images      []vm.Image
	Steps       []Step
	ImageUpdate []Step `yaml:"imageUpdate"`
}

// DefaultProfile is used when `box new` gets no --profile.
const DefaultProfile = "ubuntu"

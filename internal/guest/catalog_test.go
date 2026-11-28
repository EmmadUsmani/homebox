package guest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	guestfs "github.com/EmmadUsmani/homebox/guest"
	"github.com/EmmadUsmani/homebox/internal/guest"
)

const alpineManifest = `name: alpine
description: OpenRC guest
images:
  - arch: x86_64
    url: https://example.com/alpine.img
steps:
  - name: Base packages
    script: alpine/base.sh
    root: true
  - name: Shared tools
    script: common/tools.sh
    root: false
`

func TestCatalog(t *testing.T) {
	const script = "set -eu\n"
	catalog, err := guest.NewCatalog(fstest.MapFS{
		"z/profile.yaml":      {Data: []byte(strings.Replace(alpineManifest, "name: alpine", "name: z", 1))},
		"alpine/profile.yaml": {Data: []byte(alpineManifest)},
		"alpine/base.sh":      {Data: []byte(script)},
		"common/tools.sh":     {Data: []byte(script)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Names(); !slices.Equal(got, []string{"alpine", "z"}) {
		t.Fatalf("Names() = %v", got)
	}
	profile, err := catalog.Get("alpine")
	if err != nil {
		t.Fatal(err)
	}
	wantSteps := []guest.Step{
		{Name: "Base packages", Script: "alpine/base.sh", Root: true},
		{Name: "Shared tools", Script: "common/tools.sh"},
	}
	if !slices.Equal(profile.Steps, wantSteps) || profile.Images[0].Arch != "x86_64" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	profile.Steps[0].Name = "changed"
	profile.Images[0].Arch = "changed"
	again, err := catalog.Get("alpine")
	if err != nil || again.Steps[0] != wantSteps[0] || again.Images[0].Arch != "x86_64" {
		t.Fatalf("Get exposed catalog state: %+v, %v", again, err)
	}
	if _, err := catalog.Get("missing"); err == nil {
		t.Fatal("Get accepted an unknown profile")
	}
	if got, err := catalog.ReadScript("common/tools.sh"); err != nil || string(got) != script {
		t.Fatalf("ReadScript() = %q, %v", got, err)
	}
}

func TestCatalogRejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		error    string
	}{
		{"missing name", strings.Replace(alpineManifest, "name: alpine", "name: ''", 1), "missing profile name"},
		{"missing images", "name: alpine\n", "missing images"},
		{"missing architecture", strings.Replace(alpineManifest, "x86_64", "''", 1), "arch and URL"},
		{"missing script", strings.Replace(alpineManifest, "common/tools.sh", "common/missing.sh", 1), "common/missing.sh"},
		{"invalid path", strings.Replace(alpineManifest, "common/tools.sh", "../tools.sh", 1), "invalid guest script path"},
		{"unknown field", alpineManifest + "unexpected: true\n", "field unexpected"},
		{"extra document", alpineManifest + "---\nname: extra\n", "single YAML document"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := guest.NewCatalog(fstest.MapFS{
				"alpine/profile.yaml": {Data: []byte(test.manifest)},
				"alpine/base.sh":      {},
				"common/tools.sh":     {},
			})
			if err == nil || !strings.Contains(err.Error(), test.error) {
				t.Fatalf("NewCatalog() error = %v, want %q", err, test.error)
			}
		})
	}
}

func TestEmbeddedProfiles(t *testing.T) {
	catalog, err := guest.NewCatalog(guestfs.FS)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Names(); !slices.Equal(got, []string{"macos", guest.DefaultProfile}) {
		t.Fatalf("embedded names = %v", got)
	}
	profile, err := catalog.Get(guest.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	var archs []string
	for _, image := range profile.Images {
		digest, ok := strings.CutPrefix(image.Digest, "sha256:")
		decoded, err := hex.DecodeString(digest)
		if !ok || err != nil || len(decoded) != sha256.Size {
			t.Fatalf("invalid digest for %s: %q", image.Arch, image.Digest)
		}
		archs = append(archs, image.Arch)
	}
	slices.Sort(archs)
	if !slices.Equal(archs, []string{"aarch64", "x86_64"}) {
		t.Fatalf("embedded image architectures = %v", archs)
	}
	for _, step := range profile.Steps {
		script, err := catalog.ReadScript(step.Script)
		if err != nil || !strings.HasPrefix(string(script), "#!/bin/sh\nset -eu\n") {
			t.Fatalf("%s: expected an embedded POSIX shell script, read error: %v", step.Script, err)
		}
	}
}

func TestEmbeddedDoctorScript(t *testing.T) {
	catalog, err := guest.NewCatalog(guestfs.FS)
	if err != nil {
		t.Fatal(err)
	}
	const path = "common/doctor.sh"
	script, err := catalog.ReadScript(path)
	if err != nil || !strings.HasPrefix(string(script), "#!/bin/sh\nset -eu\n") {
		t.Fatalf("%s: expected an embedded POSIX shell script, read error: %v", path, err)
	}
	profile, err := catalog.Get(guest.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range profile.Steps {
		if step.Script == path {
			t.Fatalf("%s must run separately from provisioning", path)
		}
	}
}

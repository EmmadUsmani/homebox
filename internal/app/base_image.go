package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/images"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

const baseImageDiskGiB = 20

type baseImageDriver interface {
	ImageBackend() string
	SaveDisk(context.Context, string, string) ([]byte, error)
}

var baseImageChecks = []struct {
	path string
	root bool
}{
	{"lib/prepare-guest.sh", false},
	{"lib/scan-secrets.sh", true},
}

func (s *Service) baseDigest(profile guest.Profile) (string, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode(profile); err != nil {
		return "", err
	}
	for _, step := range profile.Steps {
		data, err := s.Profiles.ReadScript(step.Script)
		if err != nil {
			return "", err
		}
		if err := encoder.Encode(data); err != nil {
			return "", err
		}
	}
	for _, check := range baseImageChecks {
		data, err := images.Scripts.ReadFile(check.path)
		if err != nil {
			return "", err
		}
		if err := encoder.Encode(data); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// RebuildImage verifies and publishes a fresh signed-out profile base.
func (s *Service) RebuildImage(ctx context.Context, profileName, version string) error {
	driver, ok := s.Driver.(baseImageDriver)
	if !ok || driver.ImageBackend() == "" {
		return fmt.Errorf("driver %q does not support rebuilding base images on this host", s.Driver.Name())
	}
	options := NewOptions{Profile: profileName, Version: version}
	if err := s.resolveNewOptions(&options); err != nil {
		return err
	}
	profile, err := s.Profiles.Get(options.Profile)
	if err != nil {
		return err
	}
	if profile.OS == vm.DarwinGuest {
		return errors.New("rebuilding macOS guest base images is unavailable; use box new --profile macos to install a guest")
	}
	if len(profile.ImageUpdate) == 0 {
		return fmt.Errorf("profile %q does not define base image updates", profile.Name)
	}
	user, err := defaultUser()
	if err != nil {
		return err
	}
	options, err = s.baseBuildOptions(options)
	if err != nil {
		return err
	}
	return s.Store.WithLock(func() error {
		return s.rebuildBase(ctx, driver, profile, options, user)
	})
}

func (s *Service) profileBase(ctx context.Context, options NewOptions, profile guest.Profile, user string) (string, error) {
	driver, ok := s.Driver.(baseImageDriver)
	if !ok || driver.ImageBackend() == "" || profile.OS == vm.DarwinGuest || len(profile.ImageUpdate) == 0 || options.DiskGiB < baseImageDiskGiB {
		return "", nil
	}
	name := "base-" + profile.Name
	image, _, err := s.Store.LoadImage(name)
	switch {
	case err == nil:
		if image.BaseDigest == "" || image.Profile != profile.Name || options.DiskGiB < image.DiskGiB {
			return "", nil
		}
		digest, err := s.baseDigest(profile)
		if err != nil {
			return "", err
		}
		if image.BaseDigest == digest && image.BaseUser == user && image.BaseOS == runtime.GOOS && s.checkImageCompatibility(image) == nil {
			s.progress("Using profile base image %s", name)
			return name, nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	}
	options, err = s.baseBuildOptions(options)
	if err != nil {
		return "", err
	}
	if err := s.rebuildBase(ctx, driver, profile, options, user); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Service) baseBuildOptions(options NewOptions) (NewOptions, error) {
	options.Name = "b" + strings.ToLower(rand.Text()[:4])
	options.Image, options.ForceProvision = "", true
	options.NestedVirt = false
	options.CPUs, options.MemoryGiB, options.DiskGiB = min(options.CPUs, 4), min(options.MemoryGiB, 6), baseImageDiskGiB
	return options, s.validateSocketPaths(options.Name)
}

func (s *Service) rebuildBase(ctx context.Context, driver baseImageDriver, profile guest.Profile, options NewOptions, user string) (err error) {
	name := "base-" + profile.Name
	if err := state.ValidateName(name); err != nil {
		return err
	}
	old, _, err := s.Store.LoadImage(name)
	if err == nil && (old.BaseDigest == "" || old.Profile != profile.Name) {
		return fmt.Errorf("image %q already exists and is not a managed base for %q", name, profile.Name)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	digest, err := s.baseDigest(profile)
	if err != nil {
		return err
	}
	s.progress("Building profile base image %s", name)
	buildErr := s.newBox(ctx, options, profile, user, true)
	var provisionErr *ProvisionError
	if buildErr == nil || errors.As(buildErr, &provisionErr) {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			defer cancel()
			removeErr := s.remove(cleanup, options.Name)
			if removeErr == nil {
				removeErr = os.RemoveAll(s.Store.BoxDir(options.Name))
			}
			err = errors.Join(err, removeErr)
		}()
	}
	if buildErr != nil {
		return fmt.Errorf("build %s: %w", name, buildErr)
	}
	box, err := s.Store.Load(options.Name)
	if err != nil {
		return err
	}
	if err := s.doctorBox(ctx, box); err != nil {
		return err
	}
	endpoint, err := s.Driver.SSH(ctx, box.Name)
	if err != nil {
		return err
	}
	output := func(line string, truncated bool) error {
		line = untrusted.Line(line)
		if truncated {
			line += " [line truncated]"
		}
		s.progress("%s", line)
		return nil
	}
	run := func(script []byte, root bool) error {
		stdout, stderr := &logFramer{emit: output}, &logFramer{emit: output}
		runErr := s.GuestSSH.RunScript(ctx, endpoint, script, nil, root, stdout, stderr)
		for _, stream := range []*logFramer{stdout, stderr} {
			if len(stream.pending) > 0 {
				runErr = errors.Join(runErr, stream.finish())
			}
		}
		return runErr
	}
	for _, check := range baseImageChecks {
		data, err := images.Scripts.ReadFile(check.path)
		if err != nil {
			return err
		}
		script := fmt.Appendf(nil, "exec bash -s <<'HOMEBOX_IMAGE_CHECK'\n%s\nHOMEBOX_IMAGE_CHECK\n", data)
		if err := run(script, check.root); err != nil {
			return fmt.Errorf("image check %s: %w", check.path, err)
		}
	}
	if err := run([]byte("fstrim -av"), true); err != nil {
		return err
	}
	if _, err := s.stopForProvision(ctx, box.Name); err != nil {
		return err
	}
	if err := s.Store.SaveBaseImage(ctx, box, name, options.Version, driver.ImageBackend(), digest, func(path string) ([]byte, error) {
		return driver.SaveDisk(ctx, box.Name, path)
	}); err != nil {
		return err
	}
	s.progress("Saved profile base image %s", name)
	return nil
}

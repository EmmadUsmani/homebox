package guest

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/vm"
	"go.yaml.in/yaml/v3"
)

// Catalog loads profiles and their provisioning scripts from a guest tree.
type Catalog struct {
	fsys     fs.FS
	profiles map[string]Profile
}

// NewCatalog reads and validates every profile manifest in fsys.
func NewCatalog(fsys fs.FS) (*Catalog, error) {
	paths, err := fs.Glob(fsys, "*/profile.yaml")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no guest profiles found")
	}
	catalog := &Catalog{fsys: fsys, profiles: make(map[string]Profile)}
	for _, path := range paths {
		profile, err := catalog.loadProfile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if _, exists := catalog.profiles[profile.Name]; exists {
			return nil, fmt.Errorf("%s: duplicate guest profile %q", path, profile.Name)
		}
		catalog.profiles[profile.Name] = profile
	}
	return catalog, nil
}

func (c *Catalog) loadProfile(path string) (Profile, error) {
	var profile Profile
	data, err := fs.ReadFile(c.fsys, path)
	if err != nil {
		return profile, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&profile); err != nil {
		return profile, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return profile, fmt.Errorf("expected a single YAML document")
	}
	if strings.TrimSpace(profile.Name) == "" {
		return profile, fmt.Errorf("missing profile name")
	}
	if len(profile.Images) == 0 {
		return profile, fmt.Errorf("missing images")
	}
	if profile.OS != "" && profile.OS != vm.DarwinGuest {
		return profile, fmt.Errorf("unsupported guest OS %q", profile.OS)
	}
	if profile.BootScript != "" {
		if _, err := c.ReadScript(profile.BootScript); err != nil {
			return profile, fmt.Errorf("boot script: %w", err)
		}
	}
	for _, image := range profile.Images {
		if strings.TrimSpace(image.Arch) == "" || strings.TrimSpace(image.URL) == "" {
			return profile, fmt.Errorf("each image requires an arch and URL")
		}
	}
	for _, step := range slices.Concat(profile.Steps, profile.ImageUpdate) {
		if strings.TrimSpace(step.Name) == "" {
			return profile, fmt.Errorf("missing step name")
		}
		if _, err := c.ReadScript(step.Script); err != nil {
			return profile, fmt.Errorf("step %q: %w", step.Name, err)
		}
	}
	return profile, nil
}

// Get returns a copy of the named profile.
func (c *Catalog) Get(name string) (Profile, error) {
	profile, ok := c.profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown guest profile %q", name)
	}
	profile.Images = slices.Clone(profile.Images)
	profile.Steps = slices.Clone(profile.Steps)
	profile.ImageUpdate = slices.Clone(profile.ImageUpdate)
	return profile, nil
}

// Names returns the available profile names in lexical order.
func (c *Catalog) Names() []string {
	return slices.Sorted(maps.Keys(c.profiles))
}

// ReadScript reads a script by its path relative to the guest tree.
func (c *Catalog) ReadScript(path string) ([]byte, error) {
	if !fs.ValidPath(path) || path == "." || strings.Contains(path, `\`) {
		return nil, fmt.Errorf("invalid guest script path %q", path)
	}
	return fs.ReadFile(c.fsys, path)
}

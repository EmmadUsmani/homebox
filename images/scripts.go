// Package images embeds the shared image preparation and credential checks.
package images

import "embed"

// Scripts contains the guest-side image build scripts.
//
//go:embed lib/*.sh
var Scripts embed.FS

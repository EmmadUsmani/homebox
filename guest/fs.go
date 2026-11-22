// Package guestfs embeds the bundled guest profiles and their scripts.
package guestfs

import "embed"

// FS contains profiles and scripts with paths relative to guest/.
//
//go:embed */profile.yaml */*.sh
var FS embed.FS

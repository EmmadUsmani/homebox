//go:build !darwin

package doctor

import "context"

// ProbeSandbox reports that Seatbelt is unavailable on this platform.
func (Host) ProbeSandbox(context.Context, string) []Check {
	return []Check{{Name: "sandbox-canary", Skipped: true, Detail: "Seatbelt is available on macOS only"}}
}

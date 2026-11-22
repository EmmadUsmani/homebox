// Package doctor runs host-side controls for VM isolation checks.
package doctor

import (
	"fmt"
	"io"
)

// Check is the result of a single diagnostic.
type Check struct {
	Name    string
	OK      bool
	Skipped bool
	Detail  string
}

// Write prints the checks and reports whether every required check passed.
func Write(out io.Writer, checks []Check) bool {
	ok := true
	for _, check := range checks {
		switch {
		case check.Skipped:
			fmt.Fprintf(out, "SKIP %s: %s\n", check.Name, check.Detail)
		case check.OK:
			fmt.Fprintf(out, "PASS %s\n", check.Name)
		default:
			fmt.Fprintf(out, "FAIL %s: %s\n", check.Name, check.Detail)
			ok = false
		}
	}
	return ok
}

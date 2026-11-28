package doctor

import (
	"context"
	"maps"
	"slices"

	"github.com/EmmadUsmani/homebox/internal/confine"
)

func (Host) ProbeSandbox(ctx context.Context, dir string) []Check {
	results := confine.ProbeLinux(ctx, dir)
	checks := make([]Check, 0, len(results))
	for _, name := range slices.Sorted(maps.Keys(results)) {
		err := results[name]
		check := Check{Name: "sandbox-" + name, OK: err == nil}
		if err != nil {
			check.Detail = err.Error()
		}
		checks = append(checks, check)
	}
	return checks
}

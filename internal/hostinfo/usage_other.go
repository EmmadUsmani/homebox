//go:build !darwin && !linux

package hostinfo

import (
	"context"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

func (ProcessUsage) Sample(context.Context, []vm.Instance) map[string]UsageSample { return nil }

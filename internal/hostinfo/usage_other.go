//go:build !darwin

package hostinfo

import "context"

func (ProcessUsage) Sample(context.Context, string) *Usage { return nil }

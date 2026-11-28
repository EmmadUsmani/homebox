//go:build !linux

package wiring

import "context"

var checkHost func(context.Context) error

//go:build darwin || linux

package vmnet

import (
	"context"
	"log/slog"
	"testing"

	"golang.org/x/time/rate"
)

func TestFlowAdmissionBounds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	active := newFlows(ctx, nil, slog.Default())
	active.attempts = rate.NewLimiter(0, 64)
	for i := 0; i < 64; i++ {
		if !active.reserve() {
			t.Fatalf("flow %d blocked within initial rate burst", i)
		}
	}
	if active.reserve() {
		t.Fatal("flow admission exceeded the rate burst")
	}

	active.attempts = rate.NewLimiter(rate.Inf, 0)
	for i := 64; i < maxFlows; i++ {
		if !active.reserve() {
			t.Fatalf("flow %d blocked below the global cap", i)
		}
	}
	if active.reserve() {
		t.Fatal("flow admission exceeded the global cap")
	}
	active.release()
	if !active.reserve() {
		t.Fatal("released flow slot was not available")
	}
	cancel()
	for i := 0; i < maxFlows; i++ {
		active.release()
	}
	active.wait()
	if active.reserve() {
		t.Fatal("flow admitted after shutdown")
	}
}

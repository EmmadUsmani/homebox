//go:build darwin || linux

package vmnet

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestFlowAdmissionBounds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var log bytes.Buffer
	active := newFlows(ctx, nil, slog.New(slog.NewTextHandler(&log, nil)))
	if active.attempts.Limit() != newFlowsPerSecond || active.attempts.Burst() != newFlowBurst {
		t.Fatal("unexpected production flow rate")
	}
	const burst = newFlowBurst / 2
	active.attempts = rate.NewLimiter(0, burst)
	active.refusals = rate.NewLimiter(0, 1)
	for i := 0; i < burst; i++ {
		if !active.reserve() {
			t.Fatalf("flow %d blocked within initial rate burst", i)
		}
	}
	if active.reserve() {
		t.Fatal("flow admission exceeded the rate burst")
	}

	if active.reserve() || strings.Count(log.String(), "reason=rate") != 1 {
		t.Fatalf("expected one rate refusal log, got %q", log.String())
	}

	log.Reset()
	active.refusals = rate.NewLimiter(0, 1)
	active.attempts = rate.NewLimiter(rate.Inf, 0)
	for i := burst; i < maxFlows; i++ {
		if !active.reserve() {
			t.Fatalf("flow %d blocked below the global cap", i)
		}
	}
	if active.reserve() {
		t.Fatal("flow admission exceeded the global cap")
	}
	if active.reserve() || strings.Count(log.String(), "reason=capacity") != 1 {
		t.Fatalf("expected one capacity refusal log, got %q", log.String())
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

func TestUDPIdleTimeout(t *testing.T) {
	if got := udpIdleTimeout(53); got != 2*time.Second {
		t.Errorf("DNS idle timeout = %s", got)
	}
	if got := udpIdleTimeout(123); got != 30*time.Second {
		t.Errorf("NTP idle timeout = %s", got)
	}
}

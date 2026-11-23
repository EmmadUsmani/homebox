package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/state"
)

type fakeLocal struct {
	localBox
	logs func(context.Context, string, app.LogOptions, func(app.LogLine) error) error
}

func (f fakeLocal) Status(_ context.Context, name string) (app.VMStatus, error) {
	return app.VMStatus{Name: name, State: "running"}, nil
}

func (f fakeLocal) Logs(ctx context.Context, name string, options app.LogOptions, emit func(app.LogLine) error) error {
	return f.logs(ctx, name, options, emit)
}

func TestListStatusExplainsMissingLima(t *testing.T) {
	local := Local{findTool: func(string) (string, error) { return "", errors.New("not installed") }}
	if _, err := local.ListStatus(t.Context()); err == nil || !strings.Contains(err.Error(), "Install Lima") {
		t.Fatalf("ListStatus error = %v", err)
	}
}

func TestStatusAddsConfiguredCPUCount(t *testing.T) {
	local := Local{box: fakeLocal{}, load: func(name string) (state.Box, error) {
		return state.Box{Name: name, CPUs: 4}, nil
	}}
	status, err := local.Status(t.Context(), "sample")
	if err != nil || status.Name != "sample" || status.State != "running" || status.CPUs != 4 {
		t.Fatalf("Status = %+v, %v", status, err)
	}
}

func TestStreamLogsCancelsWhenCallOrAppEnds(t *testing.T) {
	for _, ends := range []string{"call", "application"} {
		t.Run(ends, func(t *testing.T) {
			ctx, cancelCall := context.WithCancel(t.Context())
			defer cancelCall()
			lifetime, cancelApp := context.WithCancel(t.Context())
			defer cancelApp()
			events := make(chan logBatch, 1)
			local := Local{
				lifetime: lifetime,
				box: fakeLocal{logs: func(ctx context.Context, name string, options app.LogOptions, emit func(app.LogLine) error) error {
					if name != "sample" || options != (app.LogOptions{Source: "guest", Unit: "ssh.service", Follow: true, Lines: 100}) {
						t.Errorf("unexpected log request: %q, %+v", name, options)
					}
					if err := emit(app.LogLine{Text: "ready"}); err != nil {
						return err
					}
					<-ctx.Done()
					return ctx.Err()
				}},
				emit: func(name string, data ...any) bool {
					if name != "local:logs" {
						t.Errorf("unexpected event %q", name)
					}
					events <- data[0].(logBatch)
					return false
				},
			}
			done := make(chan error, 1)
			go func() { done <- local.StreamLogs(ctx, "test", "sample", "guest", "ssh.service", true) }()
			select {
			case batch := <-events:
				if batch.ID != "test" || len(batch.Lines) != 1 || batch.Lines[0].Text != "ready" {
					t.Fatalf("unexpected log batch: %+v", batch)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no log batch emitted")
			}
			if ends == "call" {
				cancelCall()
			} else {
				cancelApp()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("stream error = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stream remained open")
			}
		})
	}
}

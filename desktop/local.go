package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type localBox interface {
	ListStatus(context.Context) ([]app.VMStatus, error)
	Status(context.Context, string) (app.VMStatus, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Doctor(context.Context, string) error
	Open(context.Context, string, string, bool) (string, error)
	Logs(context.Context, string, app.LogOptions, func(app.LogLine) error) error
}

type Local struct {
	box      localBox
	load     func(string) (state.Box, error)
	findTool func(string) (string, error)
	emit     func(string, ...any) bool
	lifetime context.Context
}

type localStatus struct {
	app.VMStatus
	CPUs      int `json:"cpus"`
	MemoryGiB int `json:"memory_gib"`
}

func (l *Local) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	l.lifetime = ctx
	return nil
}

func (l *Local) ListStatus(ctx context.Context) ([]app.VMStatus, error) {
	if _, err := l.findTool("limactl"); err != nil {
		return nil, errors.New("Lima (limactl) could not be found. Install Lima in /opt/homebrew/bin or /usr/local/bin to see local VMs")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return l.box.ListStatus(ctx)
}

func (l *Local) Status(ctx context.Context, name string) (localStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := l.box.Status(ctx, name)
	if err != nil {
		return localStatus{}, err
	}
	box, err := l.load(name)
	return localStatus{VMStatus: status, CPUs: box.CPUs, MemoryGiB: box.MemoryGiB}, err
}

func (l *Local) Start(ctx context.Context, name string) error {
	return l.box.Start(ctx, name)
}

func (l *Local) Stop(ctx context.Context, name string) error {
	return l.box.Stop(ctx, name)
}

func (l *Local) Doctor(ctx context.Context, name string) error {
	return l.box.Doctor(ctx, name)
}

func (l *Local) OpenPort(ctx context.Context, name string, port int) error {
	_, err := l.box.Open(ctx, name, strconv.Itoa(port), false)
	return err
}

type logBatch struct {
	ID    string        `json:"id"`
	Lines []app.LogLine `json:"lines"`
}

func (l *Local) StreamLogs(ctx context.Context, id, name, source, unit string, follow bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(l.lifetime, cancel)
	defer stop()
	if err := l.lifetime.Err(); err != nil {
		return err
	}
	if !follow {
		var lines []app.LogLine
		err := l.box.Logs(ctx, name, app.LogOptions{Source: source, Unit: unit, Lines: 100}, func(line app.LogLine) error {
			lines = append(lines, line)
			return ctx.Err()
		})
		if len(lines) > 0 && ctx.Err() == nil {
			l.emit("local:logs", logBatch{id, lines})
		}
		return err
	}
	return l.followLogs(ctx, id, name, source, unit)
}

func (l *Local) followLogs(ctx context.Context, id, name, source, unit string) error {
	const batchSize = 64
	lines := make(chan app.LogLine, batchSize)
	done := make(chan error, 1)
	go func() {
		done <- l.box.Logs(ctx, name, app.LogOptions{Source: source, Unit: unit, Lines: 100, Follow: true}, func(line app.LogLine) error {
			select {
			case lines <- line:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(lines)
	}()

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]app.LogLine, 0, batchSize)
	incoming := lines
	flush := func() {
		if len(batch) > 0 && ctx.Err() == nil {
			l.emit("local:logs", logBatch{id, batch})
			batch = make([]app.LogLine, 0, batchSize)
		}
	}
	for {
		select {
		case <-ctx.Done():
			<-done
			return ctx.Err()
		case <-ticker.C:
			flush()
			incoming = lines
		case line, ok := <-incoming:
			if !ok {
				flush()
				return <-done
			}
			batch = append(batch, line)
			if len(batch) == batchSize {
				incoming = nil
			}
		}
	}
}

func bundledBox() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err := exec.LookPath(filepath.Join(filepath.Dir(executable), "box"))
	if err != nil {
		return "", fmt.Errorf("desktop box executable: %w", err)
	}
	return path, nil
}

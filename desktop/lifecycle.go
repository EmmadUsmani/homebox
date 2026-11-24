package main

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

type Lifecycle struct {
	box *app.Service
	mu  sync.Mutex
	job *lifecycleProgress
}

type creationDefaults struct {
	app.NewOptions
	Profiles []string
}

func (l *Lifecycle) Defaults() (creationDefaults, error) {
	options, err := l.box.NewDefaults()
	return creationDefaults{NewOptions: options, Profiles: l.box.Profiles.Names()}, err
}

func (l *Lifecycle) HostReady() (bool, error) {
	_, configured, err := l.box.Store.HostSettings()
	return configured, err
}

func (l *Lifecycle) Setup(ctx context.Context) error {
	return l.run("", func(service *app.Service) error {
		if _, err := exec.LookPath("limactl"); err != nil {
			return errors.New("Install Lima in /opt/homebrew/bin or /usr/local/bin, then try again")
		}
		return service.HostInit(ctx, app.HostInitOptions{})
	})
}

func (l *Lifecycle) Create(ctx context.Context, options app.NewOptions) error {
	return l.run(options.Name, func(service *app.Service) error {
		return service.New(ctx, options)
	})
}

func (l *Lifecycle) Provision(ctx context.Context, name string) error {
	return l.run(name, func(service *app.Service) error {
		return service.Provision(ctx, name)
	})
}

func (l *Lifecycle) Delete(ctx context.Context, name, confirmation string) error {
	if name != confirmation {
		return errors.New("Type the VM name to confirm deletion")
	}
	return l.run(name, func(service *app.Service) error {
		return service.Remove(ctx, name)
	})
}

type lifecycleProgress struct {
	name    string
	started time.Time
	Lines   []string `json:"lines"`
	Step    string   `json:"step,omitempty"`
	Retry   bool     `json:"retry"`
	busy    bool
}

func (l *Lifecycle) Progress() lifecycleProgress {
	l.mu.Lock()
	if l.job == nil {
		l.mu.Unlock()
		return lifecycleProgress{}
	}
	snapshot := *l.job
	snapshot.Lines = slices.Clone(snapshot.Lines)
	l.mu.Unlock()
	if snapshot.name != "" {
		run, ok, err := l.box.Store.LoadProvisionRun(snapshot.name)
		if err == nil && ok && !run.StartedAt.Before(snapshot.started) {
			snapshot.Step = untrusted.Line(run.Step)
		}
	}
	return snapshot
}

func (l *Lifecycle) run(name string, operation func(*app.Service) error) error {
	l.mu.Lock()
	if l.job != nil && l.job.busy {
		l.mu.Unlock()
		return errors.New("A VM operation is already running")
	}
	l.job = &lifecycleProgress{name: name, started: time.Now(), busy: true}
	l.mu.Unlock()

	service := *l.box
	service.Progress = lifecycleWriter{l}
	err := operation(&service)
	var failure *app.ProvisionError
	retry := errors.As(err, &failure)
	l.mu.Lock()
	l.job.Retry, l.job.busy = retry, false
	l.mu.Unlock()
	if retry {
		err = failure.Err
	}
	if err != nil {
		return errors.New(untrusted.Line(err.Error()))
	}
	return nil
}

type lifecycleWriter struct{ lifecycle *Lifecycle }

func (w lifecycleWriter) Write(data []byte) (int, error) {
	const maxLines = 160
	l := w.lifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, raw := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		line := untrusted.Line(raw)
		if len(l.job.Lines) == maxLines {
			copy(l.job.Lines, l.job.Lines[1:])
			l.job.Lines[maxLines-1] = line
		} else {
			l.job.Lines = append(l.job.Lines, line)
		}
	}
	return len(data), nil
}

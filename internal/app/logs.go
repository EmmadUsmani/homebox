package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// LogLine is one display-safe entry from a VM or its host services.
type LogLine struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Level  string    `json:"level,omitempty"`
	Unit   string    `json:"unit,omitempty"`
	Text   string    `json:"text"`
}

// LogOptions selects a source and limits the initial history per source.
type LogOptions struct {
	Source string
	Unit   string
	Lines  int
	Follow bool
}

type logSource struct {
	name   string
	stream func(context.Context, func(LogLine) error) error
}

// Logs emits a sorted snapshot or follows sources as their lines arrive.
// It calls emit serially and returns when emit fails or the context ends.
func (s *Service) Logs(ctx context.Context, name string, options LogOptions, emit func(LogLine) error) error {
	if options.Lines < 0 || options.Lines > 1000 {
		return errors.New("log line count must be between 0 and 1000")
	}
	switch options.Source {
	case "", "provision", "host", "net", "guest":
	default:
		return fmt.Errorf("unknown log source %q", options.Source)
	}
	if options.Unit != "" && options.Source != "" && options.Source != "guest" {
		return errors.New("--unit applies only to guest logs")
	}
	if len(options.Unit) > 256 || strings.ContainsRune(options.Unit, 0) {
		return errors.New("invalid guest unit")
	}
	if _, err := s.load(name); err != nil {
		return err
	}

	fileSource := func(source string, path func(context.Context) (string, error)) logSource {
		return logSource{source, func(ctx context.Context, emit func(LogLine) error) error {
			return streamLogFile(ctx, path, options, func(raw string, truncated bool) error {
				return emit(parseLogEntry(source, raw, truncated, time.Now().UTC()).LogLine)
			})
		}}
	}
	hostPath := ""
	sources := []logSource{
		fileSource("provision", func(context.Context) (string, error) {
			run, ok, err := s.Store.LoadProvisionRun(name)
			if err != nil || !ok || run.Log == "" {
				return "", err
			}
			if filepath.Base(run.Log) != run.Log || filepath.Ext(run.Log) != ".log" {
				return "", errors.New("invalid provisioning log name")
			}
			return filepath.Join(s.Store.ProvisionDir(name), run.Log), nil
		}),
		fileSource("host", func(ctx context.Context) (string, error) {
			if hostPath == "" {
				instance, err := s.Driver.Get(ctx, name)
				if err != nil && !errors.Is(err, vm.ErrNotFound) {
					return "", err
				}
				hostPath = instance.HostLog
			}
			return hostPath, nil
		}),
		fileSource("net", func(context.Context) (string, error) {
			return state.NetworkLogPath(s.Store.RunDir(name)), nil
		}),
		{"guest", func(ctx context.Context, emit func(LogLine) error) error {
			return s.streamJournal(ctx, name, options, emit)
		}},
	}
	if options.Source != "" {
		index := slices.IndexFunc(sources, func(source logSource) bool { return source.name == options.Source })
		sources = sources[index : index+1]
	}
	if options.Follow {
		return followLogSources(ctx, sources, emit)
	}

	var lines []LogLine
	for _, source := range sources {
		err := source.stream(ctx, func(line LogLine) error {
			lines = append(lines, line)
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s logs: %w", source.name, err)
		}
	}
	slices.SortStableFunc(lines, func(a, b LogLine) int { return a.Time.Compare(b.Time) })
	for _, line := range lines {
		if err := emit(line); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func followLogSources(ctx context.Context, sources []logSource, emit func(LogLine) error) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	lines := make(chan LogLine)
	done := make(chan error, len(sources))
	for _, source := range sources {
		workers.Go(func() {
			err := source.stream(ctx, func(line LogLine) error {
				select {
				case lines <- line:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if err != nil {
				err = fmt.Errorf("%s logs: %w", source.name, err)
			}
			done <- err
		})
	}
	for remaining := len(sources); remaining > 0; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil {
				return err
			}
			remaining--
		case line := <-lines:
			if err := emit(line); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) streamJournal(ctx context.Context, name string, options LogOptions, emit func(LogLine) error) error {
	argv := []string{"journalctl", "--no-pager", "-o", "json",
		"--output-fields=MESSAGE,PRIORITY,_SYSTEMD_UNIT,_SYSTEMD_USER_UNIT"}
	if options.Unit != "" {
		argv = append(argv, "--unit="+options.Unit)
	}
	if options.Follow {
		argv = append(argv, "-f")
	}
	backoff := logPollInterval
	reported := false
	var lastCursor string
	for ctx.Err() == nil {
		attempt := ctx
		cancel := func() {}
		if !options.Follow {
			attempt, cancel = context.WithTimeout(ctx, 10*time.Second)
		}
		endpoint, err := s.Driver.SSH(attempt, name)
		var diagnostic string
		var outputErr error
		if err == nil {
			count := 0
			output := logFramer{emit: func(raw string, truncated bool) error {
				// A guest may ignore journalctl's requested history limit.
				if !options.Follow {
					if count == options.Lines {
						return nil
					}
					count++
				}
				reported, backoff = false, logPollInterval
				entry := parseLogEntry("guest", raw, truncated, time.Now().UTC())
				outputErr = emit(entry.LogLine)
				if outputErr == nil && entry.cursor != "" {
					lastCursor = entry.cursor
				}
				return outputErr
			}}
			// SSH diagnostics are useful for pin, connection and journal permission failures.
			stderr := logFramer{emit: func(raw string, _ bool) error {
				if diagnostic == "" {
					diagnostic = logText(raw, 4096)
				}
				return nil
			}}
			args := slices.Clone(argv)
			if lastCursor == "" {
				args = append(args, "-n", strconv.Itoa(options.Lines))
			} else {
				args = append(args, "--after-cursor="+lastCursor)
			}
			err = s.GuestSSH.Stream(attempt, endpoint, args, &output, &stderr)
			if len(stderr.pending) > 0 {
				_ = stderr.finish()
			}
			if len(output.pending) > 0 && err == nil && outputErr == nil {
				outputErr = output.finish()
			}
		}
		cancel()
		if outputErr != nil {
			return outputErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if options.Follow || err != nil {
			if !reported {
				if diagnostic == "" && err != nil {
					diagnostic = err.Error()
				}
				message := "Guest journal unavailable"
				if diagnostic != "" {
					message += ": " + diagnostic
				}
				if err := emit(LogLine{Time: time.Now().UTC(), Source: "guest", Level: "warn", Text: logText(message, untrusted.MaxLine)}); err != nil {
					return err
				}
				reported = true
			}
		}
		if !options.Follow {
			return nil
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff = min(2*backoff, 4*time.Second)
	}
	return ctx.Err()
}

type logEntry struct {
	LogLine
	cursor string
}

func parseLogEntry(source, raw string, truncated bool, arrival time.Time) logEntry {
	line := logEntry{LogLine: LogLine{Time: arrival, Source: source}}
	var timestamp, text string
	switch source {
	case "provision":
		if prefix, message, ok := strings.Cut(raw, "\t"); ok {
			if _, err := time.Parse(time.RFC3339Nano, prefix); err == nil {
				timestamp, text = prefix, message
			}
		}
	case "host", "net", "guest":
		var fields map[string]json.RawMessage
		if !truncated && json.Unmarshal([]byte(raw), &fields) == nil {
			if source == "guest" {
				line.cursor = journalCursor(fields["__CURSOR"])
				if micros, err := strconv.ParseInt(logField(fields, "__REALTIME_TIMESTAMP"), 10, 64); err == nil && micros > 0 && micros <= 253402300799999999 {
					line.Time = time.UnixMicro(micros).UTC()
				}
				text = logField(fields, "MESSAGE")
				if text == "" && len(fields["MESSAGE"]) > 0 && fields["MESSAGE"][0] == '[' {
					text = "[binary journal message]"
				}
				line.Unit = logField(fields, "_SYSTEMD_USER_UNIT")
				if line.Unit == "" {
					line.Unit = logField(fields, "_SYSTEMD_UNIT")
				}
				levels := [...]string{"emerg", "alert", "crit", "error", "warn", "notice", "info", "debug"}
				if priority, err := strconv.Atoi(logField(fields, "PRIORITY")); err == nil && priority >= 0 && priority < len(levels) {
					line.Level = levels[priority]
				}
			} else {
				timestamp, text = logField(fields, "time"), logField(fields, "msg")
				line.Level = logField(fields, "level")
				var details []string
				for _, key := range []string{"protocol", "destination", "reason", "active", "limit", "error", "local", "remote", "instance", "driver", "component", "endpoint", "method", "outcome"} {
					if value := logField(fields, key); value != "" {
						details = append(details, key+"="+value)
					}
				}
				if len(details) > 0 {
					text += " (" + strings.Join(details, ", ") + ")"
				}
			}
		}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		line.Time = parsed
	}
	if text == "" && timestamp == "" {
		text = raw
	}
	line.Text = logText(text, untrusted.MaxLine)
	if truncated {
		const marker = " [line truncated]"
		line.Text = logText(text, untrusted.MaxLine-len(marker)) + marker
	}
	return line
}

var journalCursorPattern = regexp.MustCompile(`^[A-Za-z0-9=;_-]{1,512}$`)

func journalCursor(raw json.RawMessage) string {
	var cursor string
	if json.Unmarshal(raw, &cursor) == nil && journalCursorPattern.MatchString(cursor) {
		return cursor
	}
	return ""
}

func logField(fields map[string]json.RawMessage, name string) string {
	raw := fields[name]
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return ""
		}
		value = number.String()
	}
	return logText(value, 4096)
}

// logText also bounds the cleaned result, since replacing invalid UTF-8 can
// expand the input. Clip at a rune boundary to keep the JSON text intact.
func logText(raw string, limit int) string {
	text := untrusted.Line(raw)
	if len(text) <= limit {
		return text
	}
	for !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

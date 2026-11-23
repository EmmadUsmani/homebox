package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

type RemoteState string

const (
	RemoteReachable   RemoteState = "reachable"
	RemoteUnreachable RemoteState = "unreachable"
	RemoteStopped     RemoteState = "not-running"
	RemoteBroken      RemoteState = "connection-broken"
	RemoteKeyChanged  RemoteState = "host-key-changed"
)

type RemoteCheck struct {
	State     RemoteState `json:"state"`
	Status    *VMStatus   `json:"status,omitempty"`
	CheckedAt time.Time   `json:"checkedAt"`
}

type RemoteSource struct {
	Runner         execx.Runner
	Profiles       Profiles
	ConfigPath     string
	KnownHostsPath string
}

var remoteAlias = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func ValidRemoteAlias(alias string) bool { return remoteAlias.MatchString(alias) }

// Check runs the guest probe through the laptop's existing SSH login.
func (source RemoteSource) Check(ctx context.Context, alias string) (RemoteCheck, error) {
	if !ValidRemoteAlias(alias) {
		return RemoteCheck{}, errors.New("enter an SSH alias using letters, digits, dots, hyphens or underscores")
	}
	script, err := source.Profiles.ReadScript("common/status.sh")
	if err != nil {
		return RemoteCheck{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, guestProbeTimeout)
	defer cancel()
	args := []string{
		"-T", "-a", "-x", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ClearAllForwardings=yes",
		"-o", "ControlPath=none", "-o", "ControlMaster=no", "-o", "PermitLocalCommand=no",
		"-o", "ForkAfterAuthentication=no", "-o", "StdinNull=no", "-o", "Tunnel=no",
		"-o", "RemoteCommand=none", "-o", "SessionType=default",
		"-o", "StrictHostKeyChecking=yes", "-o", "GlobalKnownHostsFile=none",
		"-o", "KnownHostsCommand=none", "-o", "VerifyHostKeyDNS=no", "-o", "UpdateHostKeys=no",
		"-o", "UserKnownHostsFile=" + strconv.Quote(strings.ReplaceAll(source.KnownHostsPath, "%", "%%")),
	}
	if source.ConfigPath != "" {
		args = append(args, "-F", source.ConfigPath)
	}
	args = append(args, "--", alias, "env HOMEBOX_STATUS_MODE=detail sh -s")
	status := VMStatus{Name: alias, State: string(RemoteReachable)}
	var result execx.Result
	var sshErr error
	probeErr := runGuestProbe(ctx, cancel, &status, true, func(stdout, stderr io.Writer) error {
		result, sshErr = source.Runner.Run(ctx, execx.Cmd{
			Name: "/usr/bin/ssh", Args: args, Env: []string{"LC_ALL=C"},
			Stdin: bytes.NewReader(script), Stdout: stdout, Stderr: stderr,
		})
		return sshErr
	})
	state := RemoteReachable
	if status.Guest == nil {
		state = classifyRemote(result, sshErr, ctx.Err())
	}
	check := RemoteCheck{State: state, CheckedAt: time.Now()}
	if state == RemoteReachable {
		if probeErr != nil {
			status.addError("guest probe", probeErr)
		}
		check.Status = &status
	}
	return check, nil
}

func classifyRemote(result execx.Result, sshErr, contextErr error) RemoteState {
	if sshErr == nil {
		return RemoteReachable
	}
	var exit *execx.ExitError
	if errors.As(sshErr, &exit) && exit.ExitCode > 0 && exit.ExitCode < 255 {
		return RemoteReachable
	}
	for line := range strings.SplitSeq(string(result.Stderr), "\n") {
		if strings.TrimSuffix(line, "\r") == "Host key verification failed." {
			return RemoteKeyChanged
		}
	}
	for line := range strings.SplitSeq(string(result.Stderr), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "ssh: Could not resolve hostname "):
			return RemoteUnreachable
		case strings.HasPrefix(line, "ssh: connect to host "):
			switch {
			case strings.HasSuffix(line, ": Connection refused"):
				return RemoteStopped
			case strings.HasSuffix(line, ": Operation timed out"), strings.HasSuffix(line, ": Connection timed out"),
				strings.HasSuffix(line, ": No route to host"), strings.HasSuffix(line, ": Network is unreachable"):
				return RemoteUnreachable
			}
		case strings.Contains(line, "during banner exchange"), strings.HasPrefix(line, "kex_exchange_identification:"):
			return RemoteBroken
		}
	}
	if errors.Is(contextErr, context.DeadlineExceeded) {
		return RemoteUnreachable
	}
	return RemoteBroken
}

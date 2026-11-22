// Package sshx connects to guests using the system OpenSSH client.
package sshx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/term"
)

// Client runs OpenSSH commands. SSH defaults to "ssh".
type Client struct {
	Runner         execx.Runner
	SSH            string
	replaceProcess func(string, []string, []string) error
	isTerminal     func() bool
}

func (c Client) binary() string {
	if c.SSH != "" {
		return c.SSH
	}
	return "ssh"
}

func endpointArgs(ep vm.Endpoint) ([]string, error) {
	ip := net.ParseIP(ep.Host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("expected a loopback SSH endpoint, got %q", ep.Host)
	}
	hostKeyOptions := []string{
		"-o", "StrictHostKeyChecking=yes", "-o", "GlobalKnownHostsFile=none",
		"-o", "UserKnownHostsFile=" + strconv.Quote(strings.ReplaceAll(ep.KnownHosts, "%", "%%")),
	}
	if ep.KnownHosts == "" {
		// Drivers without a known-hosts file retain loopback-only legacy behavior.
		hostKeyOptions = []string{
			"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=" + os.DevNull,
		}
	}
	args := []string{
		"-F", "none", "-i", ep.IdentityFile, "-p", fmt.Sprint(ep.Port), "-o", "IdentitiesOnly=yes",
	}
	args = append(args, hostKeyOptions...)
	return append(args, "-o", "LogLevel=ERROR", "-o", "BatchMode=yes", "-o", "ForwardAgent=no"), nil
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func remoteCommand(argv []string) string {
	words := make([]string, len(argv))
	for index, arg := range argv {
		words[index] = quote(arg)
	}
	return strings.Join(words, " ")
}

var envName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// RunScript passes a POSIX shell script on stdin with the requested environment.
func (c Client) RunScript(ctx context.Context, ep vm.Endpoint, script []byte, env map[string]string, sudo bool, stdout, stderr io.Writer) error {
	var argv []string
	if sudo {
		argv = append(argv, "sudo", "-n")
	}
	if len(env) > 0 {
		argv = append(argv, "env")
		for _, name := range slices.Sorted(maps.Keys(env)) {
			if !envName.MatchString(name) || strings.ContainsRune(env[name], 0) {
				return fmt.Errorf("invalid remote environment entry %q", name)
			}
			argv = append(argv, name+"="+env[name])
		}
	}
	argv = append(argv, "sh", "-s")
	_, err := c.run(ctx, ep, argv, bytes.NewReader(script), stdout, stderr)
	return err
}

// Run runs a short remote command and captures its output.
func (c Client) Run(ctx context.Context, ep vm.Endpoint, argv ...string) (execx.Result, error) {
	return c.run(ctx, ep, argv, nil, nil, nil)
}

func (c Client) run(ctx context.Context, ep vm.Endpoint, argv []string, stdin io.Reader, stdout, stderr io.Writer) (execx.Result, error) {
	args, err := endpointArgs(ep)
	if err != nil {
		return execx.Result{}, err
	}
	args = append(args, "--", ep.User+"@"+ep.Host, remoteCommand(argv))
	return c.Runner.Run(ctx, execx.Cmd{
		Name: c.binary(), Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr,
	})
}

// Interactive replaces the current process with an SSH session. Like plain
// ssh, the command words are handed to the guest's shell as typed, so
// `box ssh main -- 'make && ./run'` works; a terminal is requested only when
// box itself runs in one.
func (c Client) Interactive(ep vm.Endpoint, argv []string) error {
	args, err := endpointArgs(ep)
	if err != nil {
		return err
	}
	if c.stdinIsTerminal() {
		args = append(args, "-t")
	}
	args = append(args, "--", ep.User+"@"+ep.Host)
	args = append(args, argv...)
	path, err := exec.LookPath(c.binary())
	if err != nil {
		return err
	}
	replace := c.replaceProcess
	if replace == nil {
		replace = syscall.Exec
	}
	return replace(path, append([]string{c.binary()}, args...), os.Environ())
}

func (c Client) stdinIsTerminal() bool {
	if c.isTerminal != nil {
		return c.isTerminal()
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// AuthorizeKeys adds missing public key lines to the guest user's authorized_keys.
func (c Client) AuthorizeKeys(ctx context.Context, ep vm.Endpoint, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	var script strings.Builder
	script.WriteString(`set -eu
umask 077
dir="$HOME/.ssh"
file="$dir/authorized_keys"
mkdir -p "$dir"
chmod 700 "$dir"
touch "$file"
chmod 600 "$file"
for key in`)
	for _, key := range keys {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\x00\r\n") {
			return fmt.Errorf("authorized key must be a single nonempty line")
		}
		fmt.Fprintf(&script, " %s", quote(key))
	}
	script.WriteString(`; do
    if HOMEBOX_KEY="$key" awk '
        BEGIN { split(ENVIRON["HOMEBOX_KEY"], key) }
        $1 == key[1] && $2 == key[2] { found=1; exit }
        END { if (!found) exit 1 }
    ' "$file"; then
        continue
    else
        status=$?
        [ "$status" -eq 1 ] || exit "$status"
    fi
    # Repair a missing final newline before appending a new key.
    if [ -n "$(tail -c 1 "$file")" ]; then
        printf '\n' >> "$file"
    fi
    printf '%s\n' "$key" >> "$file"
done
`)
	return c.RunScript(ctx, ep, []byte(script.String()), nil, false, nil, nil)
}

//go:build darwin || linux

package vmnet

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	startTimeout = 5 * time.Second
	stopTimeout  = 5 * time.Second
	pollInterval = 20 * time.Millisecond
)

// Spawn starts exe in a new session with args followed by config.Args().
func Spawn(exe string, args []string, config Config) (int, error) {
	if err := config.validate(); err != nil {
		return 0, err
	}
	if pid, ok := Running(config.PIDFile); ok {
		return 0, fmt.Errorf("VM network daemon already running (PID %d)", pid)
	}
	file, err := openLog(config.LogFile)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	command := exec.Command(exe, append(append([]string(nil), args...), config.Args()...)...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdout = file
	command.Stderr = file
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	timeout := time.NewTimer(startTimeout)
	defer timeout.Stop()
	for {
		select {
		case err := <-exited:
			return 0, startupError(config.LogFile, info.Size(), fmt.Sprintf("daemon exited before readiness (%v)", err))
		case <-timeout.C:
			command.Process.Kill()
			<-exited
			Running(config.PIDFile)
			return 0, startupError(config.LogFile, info.Size(), "daemon startup timed out")
		case <-ticker.C:
			if ready, ok := Running(config.PIDFile); ok && ready == pid {
				return pid, nil
			}
		}
	}
}

func startupError(path string, offset int64, message string) error {
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		if _, err := file.Seek(offset, io.SeekStart); err == nil {
			if data, err := io.ReadAll(io.LimitReader(file, 16<<10)); err == nil && len(data) != 0 {
				return fmt.Errorf("%s: %s", message, strings.TrimSpace(string(data)))
			}
		}
	}
	return errors.New(message)
}

func lockPIDFile(path string) (*os.File, error) {
	deadline := time.Now().Add(5 * pollInterval)
	for {
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			file.Close()
			// A status check may briefly hold the lock while clearing a stale file.
			if errors.Is(err, syscall.EWOULDBLOCK) && time.Now().Before(deadline) {
				time.Sleep(pollInterval)
				continue
			}
			return nil, fmt.Errorf("lock VM network PID file: %w", err)
		}
		if !currentPIDFile(file) {
			file.Close()
			continue
		}
		if err := file.Truncate(0); err != nil {
			file.Close()
			return nil, err
		}
		return file, nil
	}
}

func currentPIDFile(file *os.File) bool {
	open, openErr := file.Stat()
	current, pathErr := os.Lstat(file.Name())
	return openErr == nil && pathErr == nil && os.SameFile(open, current)
}

func removePIDFile(file *os.File) {
	if currentPIDFile(file) {
		os.Remove(file.Name())
	}
}

// Running reports a ready daemon and removes any unlocked stale PID file.
func Running(pidFile string) (int, bool) {
	pid, _ := running(pidFile)
	return pid, pid != 0
}

func running(path string) (int, error) {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		removePIDFile(file)
		return 0, nil
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return 0, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 32))
	if err != nil || !currentPIDFile(file) {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, nil
	}
	return pid, nil
}

// Stop terminates the ready daemon and escalates if it fails to exit.
func Stop(pidFile string) error {
	pid, err := running(pidFile)
	if err != nil || pid == 0 {
		return err
	}
	if pid == os.Getpid() {
		return fmt.Errorf("cannot stop in-process daemon by PID")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	for _, signal := range []os.Signal{syscall.SIGTERM, os.Kill} {
		current, err := running(pidFile)
		if err != nil || current != pid {
			return err
		}
		if err := process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		deadline := time.Now().Add(stopTimeout)
		for time.Now().Before(deadline) {
			current, err := running(pidFile)
			if err != nil || current != pid {
				return err
			}
			time.Sleep(pollInterval)
		}
	}
	return fmt.Errorf("VM network daemon (PID %d) did not exit", pid)
}

package hostinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/sys/unix"
)

func TestHostagentUsageMatchesInstance(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "vm with spaces", "ha.pid")
	if err := os.MkdirAll(filepath.Dir(pidFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintln(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		want bool
	}{
		{pidFile, true},
		{pidFile + "-other", false},
		{filepath.Join(t.TempDir(), "other", "ha.pid"), false},
	} {
		runner := &execx.Fake{}
		runner.On("/bin/ps -ww -p ", fmt.Sprintf("%d 2.5 1-02:03:04 limactl hostagent --pidfile %s --socket ha.sock vm\n", os.Getpid(), test.path), nil)
		usage := (ProcessUsage{Runner: runner}).Sample(t.Context(), []vm.Instance{{Name: "vm", HostPIDFile: pidFile}})["vm"].HostAgent
		if test.want {
			if usage == nil || usage.CPUPercent != 2.5 || usage.MemoryBytes == nil || *usage.MemoryBytes == 0 || usage.UptimeSeconds != 93784 || usage.Source != "hostagent" {
				t.Fatalf("unexpected sample: %+v", usage)
			}
		} else if usage != nil {
			t.Fatalf("misattributed sample: %+v", usage)
		}
	}
}

const testVirtualizationCommand = "/System/Library/Frameworks/Virtualization.framework/XPCServices/" + virtualizationProcess

func TestVirtualizationDiskMatching(t *testing.T) {
	diskA := "/instances/box with space/disk"
	diskB := "/instances/box with space-other\nline\\n/disk"
	identityA := diskIdentity{device: 17, inode: 21}
	identityB := diskIdentity{device: 17, inode: 34}
	disks := map[string]diskIdentity{lsofPath(diskA): identityA, lsofPath(diskB): identityB}
	if got := lsofPath(diskB); got != `/instances/box with space-other\nline\\n/disk` {
		t.Fatalf("encoded disk path = %q", got)
	}
	process := func(pid int, command string) string {
		return fmt.Sprintf("p%d\x00c%s\x00\n", pid, command)
	}
	file := func(descriptor, name string, identity diskIdentity) string {
		return fmt.Sprintf("f%s\x00D0x%x\x00i%d\x00n%s\x00\n", descriptor, identity.device, identity.inode, lsofPath(name))
	}
	validA := process(101, virtualizationProcess) + file("4", diskA, identityA)
	validB := process(102, virtualizationProcess) + file("5", diskB, identityB)
	invalid := process(103, virtualizationProcess) +
		file("4", diskA+"-suffix", identityA) +
		file("5", diskA, diskIdentity{device: identityA.device, inode: 999}) +
		file("txt", diskB, identityB) +
		process(104, "another-process") + file("4", diskA, identityA)
	got := virtualizationPIDs([]byte(validA+invalid+validB), disks)
	if len(got) != 2 || got[lsofPath(diskA)] != 101 || got[lsofPath(diskB)] != 102 {
		t.Fatalf("disk attribution = %v", got)
	}
	for _, ambiguous := range []string{
		validA + process(105, virtualizationProcess) + file("4", diskA, identityA),
		validA + file("5", diskB, identityB),
	} {
		if got := virtualizationPIDs([]byte(ambiguous), disks); len(got) != 0 {
			t.Fatalf("ambiguous disks matched: %v", got)
		}
	}
}

func TestVirtualizationRejectsStaleAndReusedPIDs(t *testing.T) {
	metrics := processMetrics{usage: Usage{PID: 101}, command: testVirtualizationCommand}
	started := unix.Timeval{Sec: 200, Usec: 41}
	if got := metrics.virtualizationUsage(started, started); got == nil || got.PID != 101 || got.Source != "virtualization" {
		t.Fatalf("same process rejected: %+v", got)
	}
	for _, current := range []unix.Timeval{{}, {Sec: 200, Usec: 42}} {
		if got := metrics.virtualizationUsage(started, current); got != nil {
			t.Fatalf("stale or reused PID accepted: %+v", got)
		}
	}
}

func TestVirtualizationBatchUsesOneLsofCall(t *testing.T) {
	var instances []vm.Instance
	var records string
	machines := []string{"vm a", "vm b\nnext"}
	for _, name := range machines {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "disk"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		path, identity, ok := virtualizationDisk(dir)
		if !ok {
			t.Fatal("missing fixture disk")
		}
		instances = append(instances, vm.Instance{Name: name, RuntimeDir: dir})
		pid := os.Getpid() + len(instances) - 1
		records += fmt.Sprintf("p%d\x00c%s\x00\nf4\x00D0x%x\x00i%d\x00n%s\x00\n", pid, virtualizationProcess, identity.device, identity.inode, path)
	}
	runner := &execx.Fake{}
	runner.On("/usr/sbin/lsof ", records, nil)
	runner.On("/bin/ps ", fmt.Sprintf("%d 2.5 00:04 %s\n", os.Getpid(), testVirtualizationCommand), nil)
	got := (ProcessUsage{Runner: runner}).Sample(t.Context(), instances)
	calls := runner.Commands()
	if len(calls) != 2 || got[machines[0]].VM == nil || got[machines[1]].VM != nil {
		t.Fatalf("calls=%v, samples=%+v", calls, got)
	}
}

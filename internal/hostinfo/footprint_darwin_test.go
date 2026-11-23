package hostinfo

import (
	"os"
	"testing"
	"unsafe"
)

func TestRusageInfoV0Layout(t *testing.T) {
	var info rusageInfoV0
	if size := unsafe.Sizeof(info); size != 96 {
		t.Fatalf("rusage_info_v0 size = %d, want 96", size)
	}
	offsets := []uintptr{
		unsafe.Offsetof(info.uuid), unsafe.Offsetof(info.userTime),
		unsafe.Offsetof(info.systemTime), unsafe.Offsetof(info.packageIdleWakeups),
		unsafe.Offsetof(info.interruptWakeups), unsafe.Offsetof(info.pageins),
		unsafe.Offsetof(info.wiredSize), unsafe.Offsetof(info.residentSize),
		unsafe.Offsetof(info.physicalFootprint), unsafe.Offsetof(info.procStartAbstime),
		unsafe.Offsetof(info.procExitAbstime),
	}
	want := []uintptr{0, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88}
	for index, offset := range offsets {
		if offset != want[index] {
			t.Errorf("rusage_info_v0 field %d offset = %d, want %d", index, offset, want[index])
		}
	}
}

func TestPhysicalFootprintCurrentProcess(t *testing.T) {
	bytes := physicalFootprint(os.Getpid())
	if bytes == nil || *bytes == 0 {
		t.Fatalf("current process has no physical footprint: %v", bytes)
	}
}

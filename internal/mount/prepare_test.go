package mount

import "testing"

func TestGuestDevice(t *testing.T) {
	for i, want := range []string{"/dev/vdb", "/dev/vdc", "/dev/vdd"} {
		if got := guestDevice(i); got != want {
			t.Errorf("guestDevice(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestOnTmpfs(t *testing.T) {
	if OnTmpfs("/proc") {
		t.Error("/proc reported as tmpfs")
	}
	if OnTmpfs("/does/not/exist") {
		t.Error("missing path reported as tmpfs")
	}
	if !OnTmpfs("/dev/shm") {
		t.Skip("/dev/shm is not tmpfs on this host")
	}
}

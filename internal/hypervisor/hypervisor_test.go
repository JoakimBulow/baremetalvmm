package hypervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/raesene/baremetalvmm/internal/vm"
)

func TestForDefaultsToFirecracker(t *testing.T) {
	for _, backend := range []string{"", Firecracker} {
		v := &vm.VM{Name: "a", Backend: backend}
		if got := For(v).Name(); got != Firecracker {
			t.Errorf("For(Backend=%q).Name() = %q, want %q", backend, got, Firecracker)
		}
		if got := Name(v); got != Firecracker {
			t.Errorf("Name(Backend=%q) = %q, want %q", backend, got, Firecracker)
		}
		if err := Supported(v); err != nil {
			t.Errorf("Supported(Backend=%q): %v", backend, err)
		}
		if _, err := SnapshotterFor(v); err != nil {
			t.Errorf("SnapshotterFor(Backend=%q): %v", backend, err)
		}
	}
}

func TestUnknownBackendLeavesVMAlone(t *testing.T) {
	v := &vm.VM{Name: "a", Backend: "from-the-future", State: vm.StateRunning, PID: 4242}

	b := For(v)
	if b.Name() != "from-the-future" {
		t.Fatalf("Name() = %q", b.Name())
	}

	// Firecracker would find no process and report the VM stopped; the
	// stand-in must not, or stop/delete would orphan the real process.
	b.UpdateVMState(v)
	if v.State != vm.StateRunning || v.PID != 4242 {
		t.Errorf("UpdateVMState changed the VM: state=%s pid=%d", v.State, v.PID)
	}

	if err := b.Terminate(context.Background(), v); err == nil {
		t.Error("Terminate succeeded for an unknown backend")
	}
	if v.PID != 4242 {
		t.Errorf("Terminate cleared PID to %d", v.PID)
	}
	if _, err := b.Start(&vm.LaunchConfig{}); err == nil {
		t.Error("Start succeeded for an unknown backend")
	}
	if err := Supported(v); err == nil {
		t.Error("Supported accepted an unknown backend")
	}
	if _, err := SnapshotterFor(v); !errors.Is(err, ErrSnapshotsUnsupported) {
		t.Errorf("SnapshotterFor error = %v, want ErrSnapshotsUnsupported", err)
	}
}

func TestNamesMatchBackends(t *testing.T) {
	for _, name := range Names {
		if got := For(&vm.VM{Backend: name}).Name(); got != name {
			t.Errorf("For(%q).Name() = %q", name, got)
		}
	}
}

func TestCloudHypervisorHasNoSnapshots(t *testing.T) {
	v := &vm.VM{Name: "a", Backend: CloudHypervisor}
	if err := Supported(v); err != nil {
		t.Errorf("Supported: %v", err)
	}
	if _, err := SnapshotterFor(v); !errors.Is(err, ErrSnapshotsUnsupported) {
		t.Errorf("SnapshotterFor error = %v, want ErrSnapshotsUnsupported", err)
	}
}

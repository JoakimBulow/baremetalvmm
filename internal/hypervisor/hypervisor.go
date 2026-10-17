// Package hypervisor picks the backend that runs a VM. Firecracker is the
// default; a VM records its backend in vm.VM.Backend, and everything that
// starts, stops or checks on a VM goes through For so it reaches the right
// one.
package hypervisor

import (
	"context"
	"errors"
	"fmt"

	"github.com/raesene/baremetalvmm/internal/cloudhypervisor"
	"github.com/raesene/baremetalvmm/internal/firecracker"
	"github.com/raesene/baremetalvmm/internal/vm"
)

// Backend names, as stored in vm.VM.Backend.
const (
	// Firecracker is the default backend, used when a VM doesn't name one.
	Firecracker = "firecracker"
	// CloudHypervisor can expose hardware virtualization to the guest, so
	// nested VMs use it.
	CloudHypervisor = "cloud-hypervisor"
)

// Names lists the supported backends, default first.
var Names = []string{Firecracker, CloudHypervisor}

// Backend runs VMs under one hypervisor.
type Backend interface {
	// Name identifies the backend in VM and snapshot records.
	Name() string
	// Start boots a VM and returns the hypervisor process's PID. The process
	// must outlive the caller, so Start takes no context.
	Start(cfg *vm.LaunchConfig) (int, error)
	// Terminate stops the VM's hypervisor process and returns once it has
	// exited. On error v.PID still holds the surviving process.
	Terminate(ctx context.Context, v *vm.VM) error
	// UpdateVMState sets v.State and v.PID from the processes actually running.
	UpdateVMState(v *vm.VM)
	// Version is the hypervisor binary's version, or "" if unknown.
	Version() string
}

// Snapshotter is a Backend that can snapshot and restore a running VM.
type Snapshotter interface {
	Backend
	PauseVM(ctx context.Context, socketPath string) error
	ResumeVM(ctx context.Context, socketPath string) error
	// CreateSnapshotFiles writes guest memory and device state of a paused VM.
	CreateSnapshotFiles(ctx context.Context, socketPath, memPath, statePath string) error
	// RestoreVM starts a new hypervisor process from a snapshot, resumes the
	// guest and returns the process's PID.
	RestoreVM(ctx context.Context, socketPath, logPath, memPath, statePath string) (int, error)
}

var (
	_ Snapshotter = (*firecracker.Client)(nil)
	_ Backend     = (*cloudhypervisor.Client)(nil)
)

// ErrSnapshotsUnsupported is returned for VMs whose backend can't snapshot.
var ErrSnapshotsUnsupported = errors.New("snapshots are not supported")

// Name returns the backend a VM runs under.
func Name(v *vm.VM) string {
	if v.Backend == "" {
		return Firecracker
	}
	return v.Backend
}

// For returns the backend for a VM. A backend this build doesn't know (the VM
// was created by a newer vmm, say) gets a stand-in that refuses to start or
// stop it and leaves its recorded state alone: treating it as Firecracker
// would report it stopped and orphan its process.
func For(v *vm.VM) Backend {
	switch name := Name(v); name {
	case Firecracker:
		return firecracker.NewClient()
	case CloudHypervisor:
		return cloudhypervisor.NewClient()
	default:
		return unknown{name: name}
	}
}

// Supported returns an error if this build can't run the VM's backend.
// Check it before preparing anything for a start.
func Supported(v *vm.VM) error {
	if u, ok := For(v).(unknown); ok {
		return u.err(v.Name)
	}
	return nil
}

// SnapshotterFor returns the backend for a VM if it supports snapshots.
func SnapshotterFor(v *vm.VM) (Snapshotter, error) {
	if s, ok := For(v).(Snapshotter); ok {
		return s, nil
	}
	return nil, fmt.Errorf("VM '%s' runs under %s: %w", v.Name, Name(v), ErrSnapshotsUnsupported)
}

// UpdateVMState refreshes a VM's state from its backend.
func UpdateVMState(v *vm.VM) {
	For(v).UpdateVMState(v)
}

// Terminate stops a VM through its backend.
func Terminate(ctx context.Context, v *vm.VM) error {
	return For(v).Terminate(ctx, v)
}

// unknown stands in for a backend this build doesn't support.
type unknown struct {
	name string
}

func (u unknown) Name() string { return u.name }

func (u unknown) err(v string) error {
	return fmt.Errorf("VM '%s' uses the %q backend, which this version of vmm doesn't support", v, u.name)
}

func (u unknown) Start(cfg *vm.LaunchConfig) (int, error) {
	return 0, fmt.Errorf("unsupported backend %q", u.name)
}

func (u unknown) Terminate(ctx context.Context, v *vm.VM) error { return u.err(v.Name) }

func (u unknown) UpdateVMState(v *vm.VM) {}

func (u unknown) Version() string { return "" }

// Package vmproc finds and stops the hypervisor process behind a VM. Every
// backend runs one process per VM, named after the backend's binary and
// passed the VM's API socket path as an argument, so a process is matched by
// both: the socket file existing or a PID being recorded proves nothing.
package vmproc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/raesene/baremetalvmm/internal/vm"
)

const (
	// GracefulWait is how long to wait for the guest to power itself off
	// after a graceful shutdown request before escalating to signals. Guest
	// kernels often can't see the request (no i8042 for Firecracker's
	// Ctrl+Alt+Del, no ACPI button for Cloud Hypervisor's power button), so
	// this is kept short.
	GracefulWait = 3 * time.Second
	// sigtermWait is how long to wait for the process to exit after SIGTERM.
	sigtermWait = 5 * time.Second
	// sigkillWait is how long to wait for the process to disappear after SIGKILL.
	sigkillWait = 3 * time.Second
	// exitPollInterval is how often process liveness is polled while waiting.
	exitPollInterval = 100 * time.Millisecond
)

// Binary is the name of a hypervisor's executable, e.g. "firecracker".
type Binary string

// procArgs returns the argument vector of a process from /proc/<pid>/cmdline.
// Returns nil if the process does not exist or cannot be read.
func procArgs(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}
	args := strings.Split(string(data), "\x00")
	// cmdline is null-terminated, so the split leaves a trailing empty element
	for len(args) > 0 && args[len(args)-1] == "" {
		args = args[:len(args)-1]
	}
	return args
}

// Is reports whether pid is running this binary. /proc/<pid>/comm is
// truncated to 15 characters ("cloud-hyperviso"), so argv[0] is used.
func (b Binary) Is(pid int) bool {
	args := procArgs(pid)
	return len(args) > 0 && strings.Contains(filepath.Base(args[0]), string(b))
}

// Matches reports whether pid is this binary serving the given API socket.
// This protects against acting on a recycled PID that belongs to a
// different VM or program.
func (b Binary) Matches(pid int, socketPath string) bool {
	if pid <= 0 || socketPath == "" {
		return false
	}
	args := procArgs(pid)
	if len(args) == 0 || !strings.Contains(filepath.Base(args[0]), string(b)) {
		return false
	}
	for _, arg := range args[1:] {
		if arg == socketPath {
			return true
		}
	}
	return false
}

// Find scans /proc for a running process of this binary whose API socket is
// socketPath. Returns 0 if none is found. This recovers the PID of a VM whose
// recorded PID was lost (for example cleared by an earlier stop that did not
// actually terminate the process).
func (b Binary) Find(socketPath string) int {
	if socketPath == "" {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if b.Matches(pid, socketPath) {
			return pid
		}
	}
	return 0
}

// Resolve returns the PID of the live process for a VM. It prefers the
// recorded PID when that still refers to the VM's process, and otherwise
// falls back to scanning /proc for the API socket. Returns 0 when no matching
// process is running.
func (b Binary) Resolve(socketPath string, recordedPID int) int {
	if b.Matches(recordedPID, socketPath) {
		return recordedPID
	}
	return b.Find(socketPath)
}

// WaitForExit polls until the process is gone or the timeout expires.
// Returns true if the process exited.
func (b Binary) WaitForExit(pid int, socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !b.Matches(pid, socketPath) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(exitPollInterval)
	}
}

// SignalAndWait sends sig to pid and waits up to timeout for it to exit.
// Returns true if the process exited.
func (b Binary) SignalAndWait(pid int, socketPath string, sig syscall.Signal, timeout time.Duration) bool {
	if err := syscall.Kill(pid, sig); err != nil {
		// ESRCH means the process is already gone
		return err == syscall.ESRCH
	}
	return b.WaitForExit(pid, socketPath, timeout)
}

// UpdateVMState sets the VM's state from its process. When a running process
// is found, the VM's PID is repaired to match it so that later stop/delete
// operations terminate the right process.
func (b Binary) UpdateVMState(v *vm.VM) {
	if pid := b.Resolve(v.SocketPath, v.PID); pid > 0 {
		v.PID = pid
		v.State = vm.StateRunning
	} else {
		v.PID = 0
		if v.State == vm.StateRunning || v.State == vm.StateStarting || v.State == vm.StateStopping {
			v.State = vm.StateStopped
		}
	}
}

// Terminate stops the process backing a VM and does not return until it is
// gone. It escalates from graceful (a guest shutdown request over the API
// socket; skipped when nil or when the socket file is missing) to SIGTERM and
// finally SIGKILL, then removes the API socket file.
//
// The VM's PID field is updated to reflect reality: it is set to the PID that
// was actually terminated while work is in progress, and cleared to 0 once the
// process is confirmed gone. An error is returned only when a process is still
// running afterwards, in which case PID still holds the surviving PID so the
// caller can report it rather than orphan it.
func (b Binary) Terminate(ctx context.Context, v *vm.VM, graceful func(ctx context.Context) error) error {
	pid := b.Resolve(v.SocketPath, v.PID)
	if pid == 0 {
		return finish(v)
	}
	v.PID = pid

	if graceful != nil {
		if _, err := os.Stat(v.SocketPath); err == nil {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := graceful(shutdownCtx)
			cancel()
			if err == nil && b.WaitForExit(pid, v.SocketPath, GracefulWait) {
				return finish(v)
			}
		}
	}

	if b.SignalAndWait(pid, v.SocketPath, syscall.SIGTERM, sigtermWait) {
		return finish(v)
	}

	fmt.Fprintf(os.Stderr, "Warning: %s process %d for VM %s ignored SIGTERM, sending SIGKILL\n", b, pid, v.Name)
	if b.SignalAndWait(pid, v.SocketPath, syscall.SIGKILL, sigkillWait) {
		return finish(v)
	}

	return fmt.Errorf("%s process %d for VM '%s' is still running after SIGKILL", b, pid, v.Name)
}

// finish records that the VM's process is gone and removes its socket. A
// socket that won't go away is left for the next start to replace: callers
// treat any error as a surviving process.
func finish(v *vm.VM) error {
	v.PID = 0
	_ = os.Remove(v.SocketPath)
	return nil
}

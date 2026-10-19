package vmproc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/raesene/baremetalvmm/internal/vm"
)

const (
	firecracker     = Binary("firecracker")
	cloudHypervisor = Binary("cloud-hypervisor")
)

// startSleeper starts a long-running process with the given argv. argv[0] sets
// the name the /proc inspection helpers see.
func startSleeper(t *testing.T, argv0 string, extraArgs ...string) *exec.Cmd {
	t.Helper()

	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	// Words after the -c script become $0, $1... for the shell, so they show
	// up in /proc/<pid>/cmdline without being interpreted.
	cmd := &exec.Cmd{
		Path: sh,
		Args: append([]string{argv0, "-c", "sleep 60"}, extraArgs...),
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait for the child to exec so that /proc/<pid>/cmdline reports argv0
	deadline := time.Now().Add(5 * time.Second)
	for {
		if args := procArgs(cmd.Process.Pid); len(args) > 0 && args[0] == argv0 {
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake process %d never exec'd as %s", cmd.Process.Pid, argv0)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fakeFirecracker starts a process that looks like a Firecracker instance
// serving socketPath to the /proc inspection helpers.
func fakeFirecracker(t *testing.T, socketPath string) *exec.Cmd {
	t.Helper()
	return startSleeper(t, "/usr/local/bin/firecracker", "--api-sock", socketPath)
}

func TestIs(t *testing.T) {
	cmd := fakeFirecracker(t, "/tmp/test-is.sock")
	if !firecracker.Is(cmd.Process.Pid) {
		t.Error("expected fake process to be detected as firecracker")
	}
	if firecracker.Is(os.Getpid()) {
		t.Error("expected test process not to be detected as firecracker")
	}
	if firecracker.Is(0) || firecracker.Is(-1) {
		t.Error("expected invalid PIDs not to be detected as firecracker")
	}
}

func TestMatches(t *testing.T) {
	socket := "/tmp/test-match.sock"
	cmd := fakeFirecracker(t, socket)
	pid := cmd.Process.Pid

	if !firecracker.Matches(pid, socket) {
		t.Error("expected process to match its own socket")
	}
	if firecracker.Matches(pid, "/tmp/other.sock") {
		t.Error("expected process not to match a different socket")
	}
	if firecracker.Matches(pid, "") {
		t.Error("expected empty socket path never to match")
	}
	// A non-Firecracker process that happens to mention the socket must not match
	other := startSleeper(t, "/bin/sleeper", socket)
	if firecracker.Matches(other.Process.Pid, socket) {
		t.Error("expected non-firecracker process not to match")
	}
}

func TestCloudHypervisorMatchesOnArgv0(t *testing.T) {
	// The kernel truncates /proc/<pid>/comm to "cloud-hyperviso", so matching
	// has to use the full argv[0].
	socket := "/tmp/test-ch.sock"
	cmd := startSleeper(t, "/usr/local/bin/cloud-hypervisor", "--api-socket", socket)
	pid := cmd.Process.Pid

	if !cloudHypervisor.Matches(pid, socket) {
		t.Error("expected cloud-hypervisor process to match its socket")
	}
	if firecracker.Matches(pid, socket) {
		t.Error("expected firecracker not to claim a cloud-hypervisor process")
	}
	fc := fakeFirecracker(t, socket)
	if cloudHypervisor.Matches(fc.Process.Pid, socket) {
		t.Error("expected cloud-hypervisor not to claim a firecracker process")
	}
}

func TestFind(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "find.sock")
	cmd := fakeFirecracker(t, socket)

	if got := firecracker.Find(socket); got != cmd.Process.Pid {
		t.Errorf("Find = %d, want %d", got, cmd.Process.Pid)
	}
	if got := firecracker.Find("/tmp/no-such-vm.sock"); got != 0 {
		t.Errorf("Find for unknown socket = %d, want 0", got)
	}
	if got := firecracker.Find(""); got != 0 {
		t.Errorf("Find for empty socket = %d, want 0", got)
	}
}

func TestResolveRecoversLostPID(t *testing.T) {
	socket := "/tmp/test-resolve.sock"
	cmd := fakeFirecracker(t, socket)
	pid := cmd.Process.Pid

	if got := firecracker.Resolve(socket, pid); got != pid {
		t.Errorf("Resolve with correct pid = %d, want %d", got, pid)
	}
	// A VM record whose PID was cleared must still find its process
	if got := firecracker.Resolve(socket, 0); got != pid {
		t.Errorf("Resolve with lost pid = %d, want %d", got, pid)
	}
	// A stale PID pointing at something else must fall back to the scan
	if got := firecracker.Resolve(socket, os.Getpid()); got != pid {
		t.Errorf("Resolve with stale pid = %d, want %d", got, pid)
	}
}

func TestSignalAndWait(t *testing.T) {
	socket := "/tmp/test-signal.sock"
	cmd := fakeFirecracker(t, socket)
	pid := cmd.Process.Pid

	if !firecracker.SignalAndWait(pid, socket, syscall.SIGKILL, 5*time.Second) {
		t.Fatalf("expected pid %d to exit after SIGKILL", pid)
	}
	_, _ = cmd.Process.Wait()

	if firecracker.Matches(pid, socket) {
		t.Error("expected process to be gone after SIGKILL")
	}
	// Signalling a process that is already gone reports success
	if !firecracker.SignalAndWait(pid, socket, syscall.SIGKILL, time.Second) {
		t.Error("expected SignalAndWait on a dead process to report exit")
	}
}

func TestUpdateVMStateUsesProcessNotSocketFile(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "missing.sock")
	cmd := fakeFirecracker(t, socket)
	v := &vm.VM{Name: "t", SocketPath: socket, State: vm.StateStopped}

	// The socket file was never created, but the process is alive: the VM
	// must still be reported as running so it is not silently orphaned.
	firecracker.UpdateVMState(v)
	if v.State != vm.StateRunning || v.PID != cmd.Process.Pid {
		t.Errorf("got state=%s pid=%d, want running with pid %d", v.State, v.PID, cmd.Process.Pid)
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	if !firecracker.WaitForExit(cmd.Process.Pid, socket, 5*time.Second) {
		t.Fatal("fake firecracker did not exit")
	}
	firecracker.UpdateVMState(v)
	if v.State != vm.StateStopped || v.PID != 0 {
		t.Errorf("got state=%s pid=%d, want stopped with pid 0", v.State, v.PID)
	}
}

func TestTerminateEscalatesWhenGracefulIsIgnored(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "term.sock")
	if err := os.WriteFile(socket, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := fakeFirecracker(t, socket)
	// Reap the child as soon as it dies so it doesn't linger as a zombie,
	// which /proc would still list.
	go func() { _, _ = cmd.Process.Wait() }()

	old := gracefulWait
	gracefulWait = 500 * time.Millisecond
	defer func() { gracefulWait = old }()

	v := &vm.VM{Name: "t", SocketPath: socket}
	asked := false
	err := firecracker.Terminate(context.Background(), v, func(context.Context) error {
		asked = true
		return nil // the guest "accepted" but never powers off
	})
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !asked {
		t.Error("graceful shutdown was not requested")
	}
	if v.PID != 0 {
		t.Errorf("PID = %d after Terminate, want 0", v.PID)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Error("socket file was not removed")
	}
}

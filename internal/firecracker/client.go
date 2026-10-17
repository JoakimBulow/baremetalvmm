package firecracker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"

	"github.com/raesene/baremetalvmm/internal/vm"
	"github.com/raesene/baremetalvmm/internal/vmproc"
)

const (
	DefaultFirecrackerBin = "/usr/local/bin/firecracker"
)

// Client wraps the Firecracker SDK for VM management
type Client struct {
	FirecrackerBin string
	Logger         *logrus.Logger
}

// NewClient creates a new Firecracker client
func NewClient() *Client {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	return &Client{
		FirecrackerBin: DefaultFirecrackerBin,
		Logger:         logger,
	}
}

// Name identifies this backend in VM and snapshot records.
func (c *Client) Name() string {
	return "firecracker"
}

// Start boots a Firecracker microVM and returns the Firecracker process's
// PID. The process isn't tied to any caller's context, so it outlives the
// command or HTTP request that started it.
func (c *Client) Start(cfg *vm.LaunchConfig) (int, error) {
	if cfg.Nested {
		return 0, errors.New("firecracker can't run nested VMs; use the cloud-hypervisor backend")
	}
	ctx := context.Background()

	// Ensure socket doesn't exist
	os.Remove(cfg.SocketPath)

	// Validate paths
	if _, err := os.Stat(cfg.KernelPath); err != nil {
		return 0, fmt.Errorf("kernel not found at %s: %w", cfg.KernelPath, err)
	}
	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return 0, fmt.Errorf("rootfs not found at %s: %w", cfg.RootfsPath, err)
	}

	// Default kernel args for a basic Linux boot
	kernelArgs := cfg.KernelArgs
	if kernelArgs == "" {
		kernelArgs = "console=ttyS0 reboot=k panic=1 pci=off"
	}

	kernelArgs += cfg.IPBootArg()

	// Build drives list starting with rootfs
	drives := []models.Drive{
		{
			DriveID:      sdk.String("rootfs"),
			PathOnHost:   sdk.String(cfg.RootfsPath),
			IsRootDevice: sdk.Bool(true),
			IsReadOnly:   sdk.Bool(false),
		},
	}

	// Add mount drives (vdb, vdc, etc.)
	for i, mountDrive := range cfg.MountDrives {
		driveID := fmt.Sprintf("mount%d", i)
		drives = append(drives, models.Drive{
			DriveID:      sdk.String(driveID),
			PathOnHost:   sdk.String(mountDrive.ImagePath),
			IsRootDevice: sdk.Bool(false),
			IsReadOnly:   sdk.Bool(mountDrive.ReadOnly),
		})
	}

	// Build Firecracker configuration
	fcCfg := sdk.Config{
		SocketPath:      cfg.SocketPath,
		KernelImagePath: cfg.KernelPath,
		KernelArgs:      kernelArgs,
		Drives:          drives,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  sdk.Int64(int64(cfg.CPUs)),
			MemSizeMib: sdk.Int64(int64(cfg.MemoryMB)),
		},
	}

	// Add network interface if configured
	if cfg.TapDevice != "" {
		fcCfg.NetworkInterfaces = []sdk.NetworkInterface{
			{
				StaticConfiguration: &sdk.StaticNetworkConfiguration{
					HostDevName: cfg.TapDevice,
					MacAddress:  cfg.MacAddress,
				},
			},
		}
	}

	// Find Firecracker binary
	fcBin := c.FirecrackerBin
	if _, err := os.Stat(fcBin); err != nil {
		// Try to find it in PATH
		if path, err := exec.LookPath("firecracker"); err == nil {
			fcBin = path
		} else {
			return 0, fmt.Errorf("firecracker binary not found at %s or in PATH", c.FirecrackerBin)
		}
	}

	// Set up machine options
	machineOpts := []sdk.Opt{
		sdk.WithLogger(logrus.NewEntry(c.Logger)),
	}

	// Create log directory and console log file
	if cfg.LogPath != "" {
		logDir := filepath.Dir(cfg.LogPath)
		if err := os.MkdirAll(logDir, 0700); err != nil {
			return 0, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// Build Firecracker command, capturing serial console output to a log file
	cmdBuilder := sdk.VMCommandBuilder{}.
		WithBin(fcBin).
		WithSocketPath(cfg.SocketPath)

	if cfg.LogPath != "" {
		consolePath := vm.ConsoleLogPath(cfg.LogPath)
		consoleFile, err := os.OpenFile(consolePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return 0, fmt.Errorf("failed to create console log file: %w", err)
		}
		cmdBuilder = cmdBuilder.WithStdout(consoleFile).WithStderr(consoleFile)
	}

	cmd := cmdBuilder.Build(ctx)

	machineOpts = append(machineOpts, sdk.WithProcessRunner(cmd))

	// Create the machine
	machine, err := sdk.NewMachine(ctx, fcCfg, machineOpts...)
	if err != nil {
		return 0, fmt.Errorf("failed to create Firecracker machine: %w", err)
	}

	// Start the machine
	if err := machine.Start(ctx); err != nil {
		return 0, fmt.Errorf("failed to start Firecracker machine: %w", err)
	}

	return pidOf(machine), nil
}

// Version returns the Firecracker binary version string (e.g. "v1.16.0"), or an
// empty string if it cannot be determined. Snapshot memory/state files are tied
// to the Firecracker version, so this is recorded with each snapshot.
func (c *Client) Version() string {
	fcBin := c.FirecrackerBin
	if _, err := os.Stat(fcBin); err != nil {
		if path, err := exec.LookPath("firecracker"); err == nil {
			fcBin = path
		} else {
			return ""
		}
	}
	out, err := exec.Command(fcBin, "--version").Output()
	if err != nil {
		return ""
	}
	// Output looks like: "Firecracker v1.16.0\n..."
	firstLine := strings.SplitN(string(out), "\n", 2)[0]
	fields := strings.Fields(firstLine)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// PauseVM pauses a running Firecracker VM over its API socket. The VM must be
// paused before a snapshot can be created.
func (c *Client) PauseVM(ctx context.Context, socketPath string) error {
	machine, err := c.connectToMachine(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	if err := machine.PauseVM(ctx); err != nil {
		return fmt.Errorf("failed to pause VM: %w", err)
	}
	return nil
}

// ResumeVM resumes a paused Firecracker VM over its API socket.
func (c *Client) ResumeVM(ctx context.Context, socketPath string) error {
	machine, err := c.connectToMachine(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	if err := machine.ResumeVM(ctx); err != nil {
		return fmt.Errorf("failed to resume VM: %w", err)
	}
	return nil
}

// CreateSnapshotFiles writes a full snapshot (guest memory + device/vcpu state)
// of a paused VM to memPath and statePath. The VM must already be paused; the
// Firecracker process writes both files itself, so their parent directory must
// exist and be writable by that process.
func (c *Client) CreateSnapshotFiles(ctx context.Context, socketPath, memPath, statePath string) error {
	machine, err := c.connectToMachine(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	if err := machine.CreateSnapshot(ctx, memPath, statePath); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}
	return nil
}

// RestoreVM starts a fresh Firecracker process that loads a previously created
// snapshot (memPath + statePath) and resumes the guest. The block devices and
// TAP network device referenced by the snapshot state must already exist at the
// same host paths / names they had when the snapshot was taken.
func (c *Client) RestoreVM(ctx context.Context, socketPath, logPath, memPath, statePath string) (int, error) {
	// LoadSnapshot validation requires the socket to be absent and both
	// snapshot files to exist.
	os.Remove(socketPath)
	if _, err := os.Stat(memPath); err != nil {
		return 0, fmt.Errorf("snapshot memory file not found at %s: %w", memPath, err)
	}
	if _, err := os.Stat(statePath); err != nil {
		return 0, fmt.Errorf("snapshot state file not found at %s: %w", statePath, err)
	}

	fcBin := c.FirecrackerBin
	if _, err := os.Stat(fcBin); err != nil {
		if path, err := exec.LookPath("firecracker"); err == nil {
			fcBin = path
		} else {
			return 0, fmt.Errorf("firecracker binary not found at %s or in PATH", c.FirecrackerBin)
		}
	}

	machineOpts := []sdk.Opt{
		sdk.WithLogger(logrus.NewEntry(c.Logger)),
	}

	cmdBuilder := sdk.VMCommandBuilder{}.
		WithBin(fcBin).
		WithSocketPath(socketPath)

	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
			return 0, fmt.Errorf("failed to create log directory: %w", err)
		}
		consolePath := vm.ConsoleLogPath(logPath)
		consoleFile, err := os.OpenFile(consolePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return 0, fmt.Errorf("failed to create console log file: %w", err)
		}
		cmdBuilder = cmdBuilder.WithStdout(consoleFile).WithStderr(consoleFile)
	}

	cmd := cmdBuilder.Build(ctx)
	machineOpts = append(machineOpts,
		sdk.WithProcessRunner(cmd),
		sdk.WithSnapshot(memPath, statePath, func(cfg *sdk.SnapshotConfig) {
			cfg.ResumeVM = true
		}),
	)

	fcCfg := sdk.Config{SocketPath: socketPath}

	machine, err := sdk.NewMachine(ctx, fcCfg, machineOpts...)
	if err != nil {
		return 0, fmt.Errorf("failed to create Firecracker machine: %w", err)
	}
	if err := machine.Start(ctx); err != nil {
		return 0, fmt.Errorf("failed to restore VM from snapshot: %w", err)
	}
	return pidOf(machine), nil
}

// StopVM gracefully stops a running Firecracker VM
func (c *Client) StopVM(ctx context.Context, socketPath string) error {
	// Connect to existing machine
	machine, err := c.connectToMachine(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}

	// Try graceful shutdown first
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := machine.Shutdown(shutdownCtx); err != nil {
		c.Logger.Warnf("Graceful shutdown failed, forcing stop: %v", err)
		// Force stop
		if err := machine.StopVMM(); err != nil {
			return fmt.Errorf("failed to stop VMM: %w", err)
		}
	}

	return nil
}

// proc matches Firecracker processes to the VMs they serve.
const proc = vmproc.Binary("firecracker")

// Terminate stops the Firecracker process backing a VM and does not return
// until the process is gone. It first asks the guest to shut down
// (Ctrl+Alt+Del over the API socket, which only works when the guest kernel
// has the i8042 driver), then escalates to signals. See vmproc.Terminate.
func (c *Client) Terminate(ctx context.Context, v *vm.VM) error {
	return proc.Terminate(ctx, v, func(ctx context.Context) error {
		return c.StopVM(ctx, v.SocketPath)
	})
}

// connectToMachine connects to an existing Firecracker instance
func (c *Client) connectToMachine(ctx context.Context, socketPath string) (*sdk.Machine, error) {
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("socket not found: %w", err)
	}

	// Minimal config just for connecting
	fcCfg := sdk.Config{
		SocketPath: socketPath,
	}

	machine, err := sdk.NewMachine(ctx, fcCfg,
		sdk.WithLogger(logrus.NewEntry(c.Logger)),
	)
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// pidOf returns the Firecracker process's PID, or 0 if it isn't known.
func pidOf(machine *sdk.Machine) int {
	if machine == nil {
		return 0
	}
	pid, _ := machine.PID()
	return pid
}

// UpdateVMState sets the VM's state and PID from its Firecracker process.
func (c *Client) UpdateVMState(v *vm.VM) {
	proc.UpdateVMState(v)
}

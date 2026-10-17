// Package cloudhypervisor runs VMs under Cloud Hypervisor. Unlike Firecracker
// it can expose hardware virtualization to the guest, so it is the backend
// for nested VMs. It boots the same kernels and rootfs images: they have PVH
// boot and virtio-pci, and drives come up as /dev/vda, vdb, ... in the order
// they are attached.
package cloudhypervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/raesene/baremetalvmm/internal/vm"
	"github.com/raesene/baremetalvmm/internal/vmproc"
)

const (
	// DefaultBin is where the installer puts the cloud-hypervisor binary.
	DefaultBin = "/usr/local/bin/cloud-hypervisor"
	// bootTimeout is how long Start waits for the VM to report it is running.
	bootTimeout = 10 * time.Second
)

// proc matches Cloud Hypervisor processes to the VMs they serve.
const proc = vmproc.Binary("cloud-hypervisor")

// sysModule is where the KVM modules' parameters live; tests point it
// elsewhere.
var sysModule = "/sys/module"

// Client starts and stops Cloud Hypervisor VMs.
type Client struct {
	Bin string
}

// NewClient returns a client for the installed cloud-hypervisor binary.
func NewClient() *Client {
	return &Client{Bin: DefaultBin}
}

// Name identifies this backend in VM records.
func (c *Client) Name() string {
	return "cloud-hypervisor"
}

// Binary returns the path of the cloud-hypervisor executable, falling back
// to PATH when it isn't at c.Bin.
func (c *Client) Binary() (string, error) {
	if _, err := os.Stat(c.Bin); err == nil {
		return c.Bin, nil
	}
	if path, err := exec.LookPath("cloud-hypervisor"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("cloud-hypervisor not found at %s or in PATH; install it from https://github.com/cloud-hypervisor/cloud-hypervisor/releases", c.Bin)
}

// HostNested returns an error unless the host's KVM module lets guests run
// their own VMs.
func HostNested() error {
	for _, mod := range []string{"kvm_intel", "kvm_amd"} {
		data, err := os.ReadFile(filepath.Join(sysModule, mod, "parameters", "nested"))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(data)) {
		case "Y", "y", "1":
			return nil
		}
		return fmt.Errorf("nested virtualization is disabled on this host; enable it with \"echo 'options %s nested=1' | sudo tee /etc/modprobe.d/kvm-nested.conf\", then reload %s or reboot", mod, mod)
	}
	return errors.New("nested virtualization needs the kvm_intel or kvm_amd module, and neither is loaded")
}

// Args builds the cloud-hypervisor command line for a VM. The API socket is
// passed as a bare path so vmproc can match the process to the VM.
func Args(cfg *vm.LaunchConfig) []string {
	kernelArgs := cfg.KernelArgs
	if kernelArgs == "" {
		kernelArgs = "console=ttyS0 reboot=k panic=1"
	}
	kernelArgs += " root=/dev/vda rw" + cfg.IPBootArg()

	disks := []string{"path=" + cfg.RootfsPath + ",image_type=raw"}
	for _, d := range cfg.MountDrives {
		disk := "path=" + d.ImagePath + ",image_type=raw"
		if d.ReadOnly {
			disk += ",readonly=on"
		}
		disks = append(disks, disk)
	}

	cpus := fmt.Sprintf("boot=%d", cfg.CPUs)
	if cfg.Nested {
		cpus += ",nested=on"
	}

	args := []string{
		"--api-socket", cfg.SocketPath,
		"--kernel", cfg.KernelPath,
		"--cmdline", kernelArgs,
		"--cpus", cpus,
		"--memory", fmt.Sprintf("size=%dM", cfg.MemoryMB),
		"--disk",
	}
	args = append(args, disks...)
	if cfg.TapDevice != "" {
		args = append(args, "--net", "tap="+cfg.TapDevice+",mac="+cfg.MacAddress)
	}
	if cfg.LogPath != "" {
		args = append(args, "--serial", "file="+vm.ConsoleLogPath(cfg.LogPath))
	} else {
		args = append(args, "--serial", "null")
	}
	return append(args, "--console", "off")
}

// Start boots a VM and returns the cloud-hypervisor process's PID once the
// VM reports it is running. The process runs in its own session, so it
// outlives the command or HTTP request that started it. Its own output goes
// to cfg.LogPath; the guest's serial console goes to the console log.
func (c *Client) Start(cfg *vm.LaunchConfig) (int, error) {
	bin, err := c.Binary()
	if err != nil {
		return 0, err
	}
	if cfg.Nested {
		if err := HostNested(); err != nil {
			return 0, err
		}
	}
	if _, err := os.Stat(cfg.KernelPath); err != nil {
		return 0, fmt.Errorf("kernel not found at %s: %w", cfg.KernelPath, err)
	}
	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return 0, fmt.Errorf("rootfs not found at %s: %w", cfg.RootfsPath, err)
	}
	os.Remove(cfg.SocketPath)

	cmd := exec.Command(bin, Args(cfg)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cfg.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0700); err != nil {
			return 0, fmt.Errorf("failed to create log directory: %w", err)
		}
		logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return 0, fmt.Errorf("failed to create log file: %w", err)
		}
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start cloud-hypervisor: %w", err)
	}

	// Reap the process when it exits, so a long-running caller (vmm-web)
	// doesn't collect zombies, and so a failed boot is noticed at once.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	if err := waitRunning(cfg.SocketPath, exited); err != nil {
		_ = cmd.Process.Kill()
		return 0, fmt.Errorf("%w%s", err, logTail(cfg.LogPath))
	}
	return cmd.Process.Pid, nil
}

// waitRunning polls the API until the VM is running or the process exits.
func waitRunning(socketPath string, exited <-chan error) error {
	deadline := time.Now().Add(bootTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if err == nil {
				return errors.New("cloud-hypervisor exited while starting the VM")
			}
			return fmt.Errorf("cloud-hypervisor exited while starting the VM: %w", err)
		case <-time.After(50 * time.Millisecond):
		}
		if state, err := vmState(socketPath); err == nil && state == "Running" {
			return nil
		}
	}
	return fmt.Errorf("VM did not report running within %s", bootTimeout)
}

// logTail returns the last lines of a log file, formatted to follow an
// error message, or "" if there's nothing to show.
func logTail(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > 5 {
			lines = lines[1:]
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n  " + strings.Join(lines, "\n  ")
}

// api returns an HTTP client that talks to a VM's API socket.
func api(socketPath string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

// vmState returns the VM's state as reported by the API ("Running", ...).
func vmState(socketPath string) (string, error) {
	resp, err := api(socketPath).Get("http://localhost/api/v1/vm.info")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vm.info: %s", resp.Status)
	}
	var info struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("vm.info: %w", err)
	}
	return info.State, nil
}

// powerButton presses the guest's ACPI power button. The guest only shuts
// down if its kernel has the ACPI button and evdev drivers.
func powerButton(ctx context.Context, socketPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://localhost/api/v1/vm.power-button", nil)
	if err != nil {
		return err
	}
	resp, err := api(socketPath).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vm.power-button: %s", resp.Status)
	}
	return nil
}

// Terminate stops the VM's cloud-hypervisor process and does not return
// until it is gone: ACPI power button first, then signals. See
// vmproc.Terminate.
func (c *Client) Terminate(ctx context.Context, v *vm.VM) error {
	return proc.Terminate(ctx, v, func(ctx context.Context) error {
		return powerButton(ctx, v.SocketPath)
	})
}

// UpdateVMState sets the VM's state and PID from its cloud-hypervisor process.
func (c *Client) UpdateVMState(v *vm.VM) {
	proc.UpdateVMState(v)
}

// Version returns the cloud-hypervisor version (e.g. "v54.0"), or "" if it
// can't be determined.
func (c *Client) Version() string {
	bin, err := c.Binary()
	if err != nil {
		return ""
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	// Output looks like: "cloud-hypervisor v54.0"
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

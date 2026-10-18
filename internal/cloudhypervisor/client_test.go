package cloudhypervisor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/raesene/baremetalvmm/internal/vm"
)

// flagValues returns the values following flag, up to the next --flag.
func flagValues(args []string, flag string) []string {
	i := slices.Index(args, flag)
	if i < 0 {
		return nil
	}
	var vals []string
	for _, a := range args[i+1:] {
		if strings.HasPrefix(a, "--") {
			break
		}
		vals = append(vals, a)
	}
	return vals
}

func TestArgs(t *testing.T) {
	cfg := &vm.LaunchConfig{
		SocketPath: "/var/lib/vmm/sockets/a.sock",
		KernelPath: "/k/vmlinux",
		RootfsPath: "/v/a.ext4",
		CPUs:       2,
		MemoryMB:   1024,
		TapDevice:  "vmm-abc",
		MacAddress: "AA:FC:00:01:02:03",
		LogPath:    "/var/lib/vmm/logs/a.log",
		IPAddress:  "172.16.0.9",
		Gateway:    "172.16.0.1",
		Subnet:     "172.16.0.0/16",
		MountDrives: []vm.MountDrive{
			{ImagePath: "/m/rw.ext4"},
			{ImagePath: "/m/ro.ext4", ReadOnly: true},
		},
		Nested:          true,
		ExtraKernelArgs: "apparmor=0",
	}
	args := Args(cfg)

	checks := map[string][]string{
		// A bare socket path is what vmproc matches the process on.
		"--api-socket": {"/var/lib/vmm/sockets/a.sock"},
		"--kernel":     {"/k/vmlinux"},
		"--cmdline":    {"console=ttyS0 reboot=k panic=1 root=/dev/vda rw ip=172.16.0.9::172.16.0.1:255.255.0.0::eth0:off apparmor=0"},
		"--cpus":       {"boot=2,nested=on"},
		"--memory":     {"size=1024M"},
		// Rootfs first, then mounts in order, so they are vda, vdb, vdc.
		"--disk": {
			"path=/v/a.ext4,image_type=raw",
			"path=/m/rw.ext4,image_type=raw",
			"path=/m/ro.ext4,image_type=raw,readonly=on",
		},
		"--net":     {"tap=vmm-abc,mac=AA:FC:00:01:02:03"},
		"--serial":  {"file=/var/lib/vmm/logs/a-console.log"},
		"--console": {"off"},
	}
	for flag, want := range checks {
		if got := flagValues(args, flag); !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}

	cfg.Nested = false
	if got := flagValues(Args(cfg), "--cpus"); !slices.Equal(got, []string{"boot=2"}) {
		t.Errorf("--cpus without nesting = %q", got)
	}
}

func TestHostNested(t *testing.T) {
	tests := []struct {
		name   string
		module string // module dir to create, "" for none
		value  string
		ok     bool
	}{
		{"intel enabled", "kvm_intel", "Y\n", true},
		{"amd enabled", "kvm_amd", "1\n", true},
		{"intel disabled", "kvm_intel", "N\n", false},
		{"amd disabled", "kvm_amd", "0\n", false},
		{"no kvm module", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.module != "" {
				params := filepath.Join(dir, tt.module, "parameters")
				if err := os.MkdirAll(params, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(params, "nested"), []byte(tt.value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			old := sysModule
			sysModule = dir
			defer func() { sysModule = old }()

			if err := HostNested(); (err == nil) != tt.ok {
				t.Errorf("HostNested() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	var lines []string
	for i := 1; i <= 8; i++ {
		lines = append(lines, "line"+string(rune('0'+i)))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := logTail(path)
	if strings.Contains(got, "line3") || !strings.Contains(got, "line4") || !strings.HasSuffix(got, "line8") {
		t.Errorf("logTail = %q, want the last five lines", got)
	}
	if logTail("") != "" || logTail(filepath.Join(t.TempDir(), "missing")) != "" {
		t.Error("logTail of no file should be empty")
	}
}

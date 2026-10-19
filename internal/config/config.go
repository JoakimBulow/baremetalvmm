package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultDataDir    = "/var/lib/vmm"
	DefaultBridgeName = "vmm-br0"
	DefaultSubnet     = "172.16.0.0/16"
	DefaultGateway    = "172.16.0.1"
)

// VMDefaults holds default values for VM creation
type VMDefaults struct {
	CPUs       int      `json:"cpus,omitempty"`
	MemoryMB   int      `json:"memory_mb,omitempty"`
	DiskSizeMB int      `json:"disk_size_mb,omitempty"`
	Image      string   `json:"image,omitempty"`
	Kernel     string   `json:"kernel,omitempty"`
	SSHKeyPath string   `json:"ssh_key_path,omitempty"`
	DNSServers []string `json:"dns_servers,omitempty"`
}

// Config holds the global VMM configuration
type Config struct {
	DataDir       string      `json:"data_dir"`
	BridgeName    string      `json:"bridge_name"`
	Subnet        string      `json:"subnet"`
	Gateway       string      `json:"gateway"`
	HostInterface string      `json:"host_interface"`
	KernelPath    string      `json:"kernel_path"`
	RootfsPath    string      `json:"rootfs_path"`
	VMDefaults    *VMDefaults `json:"vm_defaults,omitempty"`
}

// GetVMDefaults returns the VM defaults, or an empty struct if none configured
func (c *Config) GetVMDefaults() VMDefaults {
	if c.VMDefaults == nil {
		return VMDefaults{}
	}
	return *c.VMDefaults
}

// Paths returns commonly used paths derived from the config
type Paths struct {
	Config    string
	VMs       string
	Images    string
	Kernels   string
	Rootfs    string
	Sockets   string
	Logs      string
	State     string
	Mounts    string
	Clusters  string
	SSH       string
	Snapshots string
}

// detectDefaultInterface finds the network interface used for the default route
// by reading /proc/net/route. Returns "eth0" as fallback if detection fails.
func detectDefaultInterface() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "eth0"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip header line
	scanner.Scan()

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			// Default route has destination 00000000
			if fields[1] == "00000000" {
				return fields[0]
			}
		}
	}

	return "eth0"
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		DataDir:       DefaultDataDir,
		BridgeName:    DefaultBridgeName,
		Subnet:        DefaultSubnet,
		Gateway:       DefaultGateway,
		HostInterface: detectDefaultInterface(),
	}
}

// ValidateDataDir checks that a data directory path is usable: it must be a
// non-empty absolute path. This is a filesystem path rather than an identifier,
// so the identifier name pattern in internal/validate does not apply.
func ValidateDataDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("data directory cannot be empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("data directory must be an absolute path: %q", path)
	}
	return nil
}

// GetPaths returns all standard paths based on the data directory
func (c *Config) GetPaths() *Paths {
	return &Paths{
		Config:    filepath.Join(c.DataDir, "config"),
		VMs:       filepath.Join(c.DataDir, "vms"),
		Images:    filepath.Join(c.DataDir, "images"),
		Kernels:   filepath.Join(c.DataDir, "images", "kernels"),
		Rootfs:    filepath.Join(c.DataDir, "images", "rootfs"),
		Sockets:   filepath.Join(c.DataDir, "sockets"),
		Logs:      filepath.Join(c.DataDir, "logs"),
		State:     filepath.Join(c.DataDir, "state"),
		Mounts:    filepath.Join(c.DataDir, "mounts"),
		Clusters:  filepath.Join(c.DataDir, "clusters"),
		SSH:       filepath.Join(c.DataDir, "ssh"),
		Snapshots: filepath.Join(c.DataDir, "snapshots"),
	}
}

// EnsureDirectories creates all necessary directories
func (c *Config) EnsureDirectories() error {
	paths := c.GetPaths()
	dirs := []string{
		paths.Config,
		paths.VMs,
		paths.Kernels,
		paths.Rootfs,
		paths.Sockets,
		paths.Logs,
		paths.State,
		paths.Mounts,
		paths.Clusters,
		paths.SSH,
		paths.Snapshots,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

// Load reads the configuration from disk
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the configuration to disk
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// SystemConfigPath is the config file used by everything that runs as root:
// sudo, vmm.service and vmm-web.service. Services have no SUDO_USER, so a
// per-user config is invisible to them and they would silently boot VMs with
// the default network settings.
var SystemConfigPath = "/etc/vmm/config.json"

// Replaced in tests.
var (
	getuid   = os.Getuid
	homeRoot = "/home"
)

// ConfigPath returns the config file path. Root always uses SystemConfigPath.
// Other users read it too once it exists, so read-only commands such as
// `vmm list` agree with the services; without it they fall back to the
// per-user config.
func ConfigPath() string {
	if getuid() == 0 {
		return SystemConfigPath
	}
	if _, err := os.Stat(SystemConfigPath); err == nil {
		return SystemConfigPath
	}
	return userConfigPath()
}

// userConfigPath is the per-user config location used before SystemConfigPath.
func userConfigPath() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "vmm", "config.json")
	}

	// When running under sudo, use the original user's home directory
	home, _ := os.UserHomeDir()
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		home = filepath.Join(homeRoot, sudoUser)
	}

	return filepath.Join(home, ".config", "vmm", "config.json")
}

// MigrateSystemConfig copies a per-user config to SystemConfigPath the first
// time vmm runs as root without one, and returns the file it copied ("" if
// nothing was done). It looks at the sudo user's config, then root's, then a
// config under /home when exactly one user has one: vmm.service runs without
// SUDO_USER and may be the first new binary to run after `vmm upgrade`.
func MigrateSystemConfig() (string, error) {
	if getuid() != 0 {
		return "", nil
	}
	if _, err := os.Stat(SystemConfigPath); !os.IsNotExist(err) {
		return "", err
	}

	src, err := legacyConfigPath()
	if src == "" || err != nil {
		return "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, DefaultConfig()); err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}

	dir := filepath.Dir(SystemConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), SystemConfigPath); err != nil {
		return "", err
	}
	return src, nil
}

// legacyConfigPath finds the per-user config MigrateSystemConfig copies.
func legacyConfigPath() (string, error) {
	candidates := []string{userConfigPath()}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "vmm", "config.json"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	matches, _ := filepath.Glob(filepath.Join(homeRoot, "*", ".config", "vmm", "config.json"))
	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("found several user configs (%s); copy the right one to %s",
		strings.Join(matches, ", "), SystemConfigPath)
}

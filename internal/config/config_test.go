package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDataDir(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"absolute", "/var/lib/vmm", false},
		{"absolute custom", "/srv/vmm-data", false},
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"relative", "vmm", true},
		{"relative dot", "./vmm", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDataDir(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDataDir(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestGetPathsDerivedFromDataDir(t *testing.T) {
	dataDir := "/custom/data/root"
	cfg := &Config{DataDir: dataDir}
	paths := cfg.GetPaths()

	want := map[string]string{
		"Config":    filepath.Join(dataDir, "config"),
		"VMs":       filepath.Join(dataDir, "vms"),
		"Images":    filepath.Join(dataDir, "images"),
		"Kernels":   filepath.Join(dataDir, "images", "kernels"),
		"Rootfs":    filepath.Join(dataDir, "images", "rootfs"),
		"Sockets":   filepath.Join(dataDir, "sockets"),
		"Logs":      filepath.Join(dataDir, "logs"),
		"State":     filepath.Join(dataDir, "state"),
		"Mounts":    filepath.Join(dataDir, "mounts"),
		"Clusters":  filepath.Join(dataDir, "clusters"),
		"SSH":       filepath.Join(dataDir, "ssh"),
		"Snapshots": filepath.Join(dataDir, "snapshots"),
	}

	got := map[string]string{
		"Config":    paths.Config,
		"VMs":       paths.VMs,
		"Images":    paths.Images,
		"Kernels":   paths.Kernels,
		"Rootfs":    paths.Rootfs,
		"Sockets":   paths.Sockets,
		"Logs":      paths.Logs,
		"State":     paths.State,
		"Mounts":    paths.Mounts,
		"Clusters":  paths.Clusters,
		"SSH":       paths.SSH,
		"Snapshots": paths.Snapshots,
	}

	for k, wantPath := range want {
		if got[k] != wantPath {
			t.Errorf("GetPaths().%s = %q, want %q", k, got[k], wantPath)
		}
	}
}

func TestEnsureDirectoriesCustomDataDir(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "vmm-data")
	cfg := &Config{DataDir: dataDir}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	paths := cfg.GetPaths()
	for _, dir := range []string{paths.VMs, paths.Kernels, paths.Rootfs, paths.Snapshots} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", dir)
		}
	}
}

// fakeSystemConfig points the system and user config locations at temp dirs
// and makes the process look like root (or not).
func fakeSystemConfig(t *testing.T, root bool) (system, home string) {
	t.Helper()
	dir := t.TempDir()
	system = filepath.Join(dir, "etc", "vmm", "config.json")
	homeDir := filepath.Join(dir, "home")
	oldSystem, oldUID, oldHome := SystemConfigPath, getuid, homeRoot
	SystemConfigPath, homeRoot = system, homeDir
	getuid = func() int {
		if root {
			return 0
		}
		return 1000
	}
	t.Cleanup(func() { SystemConfigPath, getuid, homeRoot = oldSystem, oldUID, oldHome })
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("HOME", filepath.Join(dir, "root"))
	return system, homeDir
}

func writeUserConfig(t *testing.T, home, user, subnet string) string {
	t.Helper()
	p := filepath.Join(home, user, ".config", "vmm", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"subnet": "`+subnet+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigPath(t *testing.T) {
	t.Run("root always uses system config", func(t *testing.T) {
		system, home := fakeSystemConfig(t, true)
		writeUserConfig(t, home, "alice", "10.99.0.0/16")
		t.Setenv("SUDO_USER", "alice")
		if got := ConfigPath(); got != system {
			t.Errorf("ConfigPath() = %q, want %q", got, system)
		}
	})
	t.Run("user without system config", func(t *testing.T) {
		fakeSystemConfig(t, false)
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		if got, want := ConfigPath(), "/xdg/vmm/config.json"; got != want {
			t.Errorf("ConfigPath() = %q, want %q", got, want)
		}
	})
	t.Run("user reads existing system config", func(t *testing.T) {
		system, _ := fakeSystemConfig(t, false)
		if err := os.MkdirAll(filepath.Dir(system), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(system, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := ConfigPath(); got != system {
			t.Errorf("ConfigPath() = %q, want %q", got, system)
		}
	})
}

func TestMigrateSystemConfig(t *testing.T) {
	t.Run("copies sudo user's config", func(t *testing.T) {
		system, home := fakeSystemConfig(t, true)
		src := writeUserConfig(t, home, "alice", "10.99.0.0/16")
		writeUserConfig(t, home, "bob", "10.50.0.0/16")
		t.Setenv("SUDO_USER", "alice")

		got, err := MigrateSystemConfig()
		if err != nil || got != src {
			t.Fatalf("MigrateSystemConfig() = %q, %v; want %q", got, err, src)
		}
		cfg, err := Load(system)
		if err != nil || cfg.Subnet != "10.99.0.0/16" {
			t.Fatalf("migrated config subnet = %v (err %v)", cfg, err)
		}
		if fi, _ := os.Stat(system); fi.Mode().Perm() != 0644 {
			t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
		}

		// Second run leaves the system config alone.
		writeUserConfig(t, home, "alice", "10.1.0.0/16")
		if got, err := MigrateSystemConfig(); err != nil || got != "" {
			t.Errorf("second run = %q, %v; want no-op", got, err)
		}
	})
	t.Run("service picks the only user config", func(t *testing.T) {
		_, home := fakeSystemConfig(t, true)
		src := writeUserConfig(t, home, "alice", "10.99.0.0/16")
		if got, err := MigrateSystemConfig(); err != nil || got != src {
			t.Fatalf("MigrateSystemConfig() = %q, %v; want %q", got, err, src)
		}
	})
	t.Run("service refuses to guess between users", func(t *testing.T) {
		system, home := fakeSystemConfig(t, true)
		writeUserConfig(t, home, "alice", "10.99.0.0/16")
		writeUserConfig(t, home, "bob", "10.50.0.0/16")
		if _, err := MigrateSystemConfig(); err == nil {
			t.Fatal("want error with several user configs")
		}
		if _, err := os.Stat(system); !os.IsNotExist(err) {
			t.Errorf("system config created: %v", err)
		}
	})
	t.Run("nothing to migrate", func(t *testing.T) {
		system, _ := fakeSystemConfig(t, true)
		if got, err := MigrateSystemConfig(); err != nil || got != "" {
			t.Fatalf("MigrateSystemConfig() = %q, %v", got, err)
		}
		if _, err := os.Stat(system); !os.IsNotExist(err) {
			t.Errorf("system config created: %v", err)
		}
	})
	t.Run("not root", func(t *testing.T) {
		_, home := fakeSystemConfig(t, false)
		writeUserConfig(t, home, "alice", "10.99.0.0/16")
		if got, err := MigrateSystemConfig(); err != nil || got != "" {
			t.Fatalf("MigrateSystemConfig() = %q, %v", got, err)
		}
	})
}

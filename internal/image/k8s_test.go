package image

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestK8sImageVersion(t *testing.T) {
	for name, want := range map[string]string{
		"k8s-1.36.5":  "1.36.5",
		"k8s-1.36":    "",
		"k8s-1.36.05": "",
		"k8s-kernel":  "",
		"rootfs":      "",
		"k8s-1.36.5x": "",
	} {
		if got := K8sImageVersion(name); got != want {
			t.Errorf("K8sImageVersion(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNewestPatch(t *testing.T) {
	versions := []string{"1.36.2", "1.36.10", "1.37.1", "1.36.9", "1.35.20"}
	if got := newestPatch(versions, []int{1, 36}); got != "1.36.10" {
		t.Errorf("newestPatch = %q, want 1.36.10", got)
	}
	if got := newestPatch(versions, []int{1, 34}); got != "" {
		t.Errorf("newestPatch for missing minor = %q, want empty", got)
	}
}

// k8sTestManager returns a Manager whose rootfs dir holds the given k8s images.
func k8sTestManager(t *testing.T, versions ...string) *Manager {
	t.Helper()
	dir := t.TempDir()
	for _, v := range versions {
		if err := os.WriteFile(filepath.Join(dir, "k8s-"+v+".ext4"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return &Manager{RootfsDir: dir}
}

func fakeReleases(t *testing.T, tags []string, err error) {
	t.Helper()
	orig := listReleases
	t.Cleanup(func() { listReleases = orig })
	listReleases = func() ([]ghRelease, error) {
		var rels []ghRelease
		for _, tag := range tags {
			rels = append(rels, ghRelease{TagName: tag, Assets: []ghAsset{
				{Name: "k8s-rootfs.ext4.gz", BrowserDownloadURL: "https://example.invalid/" + tag},
			}})
		}
		return rels, err
	}
}

func TestResolveK8sImage(t *testing.T) {
	t.Run("exact version never falls back to another patch", func(t *testing.T) {
		// The bug: 1.36.5 resolved to a local 1.36.0 image.
		m := k8sTestManager(t, "1.36.0")
		fakeReleases(t, nil, nil)
		name, v, err := m.ResolveK8sImage("1.36.5")
		if err != nil || name != "" || v != "1.36.5" {
			t.Errorf("got (%q, %q, %v), want no image and 1.36.5", name, v, err)
		}
	})

	t.Run("exact version local", func(t *testing.T) {
		m := k8sTestManager(t, "1.36.0", "1.36.5")
		fakeReleases(t, nil, errors.New("must not be called"))
		name, v, err := m.ResolveK8sImage("1.36.5")
		if err != nil || name != "k8s-1.36.5" || v != "1.36.5" {
			t.Errorf("got (%q, %q, %v)", name, v, err)
		}
	})

	t.Run("minor takes newest local when nothing newer is released", func(t *testing.T) {
		m := k8sTestManager(t, "1.36.0", "1.36.5", "1.37.1")
		fakeReleases(t, []string{"k8s-rootfs-1.36.4", "k8s-rootfs-1.37.1"}, nil)
		name, v, err := m.ResolveK8sImage("1.36")
		if err != nil || name != "k8s-1.36.5" || v != "1.36.5" {
			t.Errorf("got (%q, %q, %v)", name, v, err)
		}
	})

	t.Run("minor uses local when GitHub is unreachable", func(t *testing.T) {
		m := k8sTestManager(t, "1.36.2")
		fakeReleases(t, nil, errors.New("offline"))
		name, v, err := m.ResolveK8sImage("1.36")
		if err != nil || name != "k8s-1.36.2" || v != "1.36.2" {
			t.Errorf("got (%q, %q, %v)", name, v, err)
		}
	})

	t.Run("minor with nothing available", func(t *testing.T) {
		m := k8sTestManager(t, "1.35.9")
		fakeReleases(t, []string{"k8s-rootfs-1.35.9"}, nil)
		if _, _, err := m.ResolveK8sImage("1.36"); err == nil {
			t.Error("expected an error")
		}
	})
}

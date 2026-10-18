package image

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUpdates(t *testing.T) {
	m := &Manager{KernelDir: t.TempDir(), RootfsDir: t.TempDir()}
	write := func(path string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

	write(filepath.Join(m.KernelDir, "k8s-kernel"), day(17))    // before the r3 release
	write(filepath.Join(m.KernelDir, "nested-kernel"), day(19)) // after it
	write(m.GetImagePath("k8s-1.36.2"), day(1))
	write(m.GetImagePath("k8s-1.35.9"), day(10)) // before its asset was replaced

	asset := func(name string, d int) []ghAsset {
		return []ghAsset{{Name: name, BrowserDownloadURL: "https://example.invalid/" + name, UpdatedAt: day(d)}}
	}
	releases := m.availableReleases([]ghRelease{
		{TagName: "k8s-kernel-6.6.159-r3", Assets: asset("k8s-vmlinux.bin", 18)},
		{TagName: "nested-kernel-6.1.190-r3", Assets: asset("nested-vmlinux.bin", 18)},
		{TagName: "security-kernel-6.12.113-r3", Assets: asset("security-vmlinux.bin", 18)},
		{TagName: "k8s-rootfs-1.36.5", Assets: asset("k8s-rootfs.ext4.gz", 18)},
		{TagName: "k8s-rootfs-1.35.9", Assets: asset("k8s-rootfs.ext4.gz", 18)},
		{TagName: "k8s-rootfs-1.37.1", Assets: asset("k8s-rootfs.ext4.gz", 18)},
	})

	got := map[string]string{}
	for _, u := range m.Updates(releases) {
		got[u.Local] = u.Command
	}
	want := map[string]string{
		"k8s-kernel": "sudo vmm kernel pull k8s-kernel --force",
		"k8s-1.36.2": "sudo vmm image pull k8s-1.36.5",
		"k8s-1.35.9": "sudo vmm image pull k8s-1.35.9 --force",
	}
	if !reflect.DeepEqual(got, want) {
		// nested-kernel is current; security-kernel and 1.37 aren't local.
		t.Errorf("updates = %v, want %v", got, want)
	}
}

func TestAvailableReleasesKernelSeries(t *testing.T) {
	m := &Manager{KernelDir: t.TempDir(), RootfsDir: t.TempDir()}
	rel := func(tag string) ghRelease {
		return ghRelease{TagName: tag, Assets: []ghAsset{{Name: "security-vmlinux.bin"}}}
	}
	// Newest first, as GitHub lists them: the 6.6 build happened to finish
	// first, which used to make it "security-kernel".
	got := map[string]string{}
	for _, r := range m.availableReleases([]ghRelease{
		rel("security-kernel-6.6.159-r3"),
		rel("security-kernel-6.12.113-r3"),
		rel("security-kernel-6.12.113-r2"),
		rel("security-kernel-5.10.272-r3"),
	}) {
		got[r.LocalName] = r.Tag
	}
	want := map[string]string{
		"security-kernel":      "security-kernel-6.12.113-r3",
		"security-kernel-6.6":  "security-kernel-6.6.159-r3",
		"security-kernel-5.10": "security-kernel-5.10.272-r3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("local names = %v, want %v", got, want)
	}
}

package image

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// releaseCacheFile holds the last GitHub release listing, so update checks
// don't spend the unauthenticated API limit (60 requests an hour, shared
// with `vmm upgrade`) on every `vmm kernel list` or page view.
const releaseCacheFile = ".github-releases.json"

type releaseCache struct {
	Fetched  time.Time   `json:"fetched"`
	Releases []ghRelease `json:"releases"`
}

// ListAvailableReleasesCached is ListAvailableReleases, served from a cache
// of the GitHub listing if it is younger than maxAge.
func (m *Manager) ListAvailableReleasesCached(maxAge time.Duration) ([]AvailableRelease, error) {
	data, err := os.ReadFile(filepath.Join(m.KernelDir, releaseCacheFile))
	if err == nil {
		var c releaseCache
		if json.Unmarshal(data, &c) == nil && time.Since(c.Fetched) < maxAge {
			return m.availableReleases(c.Releases), nil
		}
	}
	return m.ListAvailableReleases()
}

// saveReleaseCache records a GitHub listing for ListAvailableReleasesCached.
// Best effort: a failure only means the next check queries GitHub again.
func (m *Manager) saveReleaseCache(releases []ghRelease) {
	data, err := json.Marshal(releaseCache{Fetched: time.Now(), Releases: releases})
	if err != nil {
		return
	}
	path := filepath.Join(m.KernelDir, releaseCacheFile)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// localModTime returns when a local kernel or rootfs image was written,
// and whether it exists.
func (m *Manager) localModTime(typ, name string) (time.Time, bool) {
	path := filepath.Join(m.KernelDir, name)
	if typ != "kernel" {
		path = m.GetImagePath(name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// Update is a release that is newer than a local kernel or rootfs image.
type Update struct {
	// Local is the local artifact the release supersedes.
	Local   string
	Release AvailableRelease
	// Command downloads the release.
	Command string
}

// Updates lists the releases newer than the local copies: a re-published
// asset (kernel -rN releases, replaced k8s rootfs images), or a newer patch
// for a Kubernetes minor version that has only older local images.
func (m *Manager) Updates(releases []AvailableRelease) []Update {
	local := m.localK8sVersions()
	var updates []Update
	for _, r := range releases {
		pull := "sudo vmm kernel pull "
		if r.Type != "kernel" {
			pull = "sudo vmm image pull "
		}
		if r.Outdated {
			updates = append(updates, Update{Local: r.LocalName, Release: r, Command: pull + r.LocalName + " --force"})
			continue
		}
		v := K8sImageVersion(r.LocalName)
		if r.Downloaded || v == "" {
			continue
		}
		if older := newestPatch(local, parseK8sVersion(v)); older != "" {
			updates = append(updates, Update{Local: "k8s-" + older, Release: r, Command: pull + r.LocalName})
		}
	}
	return updates
}

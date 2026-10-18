package image

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// K8sImageVersion returns the Kubernetes version of a k8s rootfs image name
// ("k8s-1.36.5" -> "1.36.5"), or "" if name isn't one.
func K8sImageVersion(name string) string {
	v, ok := strings.CutPrefix(name, "k8s-")
	if !ok || len(parseK8sVersion(v)) != 3 {
		return ""
	}
	return v
}

// ResolveK8sImage picks the k8s rootfs image for a cluster and returns its
// name and the Kubernetes version it carries.
//
// version is either exact (1.36.2), which only matches that version, or a
// minor (1.36), which takes its newest patch: released or local, whichever
// is newer. A released image that isn't local yet is downloaded. For an
// exact version with no image, it returns "" and the version, and the
// cluster installs Kubernetes at provisioning instead.
func (m *Manager) ResolveK8sImage(version string) (string, string, error) {
	want := parseK8sVersion(version)
	if want == nil {
		return "", "", fmt.Errorf("invalid Kubernetes version %q", version)
	}

	if len(want) == 3 {
		name := "k8s-" + version
		if m.ImageExists(name) {
			return name, version, nil
		}
		releases, err := k8sRootfsReleases()
		if err == nil {
			if url, ok := releases[version]; ok {
				if err := m.DownloadRootfsFromRelease(url, name); err != nil {
					return "", "", fmt.Errorf("failed to download Kubernetes %s rootfs: %w", version, err)
				}
				return name, version, nil
			}
		}
		return "", version, nil
	}

	// Minor version: the newest patch, local or released.
	local := newestPatch(m.localK8sVersions(), want)
	releases, relErr := k8sRootfsReleases()
	var released []string
	for v := range releases {
		released = append(released, v)
	}
	remote := newestPatch(released, want)

	if remote != "" && (local == "" || compareK8sVersions(remote, local) > 0) {
		name := "k8s-" + remote
		fmt.Printf("Downloading pre-built Kubernetes %s rootfs...\n", remote)
		err := m.DownloadRootfsFromRelease(releases[remote], name)
		if err == nil {
			return name, remote, nil
		}
		if local == "" {
			return "", "", fmt.Errorf("failed to download Kubernetes %s rootfs: %w", remote, err)
		}
		fmt.Printf("Warning: failed to download Kubernetes %s rootfs (%v), using local %s\n", remote, err, local)
	}
	if local != "" {
		return "k8s-" + local, local, nil
	}
	if relErr != nil {
		return "", "", fmt.Errorf("no local Kubernetes %s rootfs, and GitHub releases could not be queried: %w", version, relErr)
	}
	return "", "", fmt.Errorf("no pre-built Kubernetes %s rootfs; pass an exact --k8s-version (e.g. %s.0) to install it at provisioning", version, version)
}

// localK8sVersions lists the versions of the local k8s-<version> images.
func (m *Manager) localK8sVersions() []string {
	entries, err := os.ReadDir(m.RootfsDir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".ext4")
		if !ok || e.IsDir() {
			continue
		}
		if v := K8sImageVersion(name); v != "" {
			versions = append(versions, v)
		}
	}
	return versions
}

// listReleases fetches the GitHub releases; tests replace it.
var listReleases = fetchReleases

// k8sRootfsReleases maps each released k8s rootfs version to its image URL.
func k8sRootfsReleases() (map[string]string, error) {
	releases, err := listReleases()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, rel := range releases {
		v, ok := strings.CutPrefix(rel.TagName, "k8s-rootfs-")
		if !ok || K8sImageVersion("k8s-"+v) == "" {
			continue
		}
		for _, a := range rel.Assets {
			if a.Name == "k8s-rootfs.ext4.gz" {
				out[v] = a.BrowserDownloadURL
			}
		}
	}
	return out, nil
}

// newestPatch returns the newest of versions in the minor version want
// (major, minor), or "".
func newestPatch(versions []string, want []int) string {
	best := ""
	for _, v := range versions {
		p := parseK8sVersion(v)
		if len(p) != 3 || p[0] != want[0] || p[1] != want[1] {
			continue
		}
		if best == "" || compareK8sVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

// parseK8sVersion parses "1.36" or "1.36.5", or returns nil.
func parseK8sVersion(v string) []int {
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return nil
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return nil
		}
		nums[i] = n
	}
	return nums
}

// compareK8sVersions compares two exact versions like strings.Compare.
func compareK8sVersions(a, b string) int {
	pa, pb := parseK8sVersion(a), parseK8sVersion(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

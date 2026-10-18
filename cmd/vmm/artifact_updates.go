package main

import (
	"fmt"
	"os"
	"time"

	"github.com/raesene/baremetalvmm/internal/image"
)

// artifactCheckMaxAge is how long `vmm kernel list` and `vmm image list`
// reuse the GitHub release listing before checking for newer artifacts.
const artifactCheckMaxAge = 12 * time.Hour

// printArtifactUpdates lists newer releases of local kernels ("kernel"),
// rootfs images ("rootfs") or both (""). It never fails or holds the
// command up for long: an offline host just gets no notice. Set
// VMM_NO_UPDATE_CHECK=1 to skip it.
func printArtifactUpdates(imgMgr *image.Manager, typ string) {
	if os.Getenv("VMM_NO_UPDATE_CHECK") != "" {
		return
	}
	done := make(chan []image.AvailableRelease, 1)
	go func() {
		releases, _ := imgMgr.ListAvailableReleasesCached(artifactCheckMaxAge)
		done <- releases
	}()
	var releases []image.AvailableRelease
	select {
	case releases = <-done:
	case <-time.After(5 * time.Second):
		return
	}

	var updates []image.Update
	for _, u := range imgMgr.Updates(releases) {
		if typ == "" || u.Release.Type == typ {
			updates = append(updates, u)
		}
	}
	if len(updates) == 0 {
		return
	}
	fmt.Println("\nNewer releases available:")
	for _, u := range updates {
		fmt.Printf("  %-20s %s (published %s)\n", u.Local, u.Release.Tag, u.Release.Published.Format("2006-01-02"))
		fmt.Printf("  %-20s   %s\n", "", u.Command)
	}
}

// releaseStatus is the marker `list --remote` shows for a release.
func releaseStatus(r image.AvailableRelease) string {
	switch {
	case r.Outdated:
		return "↑ "
	case r.Downloaded:
		return "✓ "
	default:
		return "  "
	}
}

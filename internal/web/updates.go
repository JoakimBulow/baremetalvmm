package web

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/raesene/baremetalvmm/internal/upgrade"
)

const updateCheckInterval = 12 * time.Hour

// UpdateInfo describes a newer vmm release than the one running.
type UpdateInfo struct {
	Version string
	URL     string
}

// updateChecker periodically looks up the latest vmm release so the UI can
// point out that `sudo vmm upgrade` has something to install. Failures (e.g.
// an offline host) are logged once per check and otherwise ignored. Set
// VMM_NO_UPDATE_CHECK=1 to disable it.
type updateChecker struct {
	mu      sync.Mutex
	current string
	latest  *upgrade.Release
	lookup  func(context.Context) (*upgrade.Release, error)

	// artifacts counts local kernels and rootfs images with a newer
	// release; nil disables the check.
	artifacts     func() int
	artifactCount int
}

func newUpdateChecker(current string) *updateChecker {
	return &updateChecker{
		current: current,
		lookup:  upgrade.NewClient().LatestRelease,
	}
}

func (u *updateChecker) run() {
	if os.Getenv("VMM_NO_UPDATE_CHECK") != "" {
		return
	}
	for {
		u.check()
		u.checkArtifacts()
		time.Sleep(updateCheckInterval)
	}
}

func (u *updateChecker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := u.lookup(ctx)
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	u.mu.Lock()
	u.latest = rel
	u.mu.Unlock()
}

// checkArtifacts recounts the artifact updates.
func (u *updateChecker) checkArtifacts() {
	if u.artifacts == nil {
		return
	}
	u.setArtifactCount(u.artifacts())
}

// setArtifactCount records how many local artifacts have newer releases,
// e.g. after the images page has listed them or a download replaced one.
func (u *updateChecker) setArtifactCount(n int) {
	u.mu.Lock()
	u.artifactCount = n
	u.mu.Unlock()
}

// artifactUpdates returns how many local artifacts have newer releases.
func (u *updateChecker) artifactUpdates() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.artifactCount
}

// available returns the newer release, or nil if the running version is
// current, unknown (a development build), or no check has succeeded yet.
func (u *updateChecker) available() *UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest == nil {
		return nil
	}
	if _, ok := upgrade.ParseVersion(u.current); !ok {
		return nil
	}
	if upgrade.Compare(u.latest.Version, u.current) <= 0 {
		return nil
	}
	return &UpdateInfo{Version: u.latest.Version, URL: u.latest.URL}
}

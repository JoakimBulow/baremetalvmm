package web

import (
	"context"
	"errors"
	"testing"

	"github.com/raesene/baremetalvmm/internal/upgrade"
)

func checkerWith(current string, rel *upgrade.Release, err error) *updateChecker {
	u := newUpdateChecker(current)
	u.lookup = func(context.Context) (*upgrade.Release, error) { return rel, err }
	u.check()
	return u
}

func TestUpdateAvailable(t *testing.T) {
	newer := &upgrade.Release{Version: "0.14.0", URL: "https://example/v0.14.0"}

	if got := checkerWith("0.13.1", newer, nil).available(); got == nil || got.Version != "0.14.0" || got.URL == "" {
		t.Fatalf("expected update 0.14.0, got %+v", got)
	}
	if got := checkerWith("0.14.0", newer, nil).available(); got != nil {
		t.Fatalf("up to date, got %+v", got)
	}
	if got := checkerWith("0.15.0", newer, nil).available(); got != nil {
		t.Fatalf("newer than latest, got %+v", got)
	}
	if got := checkerWith("dev", newer, nil).available(); got != nil {
		t.Fatalf("dev build should not nag, got %+v", got)
	}
	if got := checkerWith("0.13.1", nil, errors.New("offline")).available(); got != nil {
		t.Fatalf("failed check should report nothing, got %+v", got)
	}
}

func TestArtifactUpdates(t *testing.T) {
	u := newUpdateChecker("0.17.1")
	if got := u.artifactUpdates(); got != 0 {
		t.Fatalf("before any check: %d", got)
	}
	u.checkArtifacts() // no counter set: a no-op
	u.artifacts = func() int { return 3 }
	u.checkArtifacts()
	if got := u.artifactUpdates(); got != 3 {
		t.Fatalf("after check: %d, want 3", got)
	}
	u.setArtifactCount(0)
	if got := u.artifactUpdates(); got != 0 {
		t.Fatalf("after reset: %d, want 0", got)
	}
}

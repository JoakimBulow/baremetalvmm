package cluster

import (
	"reflect"
	"testing"
	"time"
)

func TestNodesReady(t *testing.T) {
	since := time.Date(2026, 10, 18, 10, 0, 0, 500, time.UTC)
	out := `cp True 2026-10-18T10:00:00Z
w1 True 2026-10-18T09:55:00Z
w2 False 2026-10-18T10:00:30Z
w3 Unknown 2026-10-18T10:01:00Z
w4 True 2026-10-18T09:50:00Z
w5
`
	started := map[string]time.Time{"cp": since, "w1": since, "w2": since, "w3": since, "w5": since}
	ready, waiting := nodesReady(out, started)
	// w4 was already running, so a heartbeat before since is fine for it.
	if ready != 2 {
		t.Errorf("ready = %d, want 2 (cp, w4)", ready)
	}
	// w1's Ready is stale, from before the stop; w5 has no Ready condition yet.
	if want := []string{"w1", "w2", "w3", "w5"}; !reflect.DeepEqual(waiting, want) {
		t.Errorf("waiting = %v, want %v", waiting, want)
	}

	if ready, waiting := nodesReady("", started); ready != 0 || len(waiting) != 0 {
		t.Errorf("empty output: ready=%d waiting=%v", ready, waiting)
	}
}

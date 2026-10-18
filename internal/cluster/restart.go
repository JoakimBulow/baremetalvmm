package cluster

import (
	"fmt"
	"strings"
	"time"
)

// nodeStatusQuery prints one line per node: name, Ready status and the
// time the kubelet last posted it.
const nodeStatusQuery = `kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{" "}{range .status.conditions[?(@.type=="Ready")]}{.status}{" "}{.lastHeartbeatTime}{end}{"\n"}{end}'`

// WaitForNodesAfterStart waits until every node of a cluster is Ready, and
// those in started (node name to when its VM was started) with status their
// kubelet posted since. The API server is back before the kubelets are, and
// until they report it still serves the Ready status recorded before the
// cluster stopped. Nodes that were already running only need to be Ready:
// an unchanged kubelet may not post status for minutes.
func WaitForNodesAfterStart(cl *Cluster, client *SSHClient, started map[string]time.Time, timeout time.Duration) error {
	kubeconfig := "/etc/kubernetes/admin.conf"
	if cl.Distro == DistroOpenShift {
		kubeconfig = microShiftKubeconfig
	}
	expected := len(cl.ClusterVMs())
	if cl.Distro == DistroOpenShift {
		expected = 1
	}

	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := client.Run(fmt.Sprintf("KUBECONFIG=%s %s 2>/dev/null", kubeconfig, nodeStatusQuery))
		if err == nil {
			last = out
			if ready, _ := nodesReady(out, started); ready >= expected {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	_, waiting := nodesReady(last, started)
	if len(waiting) == 0 {
		waiting = []string{"(API server not answering)"}
	}
	return fmt.Errorf("timed out after %s waiting for nodes to report Ready: %s", timeout, strings.Join(waiting, ", "))
}

// nodesReady parses nodeStatusQuery output and returns how many nodes are
// Ready (for nodes in started, with a heartbeat at or after their start)
// and the names of the rest.
func nodesReady(out string, started map[string]time.Time) (int, []string) {
	ready := 0
	var waiting []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 3 && fields[1] == "True" {
			since, restarted := started[fields[0]]
			// Heartbeats have one-second resolution.
			if hb, err := time.Parse(time.RFC3339, fields[2]); err == nil && (!restarted || !hb.Before(since.Truncate(time.Second))) {
				ready++
				continue
			}
		}
		waiting = append(waiting, fields[0])
	}
	return ready, waiting
}

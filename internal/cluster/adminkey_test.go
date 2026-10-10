package cluster

import (
	"strings"
	"testing"
)

func TestAdminSSHConfig(t *testing.T) {
	nodes := []NodeInfo{
		{Name: "demo-control-plane", IP: "172.16.0.2"},
		{Name: "demo-worker-1", IP: "172.16.0.3"},
	}
	got := AdminSSHConfig("demo", nodes)

	for _, want := range []string{
		"Host demo-control-plane 172.16.0.2\n    HostName 172.16.0.2\n",
		"Host demo-worker-1 172.16.0.3\n    HostName 172.16.0.3\n",
		"IdentityFile " + adminKeyRemotePath,
		"User root",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "Host ") != len(nodes) {
		t.Errorf("expected %d Host blocks:\n%s", len(nodes), got)
	}
}

package lifecycle

import (
	"strings"
	"testing"

	"github.com/raesene/baremetalvmm/internal/image"
)

func TestCheckNestedClusterRejectsBeforeHostChecks(t *testing.T) {
	imgMgr := image.NewManager(t.TempDir(), t.TempDir())

	if err := CheckNestedCluster(imgMgr, true, "k8s-kernel"); err == nil || !strings.Contains(err.Error(), "kubeadm") {
		t.Errorf("OpenShift: err = %v, want a kubeadm-only error", err)
	}
	// nested-kernel lacks the Kubernetes options, so there's no fallback to it.
	if err := CheckNestedCluster(imgMgr, false, ""); err == nil || !strings.Contains(err.Error(), "k8s-kernel") {
		t.Errorf("no kernel: err = %v, want a pull k8s-kernel error", err)
	}
}

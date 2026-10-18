package cluster

import (
	"testing"

	"github.com/raesene/baremetalvmm/internal/image"
)

func TestChooseK8sImageFromImage(t *testing.T) {
	m := image.NewManager(t.TempDir(), t.TempDir())

	name, v, err := ChooseK8sImage(m, "k8s-1.36.2", "")
	if err != nil || name != "k8s-1.36.2" || v != "1.36.2" {
		t.Errorf("image version: got (%q, %q, %v)", name, v, err)
	}
	if _, _, err := ChooseK8sImage(m, "k8s-1.36.2", "1.36.0"); err == nil {
		t.Error("expected an error for a version that contradicts the image")
	}
	// Custom images keep the requested version (provisioning reads kubeadm).
	name, v, err = ChooseK8sImage(m, "my-snapshot", "")
	if err != nil || name != "my-snapshot" || v != "" {
		t.Errorf("custom image: got (%q, %q, %v)", name, v, err)
	}
}

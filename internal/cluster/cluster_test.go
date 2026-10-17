package cluster

import (
	"testing"

	"github.com/raesene/baremetalvmm/internal/hypervisor"
	"github.com/raesene/baremetalvmm/internal/vm"
)

func TestConfigureNodeVM(t *testing.T) {
	cl := &Cluster{Name: "c", ControlPlaneVM: "c-control-plane", AdminVM: "c-admin", Nested: true}

	node := vm.NewVM("c-control-plane")
	cl.ConfigureNodeVM(node)
	if node.Backend != hypervisor.CloudHypervisor || !node.Nested {
		t.Errorf("nested cluster node: backend=%q nested=%v", node.Backend, node.Nested)
	}

	// The admin workstation doesn't need nesting and stays on Firecracker.
	admin := vm.NewVM("c-admin")
	cl.ConfigureNodeVM(admin)
	if admin.Backend != "" || admin.Nested {
		t.Errorf("admin workstation: backend=%q nested=%v", admin.Backend, admin.Nested)
	}

	cl.Nested = false
	plain := vm.NewVM("c-control-plane")
	cl.ConfigureNodeVM(plain)
	if plain.Backend != "" || plain.Nested {
		t.Errorf("non-nested cluster node: backend=%q nested=%v", plain.Backend, plain.Nested)
	}
}

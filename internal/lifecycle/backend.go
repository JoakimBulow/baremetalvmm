package lifecycle

import (
	"fmt"
	"strings"

	"github.com/raesene/baremetalvmm/internal/cloudhypervisor"
	"github.com/raesene/baremetalvmm/internal/hypervisor"
	"github.com/raesene/baremetalvmm/internal/image"
)

// NestedKernel is the kernel a nested VM gets when none is named. Our other
// kernels are built without KVM, so the guest couldn't use the
// virtualization extensions it is given.
const NestedKernel = "nested-kernel"

// ChooseBackend settles the backend, and for nested VMs the kernel, of a new
// VM, and checks this host can run it. backend is the one asked for ("" for
// the default) and kernel the kernel asked for ("" for none). Nested implies
// Cloud Hypervisor. The returned backend is "" for Firecracker, so the VM's
// JSON stays readable by vmm versions that predate backends.
func ChooseBackend(imgMgr *image.Manager, backend string, nested bool, kernel string) (string, string, error) {
	switch backend {
	case "", hypervisor.Firecracker, hypervisor.CloudHypervisor:
	default:
		return "", "", fmt.Errorf("unknown backend %q; choose from %s", backend, strings.Join(hypervisor.Names, ", "))
	}

	if nested {
		if backend == hypervisor.Firecracker {
			return "", "", fmt.Errorf("firecracker can't run nested VMs; use the %s backend, which nested VMs get by default", hypervisor.CloudHypervisor)
		}
		backend = hypervisor.CloudHypervisor
		if err := cloudhypervisor.HostNested(); err != nil {
			return "", "", err
		}
		if kernel == "" {
			if !imgMgr.KernelExists(NestedKernel) {
				return "", "", fmt.Errorf("nested VMs need a kernel with KVM support; build one with 'sudo vmm kernel build --version 6.1 --name %s --profile nested', or pass --kernel", NestedKernel)
			}
			kernel = NestedKernel
		}
	}

	if backend == hypervisor.CloudHypervisor {
		if _, err := cloudhypervisor.NewClient().Binary(); err != nil {
			return "", "", err
		}
	}
	if backend == hypervisor.Firecracker {
		backend = ""
	}
	return backend, kernel, nil
}

package main

import (
	"context"
	"fmt"

	"github.com/raesene/baremetalvmm/internal/hypervisor"
	"github.com/raesene/baremetalvmm/internal/network"
	"github.com/raesene/baremetalvmm/internal/validate"
	"github.com/raesene/baremetalvmm/internal/vm"
	"github.com/spf13/cobra"
)

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "stop <name>",
		Short:             "Stop a microVM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validate.VMName(name); err != nil {
				return err
			}
			paths := cfg.GetPaths()

			existingVM, err := vm.Load(paths.VMs, name)
			if err != nil {
				return fmt.Errorf("VM '%s' not found", name)
			}

			// Update state
			hypervisor.UpdateVMState(existingVM)

			if existingVM.State != vm.StateRunning {
				return fmt.Errorf("VM '%s' is not running (state: %s)", name, existingVM.State)
			}

			fmt.Printf("Stopping VM '%s'...\n", name)

			existingVM.State = vm.StateStopping
			existingVM.Save(paths.VMs)

			// Terminate the Firecracker process, escalating to signals if the
			// guest does not shut down. Returns only once the process is gone.
			ctx := context.Background()
			if err := hypervisor.Terminate(ctx, existingVM); err != nil {
				existingVM.State = vm.StateError
				if saveErr := existingVM.Save(paths.VMs); saveErr != nil {
					fmt.Printf("Warning: failed to save VM state: %v\n", saveErr)
				}
				return fmt.Errorf("failed to stop VM '%s': %w", name, err)
			}

			finishStop(network.NewManager(cfg.BridgeName, cfg.Subnet, cfg.Gateway, cfg.HostInterface), existingVM)

			fmt.Printf("VM '%s' stopped\n", name)
			return nil
		},
	}
}

// finishStop tidies up after a VM's hypervisor process has been terminated:
// it removes the TAP device and port forwards and saves the VM as stopped.
func finishStop(netMgr *network.Manager, v *vm.VM) {
	// Clean up TAP device so it can be reused on next start
	if v.TapDevice != "" && netMgr.TapExists(v.TapDevice) {
		if err := netMgr.DeleteTap(v.TapDevice); err != nil {
			fmt.Printf("Warning: failed to delete TAP device: %v\n", err)
		}
	}

	clearPortForwards(netMgr, v)

	// Cleanup (Terminate already cleared the PID and removed the socket)
	v.State = vm.StateStopped
	if err := v.Save(cfg.GetPaths().VMs); err != nil {
		fmt.Printf("Warning: failed to save VM state: %v\n", err)
	}
}

package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/raesene/baremetalvmm/internal/hypervisor"
	"github.com/raesene/baremetalvmm/internal/lifecycle"
	"github.com/raesene/baremetalvmm/internal/vm"
	"github.com/spf13/cobra"
)

func autostartCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "autostart",
		Short:  "Start all VMs marked for auto-start (used by systemd)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := cfg.GetPaths()

			vms, err := vm.List(paths.VMs)
			if err != nil {
				return fmt.Errorf("failed to list VMs: %w", err)
			}

			started := 0
			for _, v := range vms {
				// Skip VMs not marked for autostart
				if !v.AutoStart {
					continue
				}

				// Update actual state
				hypervisor.UpdateVMState(v)
				if v.State == vm.StateRunning {
					fmt.Printf("VM '%s' is already running\n", v.Name)
					continue
				}

				fmt.Printf("Auto-starting VM '%s'...\n", v.Name)

				err := lifecycle.Start(cfg, v, lifecycle.Options{
					BestEffort: true,
					Warn:       func(msg string) { fmt.Printf("  Warning: %s\n", msg) },
				})
				if err != nil {
					fmt.Printf("  Error: %v\n", err)
					continue
				}

				fmt.Printf("  Started (IP: %s, PID: %d)\n", v.IPAddress, v.PID)
				started++
			}

			fmt.Printf("Auto-started %d VMs\n", started)
			return nil
		},
	}
}

func autostopCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "autostop",
		Short:  "Stop all running VMs (used by systemd)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := cfg.GetPaths()

			vms, err := vm.List(paths.VMs)
			if err != nil {
				return fmt.Errorf("failed to list VMs: %w", err)
			}

			var running []*vm.VM
			for _, v := range vms {
				hypervisor.UpdateVMState(v)
				if v.State == vm.StateRunning {
					running = append(running, v)
				}
			}

			// Stop the VMs in parallel: each one gets up to 30s to shut down
			// cleanly, and one at a time a busy host would outlast systemd's
			// stop timeout and have the rest killed.
			errs := make([]error, len(running))
			var wg sync.WaitGroup
			for i, v := range running {
				fmt.Printf("Stopping VM '%s'...\n", v.Name)
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = hypervisor.Terminate(context.Background(), v)
				}()
			}
			wg.Wait()

			// Port forwards and state afterwards, one VM at a time: iptables
			// calls can't safely overlap.
			netMgr := newNetManager()
			stopped := 0
			for i, v := range running {
				if errs[i] != nil {
					fmt.Printf("Warning: %v\n", errs[i])
					v.State = vm.StateError
					if saveErr := v.Save(paths.VMs); saveErr != nil {
						fmt.Printf("Warning: failed to save VM state: %v\n", saveErr)
					}
					continue
				}
				clearPortForwards(netMgr, v)
				v.State = vm.StateStopped
				v.Save(paths.VMs)
				stopped++
			}

			fmt.Printf("Stopped %d VMs\n", stopped)
			return nil
		},
	}
}

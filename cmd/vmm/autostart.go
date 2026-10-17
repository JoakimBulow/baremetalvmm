package main

import (
	"context"
	"fmt"

	"github.com/raesene/baremetalvmm/internal/firecracker"
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

			fcClient := firecracker.NewClient()
			started := 0
			for _, v := range vms {
				// Skip VMs not marked for autostart
				if !v.AutoStart {
					continue
				}

				// Update actual state
				fcClient.UpdateVMState(v)
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

			fcClient := firecracker.NewClient()
			netMgr := newNetManager()
			stopped := 0

			for _, v := range vms {
				fcClient.UpdateVMState(v)
				if v.State != vm.StateRunning {
					continue
				}

				fmt.Printf("Stopping VM '%s'...\n", v.Name)

				ctx := context.Background()
				if err := fcClient.Terminate(ctx, v); err != nil {
					fmt.Printf("Warning: %v\n", err)
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

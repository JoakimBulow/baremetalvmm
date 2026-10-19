package main

import (
	"fmt"
	"os"

	"github.com/raesene/baremetalvmm/internal/config"
)

func main() {
	if src, err := config.MigrateSystemConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create %s: %v\n", config.SystemConfigPath, err)
	} else if src != "" {
		fmt.Fprintf(os.Stderr, "Copied %s to %s, which root-run vmm now uses; edit that file from now on\n", src, config.SystemConfigPath)
	}

	var err error
	cfg, err = config.Load(config.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

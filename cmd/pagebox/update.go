package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/arpitbhayani/pagebox/internal/ui"
)

var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pagebox version %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Upgrade the binary",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Heading("pagebox update", nil, nil)
		ui.PrintInfo("checking for updates...", "", nil)
		// Deferred until release pipeline is established as per PRD
		ui.PrintOk("already up to date", version, nil)
	},
}



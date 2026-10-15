package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/arpitbhayani/pagebox/internal/ui"
)

func main() {
	ui.SetColor(nil) // Allow auto-detection
	
	rootCmd := &cobra.Command{
		Use:   "pagebox",
		Short: "Pagebox serves a folder of HTML documents over HTTP and syncs them.",
	}

	// Global flags
	var quiet bool
	var assumeYes bool
	var noColor bool

	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "suppress narration")
	rootCmd.PersistentFlags().BoolVar(&assumeYes, "yes", false, "never block on a prompt, accept defaults")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "force color off")

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		ui.SetQuiet(quiet)
		ui.SetYes(assumeYes)
		if noColor {
			disabled := false
			ui.SetColor(&disabled)
		}
	}

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(listCmd)
	pullCmd.Flags().Bool("force", false, "overwrite dirty/diverged files")
	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(serveCmd)
	daemonInstallCmd.Flags().Bool("no-sudo", false, "skip privileged setup (hosts, port 80)")
	daemonUninstallCmd.Flags().Bool("no-sudo", false, "skip privileged cleanup")
	
	daemonCmd.AddCommand(daemonInstallCmd)
	daemonCmd.AddCommand(daemonUninstallCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	rootCmd.AddCommand(daemonCmd)

	uninstallCmd.Flags().Bool("no-sudo", false, "skip privileged cleanup")
	rootCmd.AddCommand(uninstallCmd)

	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

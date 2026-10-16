package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/arpitbhayani/pagebox/internal/ui"
)

var noUpdate bool

func main() {
	ui.SetColor(nil) // Allow auto-detection

	var showVer bool
	var doUpdate bool

	rootCmd := &cobra.Command{
		Use:   "pagebox",
		Short: "Pagebox serves a folder of HTML documents over HTTP and syncs them.",
		Run: func(cmd *cobra.Command, args []string) {
			if showVer {
				fmt.Printf("pagebox version %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
				return
			}
			if doUpdate {
				if err := runSelfUpdate(version); err != nil {
					ui.PrintErr("update failed", err.Error(), os.Stderr)
					os.Exit(1)
				}
				return
			}
			_ = cmd.Help()
		},
	}

	// Global flags
	var quiet bool
	var assumeYes bool
	var noColor bool

	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "suppress narration")
	rootCmd.PersistentFlags().BoolVar(&assumeYes, "yes", false, "never block on a prompt, accept defaults")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "force color off")
	rootCmd.PersistentFlags().BoolVar(&noUpdate, "no-update", false, "disable automatic background update checks")

	rootCmd.Flags().BoolVarP(&showVer, "version", "v", false, "print version and exit")
	rootCmd.Flags().BoolVar(&doUpdate, "update", false, "check for and install latest version of pagebox")

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		ui.SetQuiet(quiet)
		ui.SetYes(assumeYes)
		if noColor {
			disabled := false
			ui.SetColor(&disabled)
		}
	}

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(listCmd)
	pullCmd.Flags().Bool("force", false, "overwrite dirty/diverged files")
	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(serveCmd)
	daemonInstallCmd.Flags().Bool("no-sudo", false, "skip privileged setup (port 80 binding)")
	daemonUninstallCmd.Flags().Bool("clean-system", false, "clean up privileged settings (/etc/hosts, sysctl)")
	daemonUninstallCmd.Flags().Bool("no-sudo", false, "skip privileged cleanup")

	daemonCmd.AddCommand(daemonInstallCmd)
	daemonCmd.AddCommand(daemonUninstallCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
	rootCmd.AddCommand(daemonCmd)

	uninstallCmd.Flags().Bool("clean-system", false, "clean up privileged settings (/etc/hosts, sysctl)")
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

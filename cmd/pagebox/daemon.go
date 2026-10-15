package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/daemon"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the background service",
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register and start the per-user service",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Heading("daemon setup", nil, nil)

		noSudo, _ := cmd.Flags().GetBool("no-sudo")

		// One-time privileged setup
		if !noSudo {
			ui.PrintStep("sudo setup", "hosts entry and port 80 binding", nil)
			
			// Try to setup hosts
			if !hasHostsEntry() {
				ui.PrintInfo("adding 127.0.0.1 pagebox.local to /etc/hosts", "", nil)
				execSudo("sh", "-c", "echo '127.0.0.1 pagebox.local' >> /etc/hosts")
			} else {
				ui.PrintInfo("hosts entry already exists", "", nil)
			}

			if runtime.GOOS == "linux" {
				// Linux requires lowering unprivileged port start to bind 80
				ui.PrintInfo("allowing unprivileged users to bind port 80", "/etc/sysctl.d/99-pagebox.conf", nil)
				execSudo("sh", "-c", "echo 'net.ipv4.ip_unprivileged_port_start=80' > /etc/sysctl.d/99-pagebox.conf && sysctl --system")
			}
		}

		binPath, err := os.Executable()
		if err != nil {
			ui.PrintErr("could not find executable path", err.Error(), nil)
			os.Exit(1)
		}
		binPath, _ = filepath.Abs(binPath)

		sp := ui.NewSpinner("Installing service")
		err = daemon.Install(binPath)
		sp.Stop(err == nil)

		if err != nil {
			ui.PrintErr("failed to install daemon", err.Error(), nil)
			os.Exit(1)
		}

		cfg, _ := pbconfig.Load()
		serviceType := "systemd user service"
		if runtime.GOOS == "darwin" {
			serviceType = "launchd LaunchAgent"
		}

		localAddr := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
		if cfg.Port == 80 {
			localAddr = "http://127.0.0.1"
		}

		ui.PrintOk("daemon installed and started", "", nil)
		ui.Heading("daemon summary", nil, nil)
		ui.KV("service", serviceType, 16, nil)
		ui.KV("status", "running in background", 16, nil)
		ui.KV("documents", cfg.DocsDir, 16, nil)
		ui.KV("address", localAddr, 16, nil)
		if cfg.Host != "" && cfg.Host != "127.0.0.1" && cfg.Host != "localhost" {
			ui.KV("hostname", fmt.Sprintf("http://%s", cfg.Host), 16, nil)
		}
		ui.Rule(nil)
		ui.Hint("management commands:", nil)
		ui.Bullet("view service status:   pagebox daemon status", nil)
		ui.Bullet("stream live logs:      pagebox daemon logs", nil)
		ui.Bullet("stop & uninstall:      pagebox uninstall", nil)
	},
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the background service",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Heading("daemon uninstall", nil, nil)
		noSudo, _ := cmd.Flags().GetBool("no-sudo")

		sp := ui.NewSpinner("Removing service")
		err := daemon.Uninstall()
		sp.Stop(err == nil)

		if err != nil {
			ui.PrintErr("failed to uninstall daemon", err.Error(), nil)
		} else {
			ui.PrintOk("daemon stopped and removed", "", nil)
		}

		if !noSudo {
			// Best effort cleanup
			execSudo("sh", "-c", "sed -i.bak '/127.0.0.1 pagebox.local/d' /etc/hosts")
			if runtime.GOOS == "linux" {
				execSudo("rm", "-f", "/etc/sysctl.d/99-pagebox.conf")
				execSudo("sysctl", "--system")
			}
			ui.PrintOk("cleaned up privileged settings", "", nil)
		}

		ui.Hint("pagebox background service has been completely uninstalled.", nil)
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and uninstall the background service",
	Run:   daemonUninstallCmd.Run,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the service is running",
	Run: func(cmd *cobra.Command, args []string) {
		running, url, err := daemon.Status()
		if err != nil {
			ui.PrintErr("failed to check status", err.Error(), nil)
			os.Exit(1)
		}

		if running {
			ui.PrintOk("daemon is running", url, nil)
		} else {
			ui.PrintWarn("daemon is stopped", "", nil)
		}
	},
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail the daemon log",
	Run: func(cmd *cobra.Command, args []string) {
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, ".pagebox", "logs", "daemon.log")
		
		tail := exec.Command("tail", "-f", logPath)
		tail.Stdout = os.Stdout
		tail.Stderr = os.Stderr
		
		ui.PrintInfo("tailing "+logPath, "Ctrl-C to stop", nil)
		tail.Run()
	},
}



func hasHostsEntry() bool {
	b, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "pagebox.local")
}

func execSudo(name string, arg ...string) {
	cmd := exec.Command("sudo", append([]string{name}, arg...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

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

		binPath, err := os.Executable()
		if err != nil {
			ui.PrintErr("could not find executable path", err.Error(), nil)
			os.Exit(1)
		}
		binPath, _ = filepath.Abs(binPath)

		cfg, _ := pbconfig.Load()
		daemonPort := cfg.Port
		if daemonPort <= 0 {
			daemonPort = 8732
		}

		// Privileged setup (only if explicitly binding privileged port 80)
		if !noSudo && runtime.GOOS == "linux" && daemonPort == 80 {
			ui.PrintStep("sudo setup", "port 80 binding", nil)
			ui.PrintInfo("allowing unprivileged users to bind port 80", "/etc/sysctl.d/99-pagebox.conf", nil)
			execSudo("sh", "-c", "echo 'net.ipv4.ip_unprivileged_port_start=80' > /etc/sysctl.d/99-pagebox.conf && sysctl --system")
		}

		sp := ui.NewSpinner("Installing service")
		err = daemon.Install(binPath, daemonPort)
		sp.Stop(err == nil)

		if err != nil {
			ui.PrintErr("failed to install daemon", err.Error(), nil)
			os.Exit(1)
		}
		serviceType := "systemd user service"
		if runtime.GOOS == "darwin" {
			serviceType = "launchd LaunchAgent"
		}

		localAddr := fmt.Sprintf("http://localhost:%d", daemonPort)
		if daemonPort == 80 {
			localAddr = "http://localhost"
		}

		ui.PrintOk("daemon installed and started", "", nil)
		ui.Heading("daemon summary", nil, nil)
		ui.KV("service", serviceType, 16, nil)
		ui.KV("status", "running in background", 16, nil)
		ui.KV("documents", cfg.DocsDir, 16, nil)
		ui.KV("address", localAddr, 16, nil)
		if cfg.Host != "" && cfg.Host != "127.0.0.1" && cfg.Host != "localhost" {
			hostAddr := fmt.Sprintf("http://%s:%d", cfg.Host, daemonPort)
			if daemonPort == 80 {
				hostAddr = fmt.Sprintf("http://%s", cfg.Host)
			}
			ui.KV("hostname", hostAddr, 16, nil)
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
		cleanSystem, _ := cmd.Flags().GetBool("clean-system")

		sp := ui.NewSpinner("Removing service")
		err := daemon.Uninstall()
		sp.Stop(err == nil)

		if err != nil {
			ui.PrintErr("failed to uninstall daemon", err.Error(), nil)
		} else {
			ui.PrintOk("daemon stopped and removed", "", nil)
		}

		if cleanSystem {
			cfg, _ := pbconfig.Load()
			hostToClean := cfg.Host
			if hostToClean == "" {
				hostToClean = "localhost"
			}
			cleaned := false
			if hasHostsEntry(hostToClean) {
				ui.PrintStep("sudo cleanup", fmt.Sprintf("removing %s from /etc/hosts", hostToClean), nil)
				execSudo("sh", "-c", fmt.Sprintf("sed -i.bak '/127.0.0.1 %s/d' /etc/hosts", hostToClean))
				cleaned = true
			}
			if runtime.GOOS == "linux" {
				if _, err := os.Stat("/etc/sysctl.d/99-pagebox.conf"); err == nil {
					ui.PrintStep("sudo cleanup", "removing /etc/sysctl.d/99-pagebox.conf", nil)
					execSudo("rm", "-f", "/etc/sysctl.d/99-pagebox.conf")
					execSudo("sysctl", "--system")
					cleaned = true
				}
			}
			if cleaned {
				ui.PrintOk("cleaned up privileged settings", "", nil)
			}
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



func hasHostsEntry(host string) bool {
	if host == "" || host == "localhost" || host == "127.0.0.1" {
		return true
	}
	b, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return false
	}
	return strings.Contains(string(b), host)
}

func execSudo(name string, arg ...string) {
	cmd := exec.Command("sudo", append([]string{name}, arg...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

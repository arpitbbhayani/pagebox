package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func Install(binPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(binPath)
	case "linux":
		return installSystemd(binPath)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func Uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchd()
	case "linux":
		return uninstallSystemd()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func Status() (bool, string, error) {
	switch runtime.GOOS {
	case "darwin":
		return statusLaunchd()
	case "linux":
		return statusSystemd()
	default:
		return false, "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func installLaunchd(binPath string) error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "me.arpitbhayani.pagebox.plist")
	
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>me.arpitbhayani.pagebox</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>serve</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>`, binPath)

	os.MkdirAll(filepath.Dir(plistPath), 0755)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return err
	}
	
	exec.Command("launchctl", "unload", plistPath).Run()
	time.Sleep(500 * time.Millisecond) // Give it time to completely unload
	return exec.Command("launchctl", "load", plistPath).Run()
}

func uninstallLaunchd() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "me.arpitbhayani.pagebox.plist")
	
	exec.Command("launchctl", "unload", plistPath).Run()
	return os.Remove(plistPath)
}

func statusLaunchd() (bool, string, error) {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return false, "", err
	}
	running := strings.Contains(string(out), "me.arpitbhayani.pagebox")
	if running {
		return true, "http://pagebox.local", nil
	}
	return false, "", nil
}

func installSystemd(binPath string) error {
	home, _ := os.UserHomeDir()
	serviceDir := filepath.Join(home, ".config", "systemd", "user")
	os.MkdirAll(serviceDir, 0755)
	servicePath := filepath.Join(serviceDir, "pagebox.service")

	service := fmt.Sprintf(`[Unit]
Description=Pagebox Sync Daemon
After=network.target

[Service]
ExecStart=%s serve
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, binPath)

	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return err
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return err
	}
	
	if err := exec.Command("systemctl", "--user", "enable", "pagebox.service").Run(); err != nil {
		return err
	}
	
	if err := exec.Command("systemctl", "--user", "start", "pagebox.service").Run(); err != nil {
		return err
	}

	exec.Command("loginctl", "enable-linger", os.Getenv("USER")).Run()

	return nil
}

func uninstallSystemd() error {
	exec.Command("systemctl", "--user", "stop", "pagebox.service").Run()
	exec.Command("systemctl", "--user", "disable", "pagebox.service").Run()
	
	home, _ := os.UserHomeDir()
	servicePath := filepath.Join(home, ".config", "systemd", "user", "pagebox.service")
	os.Remove(servicePath)
	
	return exec.Command("systemctl", "--user", "daemon-reload").Run()
}

func statusSystemd() (bool, string, error) {
	err := exec.Command("systemctl", "--user", "is-active", "--quiet", "pagebox.service").Run()
	running := (err == nil)
	if running {
		return true, "http://pagebox.local", nil
	}
	return false, "", nil
}

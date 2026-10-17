package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
)

func Install(binPath string, port int) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(binPath, port)
	case "linux":
		return installSystemd(binPath, port)
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

func installLaunchd(binPath string, port int) error {
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
		<string>--port</string>
		<string>%d</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>`, binPath, port)

	os.MkdirAll(filepath.Dir(plistPath), 0755)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return err
	}
	
	exec.Command("launchctl", "unload", plistPath).Run()
	time.Sleep(500 * time.Millisecond) // Give it time to completely unload
	return runCmd("launchctl", "load", plistPath)
}

func uninstallLaunchd() error {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "me.arpitbhayani.pagebox.plist")
	
	exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func daemonURL() string {
	cfg, _ := pbconfig.Load()
	port := cfg.Port
	if port <= 0 {
		port = 8732
	}
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	if port == 80 {
		return fmt.Sprintf("http://%s", host)
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func statusLaunchd() (bool, string, error) {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return false, "", err
	}
	running := strings.Contains(string(out), "me.arpitbhayani.pagebox")
	if running {
		return true, daemonURL(), nil
	}
	return false, "", nil
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

func installSystemd(binPath string, port int) error {
	home, _ := os.UserHomeDir()
	serviceDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return err
	}
	servicePath := filepath.Join(serviceDir, "pagebox.service")

	service := fmt.Sprintf(`[Unit]
Description=Pagebox Sync Daemon
After=network.target

[Service]
ExecStart=%s serve --port %d
Restart=always
RestartSec=1

[Install]
WantedBy=default.target
`, binPath, port)

	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return err
	}

	if err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
		if fallbackErr := installSystemdFallback(serviceDir, servicePath); fallbackErr == nil {
			exec.Command("loginctl", "enable-linger", os.Getenv("USER")).Run()
			return nil
		}
		return err
	}

	if err := runCmd("systemctl", "--user", "enable", "pagebox.service"); err != nil {
		return err
	}

	if err := runCmd("systemctl", "--user", "restart", "pagebox.service"); err != nil {
		return err
	}

	exec.Command("loginctl", "enable-linger", os.Getenv("USER")).Run()

	return nil
}

func installSystemdFallback(serviceDir, servicePath string) error {
	systemdPID := findUserSystemdPID()
	if systemdPID <= 0 {
		return fmt.Errorf("systemd --user is not running")
	}

	wantsDir := filepath.Join(serviceDir, "default.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	wantLink := filepath.Join(wantsDir, "pagebox.service")
	_ = os.Remove(wantLink)
	if err := os.Symlink(servicePath, wantLink); err != nil {
		return err
	}

	existingPIDs := findServicePIDs()

	// SIGHUP instructs systemd --user to reload unit files (equivalent to daemon-reload)
	if err := signalPID(systemdPID, syscall.SIGHUP); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)

	if len(existingPIDs) > 0 {
		// Terminate existing service process(es) so systemd restarts pagebox.service with the new unit
		for _, pid := range existingPIDs {
			_ = signalPID(pid, syscall.SIGTERM)
		}
	} else {
		// SIGRTMIN+0 (signal 34) instructs systemd --user to start default.target
		_ = signalPID(systemdPID, syscall.Signal(34))
	}

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		pids := findServicePIDs()
		if len(pids) > 0 && !samePIDs(existingPIDs, pids) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	if len(findServicePIDs()) > 0 {
		return nil
	}
	return fmt.Errorf("pagebox.service did not start")
}

func uninstallSystemd() error {
	stopErr := runCmd("systemctl", "--user", "stop", "pagebox.service")
	_ = runCmd("systemctl", "--user", "disable", "pagebox.service")

	home, _ := os.UserHomeDir()
	serviceDir := filepath.Join(home, ".config", "systemd", "user")
	servicePath := filepath.Join(serviceDir, "pagebox.service")
	wantLink := filepath.Join(serviceDir, "default.target.wants", "pagebox.service")

	if stopErr != nil {
		if systemdPID := findUserSystemdPID(); systemdPID > 0 {
			if pids := findServicePIDs(); len(pids) > 0 {
				// Disable Restart before sending SIGTERM so systemd --user doesn't respawn it
				if b, err := os.ReadFile(servicePath); err == nil {
					updated := strings.ReplaceAll(string(b), "Restart=always", "Restart=no")
					_ = os.WriteFile(servicePath, []byte(updated), 0644)
					_ = signalPID(systemdPID, syscall.SIGHUP)
					time.Sleep(150 * time.Millisecond)
				}
				for _, pid := range pids {
					_ = signalPID(pid, syscall.SIGTERM)
				}
			}
		}
	}

	_ = os.Remove(wantLink)
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
		if systemdPID := findUserSystemdPID(); systemdPID > 0 {
			return signalPID(systemdPID, syscall.SIGHUP)
		}
		return err
	}
	return nil
}

func statusSystemd() (bool, string, error) {
	err := exec.Command("systemctl", "--user", "is-active", "--quiet", "pagebox.service").Run()
	running := (err == nil) || len(findServicePIDs()) > 0
	if running {
		return true, daemonURL(), nil
	}
	return false, "", nil
}

func findUserSystemdPID() int {
	uid := os.Getuid()
	cgroupInit := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/init.scope/cgroup.procs", uid, uid)
	for _, pid := range readCgroupPIDs(cgroupInit) {
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err == nil && strings.TrimSpace(string(comm)) == "systemd" {
			return pid
		}
	}

	out, err := exec.Command("pgrep", "-u", strconv.Itoa(uid), "-f", "systemd --user").Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && pid > 0 {
				return pid
			}
		}
	}
	return 0
}

func findServicePIDs() []int {
	uid := os.Getuid()
	paths := []string{
		fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/app.slice/pagebox.service/cgroup.procs", uid, uid),
		fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/pagebox.service/cgroup.procs", uid, uid),
	}
	for _, p := range paths {
		if pids := readCgroupPIDs(p); len(pids) > 0 {
			return pids
		}
	}
	return nil
}

func readCgroupPIDs(path string) []int {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if pid, err := strconv.Atoi(line); err == nil && pid > 0 {
			if signalPID(pid, syscall.Signal(0)) == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

func signalPID(pid int, sig syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

func samePIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[int]bool, len(a))
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

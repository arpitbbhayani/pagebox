# Frequently Asked Questions (FAQ)

## Table of Contents
- [Why is Pagebox running on port 8732 instead of port 80?](#why-is-pagebox-running-on-port-8732-instead-of-port-80)
- [How do I restart the Pagebox daemon?](#how-do-i-restart-the-pagebox-daemon)
- [Troubleshooting: `systemctl --user` fails with "Failed to connect to bus" in WSL](#troubleshooting-systemctl---user-fails-with-failed-to-connect-to-bus-in-wsl)
- [How do I change the port Pagebox runs on?](#how-do-i-change-the-port-pagebox-runs-on)

---

### Why is Pagebox default port 8732?

**Q: Why does Pagebox default to port 8732 instead of 80?**

1. **Privileged Port Restrictions**: On Linux and macOS, port numbers below 1024 (such as port 80) are privileged. Running on port 80 requires administrative setup or root permissions (`sudo`).
2. **Safe, Unprivileged Operation**: Port `8732` is an unprivileged user port that runs out of the box without requiring sudo, special sysctl capability configurations, or administrative rights.

You can configure Pagebox to run on port 80 or any custom port by setting `port = <number>` in `~/.pagebox/config.toml` or passing `--port <number>`.

---

### How do I restart the Pagebox daemon?

**Q: How do I restart the background daemon after changing configuration or service files?**

#### On Linux / WSL (systemd):
```bash
systemctl --user restart pagebox
```

#### On macOS (launchd):
```bash
launchctl unload ~/Library/LaunchAgents/me.arpitbhayani.pagebox.plist
launchctl load ~/Library/LaunchAgents/me.arpitbhayani.pagebox.plist
```

#### Verifying:
Check the service status and logs:
```bash
pagebox daemon status
pagebox daemon logs
```
Or check active listening ports:
```bash
ss -tlpn | grep pagebox
```

---

### Troubleshooting: `systemctl --user` fails with "Failed to connect to bus" in WSL

**Q: Running `systemctl --user` inside WSL gives `Failed to connect to bus: No such file or directory`. How do I manage the daemon?**

In some WSL2 environments (especially with WSLg enabled), the user runtime directory (`/run/user/<uid>`) is shadowed, preventing subshells from establishing a connection to the systemd user D-Bus.

Because the Pagebox systemd service is configured with `Restart=always`, you can restart it directly:

1. **Reload systemd unit configuration**:
   ```bash
   kill -HUP $(pgrep -u $USER -x systemd)
   ```
2. **Terminate the running process**:
   ```bash
   pkill -f "pagebox serve"
   ```
3. Systemd will automatically restart the daemon with the updated configuration within 5 seconds.

---

### How do I change the port Pagebox runs on?

**Q: How do I customize the port?**

1. **In `config.toml`**:
   Edit `~/.pagebox/config.toml` and set your desired port:
   ```toml
   port = 8080
   ```
2. **Foreground Command**:
   Pass `--port` / `-p`:
   ```bash
   pagebox serve --port 8080
   ```
3. **Background Daemon**:
   After updating `port = <number>` in `~/.pagebox/config.toml`, simply reinstall the daemon to apply the change:
   ```bash
   pagebox daemon install
   ```

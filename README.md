# pagebox

A local-first document mirror, viewer, and synchronization engine. Pagebox serves HTML documents over HTTP with sub-millisecond local latency, applies a disciplined editorial design system, and keeps your files continuously synchronized with an Amazon S3 bucket.

---

## Why Pagebox?

Cloud documentation portals, internal wikis, and remote S3 browsers introduce unnecessary friction: they require active internet connectivity, add noticeable page load latencies, and isolate documentation from terminal-based tools, text editors, and local AI coding agents.

Pagebox bridges the gap between cloud storage and local workflows:

- **Local-First & Offline-Ready**: Every document lives on your local filesystem as plain HTML or Markdown. Read and navigate documents with zero latency even while completely offline.
- **Client-Side Markdown Rendering**: Seamlessly renders `.md` and `.markdown` documents to HTML purely in the browser with zero server-side parser overhead. Strictly preserves the Array UI editorial design system (`Assistant` body/headings, `Lora` italic quotes, `IBM Plex Mono` code/tables, code block copy buttons, and dark/light mode synchronization).
- **Continuous Background S3 Sync**: A lightweight daemon monitors your local directory with `fsnotify` and polls S3. Local edits are automatically debounced and pushed to the cloud using ETag-based optimistic locking.
- **Lazy Placeholder Fetching**: Pagebox indexes remote files without forcing you to download gigabytes of documents upfront. Uncached documents exist as lightweight placeholders and stream down on first browser access.
- **Editorial Systems Design (Array UI)**: Built on the Array UI design system—featuring mathematical 1px hairline borders, dense `Assistant` and `IBM Plex Mono` typography, tactile dark/light mode toggle, instant search with `/` shortcut, and iframe document preview.
- **Single Static Binary**: Written in Go with zero runtime dependencies. Runs as a foreground server or as an OS-managed user daemon (`systemd` on Linux/WSL, `launchd` on macOS).

---

## Installation

### Quick Install (macOS, Linux, WSL)

Install the latest binary using the universal install script:

```bash
curl -fsSL https://raw.githubusercontent.com/arpitbbhayani/pagebox/refs/heads/master/install.sh | sh
```

The script automatically detects your OS and architecture (`amd64`, `arm64`, `arm`, `386`, `riscv64`), places the executable in your `$PATH` (e.g. `~/.local/bin` or `/usr/local/bin`), and configures your shell environment (`~/.zshrc`, `~/.bashrc`, `~/.config/fish/config.fish`).

### Build from Source

Requires Go 1.22+:

```bash
git clone https://github.com/arpitbbhayani/pagebox.git
cd pagebox
make build
install -d ~/.local/bin && install pagebox ~/.local/bin/
```

### Self-Update

To update an existing installation to the latest release:

```bash
pagebox update
# or: pagebox --update
```

Check the installed version at any time:

```bash
pagebox version
# or: pagebox --version / pagebox -v
```

---

## Quick Start

### 1. Initialize Configuration

Run the interactive setup wizard to configure your documents folder, AWS credentials, and S3 bucket:

```bash
pagebox init
```

Configuration is stored in `~/.pagebox/config.toml`:

```toml
backend = "s3"
bucket = "my-docs-bucket"
prefix = ""
region = "us-east-1"
aws_profile = "default"
docs_dir = "/home/user/pagebox"
host = "pagebox.local"
port = 8730
bind = "0.0.0.0"
poll_interval = "5m"
push_debounce = "10s"
```

### 2. Run the Server

Start Pagebox in the foreground:

```bash
pagebox serve
```

Open your browser at [http://localhost:8730](http://localhost:8730) (or `http://pagebox.local:8730`).

### 3. Install as a Background Service

Run Pagebox continuously in the background as an OS service:

```bash
pagebox daemon install
```

- **Linux / WSL**: Installs and enables a systemd user service (`~/.config/systemd/user/pagebox.service`).
- **macOS**: Installs and loads a launchd agent (`~/Library/LaunchAgents/me.arpitbhayani.pagebox.plist`).

Check service status and tail logs:

```bash
pagebox daemon status
pagebox daemon logs
```

---

## Features

### Two-Way Synchronization & Concurrency Control

Pagebox maintains a local index (`~/.pagebox/index.json`) that tracks every document against its remote ETag.

| State | Description |
| :--- | :--- |
| **`cached`** | In sync with remote storage. Served immediately from disk. |
| **`stale`** | Remote object changed in S3. Lazy-fetches updated content on next read. |
| **`dirty`** | Local file modified. Debounced push to S3 scheduled automatically. |
| **`diverged`** | Both local and remote versions changed independently. Push rejected to prevent data loss. |
| **`placeholder`** | Remote object recognized in index, downloaded on demand when opened. |

- **Debounced Pushes**: Successive saves in text editors or IDEs are debounced (default `10s`) before sending a `PutObject` request.
- **Precondition Checks**: S3 writes verify `If-Match: <base-etag>` to eliminate race conditions between multiple writers.
- **Conflict Isolation**: If a file diverges, Pagebox marks it as `diverged` in `pagebox status` instead of silently overwriting your local copy.

### Editorial Reader & Document Viewer

- **Dense Information Architecture**: Document table with live search filtering, status badges, file sizes, and relative sync timestamps.
- **Keyboard-First Navigation**: Press `/` anywhere on the index to focus the search bar. Press `Escape` to clear.
- **Top Bar & Iframe Isolation**: Documents render inside a clean viewport iframe with a `/pagebox` navigation bar and back arrow (`← /pagebox`) returning directly to the index.
- **Automatic Theme Synchronization**: Dark and light modes seamlessly sync between the parent shell and embedded document.
- **Direct Raw Access**: Add `?raw=1` to any document URL to bypass the viewer shell and inspect raw HTML directly.

### Health Diagnostics & Self-Healing

Run the doctor command to verify your environment:

```bash
pagebox doctor
```

Validates:
- AWS credential validity and profile resolution
- S3 bucket accessibility, read/write permissions, and versioning state
- Local document folder read/write permissions
- Port availability and listener binding
- Background daemon service health and systemd/launchd socket connectivity

---

## Command Reference

| Command | Description |
| :--- | :--- |
| `pagebox init` | Interactive configuration setup wizard |
| `pagebox add [paths...]` | Add untracked and modified files from configured docs folder to index |
| `pagebox serve` | Run HTTP server and sync loop in foreground |
| `pagebox list` | Display paginated list of all tracked documents and states |
| `pagebox status` | Show dirty, diverged, and stale document summary |
| `pagebox pull` | Pull latest updates from S3 (use `--force` to overwrite local changes) |
| `pagebox push` | Immediately push pending local modifications to S3 |
| `pagebox delete <doc-id>` | Delete document locally and remove corresponding key from S3 |
| `pagebox daemon install` | Install and start background OS service |
| `pagebox daemon status` | Inspect background daemon status |
| `pagebox daemon logs` | Stream live daemon service logs |
| `pagebox daemon uninstall` | Stop and remove background OS service |
| `pagebox doctor` | Run environment and connectivity diagnostics |
| `pagebox update` | Check for and install latest Pagebox binary release |
| `pagebox version` | Print current version, OS, and machine architecture |

---

## WSL & Windows Setup

Pagebox includes native detection for Windows Subsystem for Linux (WSL).

When running inside WSL:
1. Open documents directly in your Windows browser at [http://localhost:8730](http://localhost:8730).
2. To use `http://pagebox.local:8730`, add the loopback mapping to `C:\Windows\System32\drivers\etc\hosts`:
   ```text
   127.0.0.1 pagebox.local
   ```

For advanced configuration, custom ports, and service management troubleshooting, consult the [FAQ](docs/faq.md).

---

## Development

```bash
# Run unit and integration tests
make test

# Compile local binary
make build

# Cross-compile release binaries across all architectures into dist/
make dist

# Prepare and tag a release
make publish 0.2.0
```

---

## License

[MIT License](LICENSE) © Arpit Bhayani

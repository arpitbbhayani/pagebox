# pagebox

A local daemon and CLI that serves a folder of HTML documents over HTTP and keeps them in sync with an S3 bucket.

## installation

### quick install

```bash
curl -fsSL https://raw.githubusercontent.com/arpitbbhayani/pagebox/master/install.sh | sh
```

### from source

```bash
git clone https://github.com/arpitbbhayani/pagebox.git
cd pagebox
make
```

## get started

```bash
pagebox init
pagebox daemon install
```

## commands

- `pagebox list` — view all documents
- `pagebox pull` — fetch changes from S3
- `pagebox push` — push local changes to S3
- `pagebox status` — view unpushed or diverged changes
- `pagebox daemon status` — check background service status
- `pagebox daemon logs` — tail live background service logs
- `pagebox uninstall` — stop and uninstall the background service

Use `pagebox help` to see all available commands.

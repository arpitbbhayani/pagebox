# pagebox

A local daemon and CLI that serves a folder of HTML documents over HTTP and keeps them in sync with an S3 bucket.

## get started

```bash
go build -o pb ./cmd/pagebox

./pb init
./pb daemon install
```

## commands

- `pb list` — view all documents
- `pb pull` — fetch changes from S3
- `pb push` — push local changes to S3
- `pb status` — view unpushed or diverged changes
- `pb doctor` — check system and AWS configuration

Use `pb help` to see all available commands.

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var pullCmd = &cobra.Command{
	Use:   "pull [id]",
	Short: "Update from S3",
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")

		cfg, err := pbconfig.Load()
		if err != nil {
			ui.PrintErr("failed to load config", err.Error(), nil)
			os.Exit(1)
		}

		store, err := storage.New(cfg)
		if err != nil {
			ui.PrintErr("failed to init storage", err.Error(), nil)
			os.Exit(1)
		}

		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}

		if len(args) > 0 {
			pullOne(args[0], force, cfg, store, idx)
		} else {
			pullStale(cfg, store, idx)
		}
	},
}

func pullOne(id string, force bool, cfg pbconfig.Config, store storage.Storage, idx *index.Store) {
	entry, ok := idx.Get(id)
	if !ok {
		ui.PrintErr("document not found in index", id, nil)
		os.Exit(1)
	}

	if entry.State == index.StateDirty || entry.State == index.StateDiverged {
		if !force {
			ui.PrintErr("refusing to overwrite modified file", id, nil)
			ui.Hint("use --force to overwrite and backup local copy", nil)
			ui.Command(fmt.Sprintf("pagebox pull %s --force", id), nil)
			os.Exit(1)
		}
		backupFile(cfg.DocsDir, id)
	}

	sp := ui.NewSpinner(fmt.Sprintf("Downloading %s", id))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body, etag, err := store.Get(ctx, entry.Key, entry.RemoteETag)
	if err != nil {
		sp.Stop(false)
		ui.PrintErr("download failed", err.Error(), nil)
		os.Exit(2)
	}
	defer body.Close()

	dest := filepath.Join(cfg.DocsDir, entry.Key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		sp.Stop(false)
		ui.PrintErr("failed to create directory", err.Error(), nil)
		os.Exit(1)
	}

	f, err := os.Create(dest)
	if err != nil {
		sp.Stop(false)
		ui.PrintErr("failed to create file", err.Error(), nil)
		os.Exit(1)
	}

	written, err := io.Copy(f, body)
	f.Close()
	
	if err != nil {
		sp.Stop(false)
		ui.PrintErr("write failed", err.Error(), nil)
		os.Exit(1)
	}

	idx.Update(id, func(e *index.Entry) {
		e.State = index.StateCached
		e.BaseETag = etag
		e.RemoteETag = etag
		e.Size = written
		e.LastSyncedAt = time.Now()
	})

	sp.Stop(true)
	ui.PrintOk("pulled document", id, nil)
}

func pullStale(cfg pbconfig.Config, store storage.Storage, idx *index.Store) {
	entries := idx.List()
	staleCount := 0
	
	for _, e := range entries {
		if e.State == index.StateStale {
			staleCount++
			pullOne(e.ID, false, cfg, store, idx)
		}
	}

	if staleCount == 0 {
		ui.PrintInfo("no stale documents to pull", "", nil)
	}
}

func backupFile(docsDir, id string) {
	home, _ := os.UserHomeDir()
	ts := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(home, ".pagebox", "backups", ts)
	os.MkdirAll(backupDir, 0755)

	srcPath := filepath.Join(docsDir, id+".html")
	destPath := filepath.Join(backupDir, id+".html")

	src, err := os.Open(srcPath)
	if err != nil {
		return
	}
	defer src.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return
	}
	defer dest.Close()

	io.Copy(dest, src)
	ui.PrintInfo("backed up local copy", destPath, nil)
}



package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/server"
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

		failed := 0
		if len(args) > 0 {
			pullOne(args[0], force, cfg, store, idx)
		} else {
			failed = pullAll(force, cfg, store, idx)
		}

		if err := server.WriteIndexFile(cfg, idx); err != nil {
			ui.PrintWarn("failed to update index.html", err.Error(), nil)
		}
		_ = server.WriteRobotsFile(cfg)
		if failed > 0 {
			os.Exit(2)
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
		backupFile(cfg.DocsDir, entry.Key)
	}

	if err := downloadDoc(entry, cfg, store, idx); err != nil {
		os.Exit(2)
	}
}

// downloadDoc fetches a document from S3 into the docs directory and records it as cached.
// Errors are reported to the user before being returned.
func downloadDoc(entry index.Entry, cfg pbconfig.Config, store storage.Storage, idx *index.Store) error {
	id := entry.ID
	sp := ui.NewSpinner(fmt.Sprintf("Downloading %s", id))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fail := func(msg string, err error) error {
		sp.Stop(false)
		ui.PrintErr(msg, err.Error(), nil)
		return err
	}

	body, etag, err := store.Get(ctx, entry.Key, entry.RemoteETag)
	if err != nil {
		return fail("download failed", err)
	}
	defer body.Close()

	dest := filepath.Join(cfg.DocsDir, filepath.FromSlash(entry.Key))
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fail("failed to create directory", err)
	}

	tmp := dest + ".pagebox-tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fail("failed to create file", err)
	}

	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(f, h), body)
	f.Close()
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		os.Remove(tmp)
		return fail("write failed", err)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	idx.Update(id, func(e *index.Entry) {
		e.ID = id
		e.Key = entry.Key
		e.State = index.StateCached
		e.BaseETag = etag
		e.RemoteETag = etag
		e.LocalSHA256 = sum
		e.Size = written
		e.LastSyncedAt = time.Now()
	})

	sp.Stop(true)
	ui.PrintOk("pulled document", id, nil)
	return nil
}

// pullAll downloads every remote document that is missing or stale locally and returns the failure count.
func pullAll(force bool, cfg pbconfig.Config, store storage.Storage, idx *index.Store) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sp := ui.NewSpinner("Checking remote storage")
	objects, err := store.List(ctx)
	if err != nil {
		sp.Stop(false)
		ui.PrintErr("failed to list remote objects", err.Error(), nil)
		os.Exit(1)
	}
	sp.Stop(true)

	remote := remoteDocs(objects)
	ids := make([]string, 0, len(remote))
	for id := range remote {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	pulled, skipped, failed := 0, 0, 0

	for _, id := range ids {
		rem := remote[id]
		e, ok := idx.Get(id)
		if !ok {
			e = index.Entry{ID: id, Key: rem.Key}
		}
		e.ID, e.Key = id, rem.Key
		e.RemoteETag = rem.ETag

		st, sha := compareDoc(cfg.DocsDir, e, rem)
		switch st {
		case statusClean:
			idx.Update(id, func(entry *index.Entry) {
				entry.ID, entry.Key = id, rem.Key
				entry.State = index.StateCached
				entry.BaseETag = rem.ETag
				entry.RemoteETag = rem.ETag
				entry.LocalSHA256 = sha
				entry.Size = rem.Size
			})
			continue
		case statusDirty:
			ui.PrintWarn("kept local changes (push to upload)", id, nil)
			skipped++
			continue
		case statusDiverged:
			if !force {
				ui.PrintWarn("skipped diverged document (use --force to overwrite)", id, nil)
				skipped++
				continue
			}
			backupFile(cfg.DocsDir, rem.Key)
		}

		if err := downloadDoc(e, cfg, store, idx); err != nil {
			failed++
			continue
		}
		pulled++
	}

	if failed > 0 {
		ui.PrintErr("some documents failed to download", fmt.Sprintf("%d failed", failed), nil)
	}
	if pulled == 0 && skipped == 0 && failed == 0 {
		ui.PrintOk("everything up to date", "", nil)
	}
	return failed
}

func backupFile(docsDir, key string) {
	home, _ := os.UserHomeDir()
	ts := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(home, ".pagebox", "backups", ts)

	srcPath := filepath.Join(docsDir, filepath.FromSlash(key))
	destPath := filepath.Join(backupDir, filepath.FromSlash(key))
	os.MkdirAll(filepath.Dir(destPath), 0755)

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



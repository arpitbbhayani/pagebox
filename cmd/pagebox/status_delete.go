package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	
	"github.com/spf13/cobra"
	
	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Compare local files with S3: dirty, diverged, stale, and missing documents",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := pbconfig.Load()
		if err != nil {
			ui.PrintErr("failed to load config", err.Error(), nil)
			os.Exit(1)
		}

		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}

		store, err := storage.New(cfg)
		if err != nil {
			ui.PrintErr("failed to init storage", err.Error(), nil)
			os.Exit(1)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		sp := ui.NewSpinner("Comparing local files with remote")
		objects, err := store.List(ctx)
		if err != nil {
			sp.Stop(false)
			ui.PrintErr("failed to list remote objects", err.Error(), nil)
			os.Exit(1)
		}
		sp.Stop(true)

		remote := remoteDocs(objects)
		var dirty, diverged, stale, missing, gone []index.Entry
		clean := 0

		for id, rem := range remote {
			e, _ := idx.Get(id)
			e.ID, e.Key = id, rem.Key
			st, sha := compareDoc(cfg.DocsDir, e, rem)

			switch st {
			case statusClean:
				clean++
			case statusRemoteOnly:
				missing = append(missing, e)
			case statusStale:
				stale = append(stale, e)
			case statusDirty:
				dirty = append(dirty, e)
			case statusDiverged:
				diverged = append(diverged, e)
			}

			// Persist what we learned so the index and UI agree with reality.
			idx.Update(id, func(entry *index.Entry) {
				entry.ID, entry.Key = id, rem.Key
				entry.RemoteETag = rem.ETag
				switch st {
				case statusClean:
					entry.State = index.StateCached
					entry.BaseETag = rem.ETag
					entry.LocalSHA256 = sha
					entry.Size = rem.Size
				case statusRemoteOnly:
					entry.State = index.StatePlaceholder
				case statusStale:
					entry.State = index.StateStale
				case statusDirty:
					entry.State = index.StateDirty
					entry.LocalSHA256 = sha
				case statusDiverged:
					entry.State = index.StateDiverged
					entry.LocalSHA256 = sha
				}
			})
		}

		// Tracked locally but absent from S3: new files awaiting push, or removed remotely.
		for _, e := range idx.List() {
			if _, ok := remote[e.ID]; ok {
				continue
			}
			localPath := filepath.Join(cfg.DocsDir, filepath.FromSlash(e.Key))
			if fi, err := os.Stat(localPath); err == nil && !fi.IsDir() && (e.BaseETag == "" || e.State == index.StateNew) {
				dirty = append(dirty, index.Entry{ID: e.ID, Key: e.Key, State: index.StateNew})
			} else {
				gone = append(gone, e)
			}
		}
		sort.Slice(dirty, func(i, j int) bool { return dirty[i].ID < dirty[j].ID })
		for _, l := range [][]index.Entry{diverged, stale, missing, gone} {
			sort.Slice(l, func(i, j int) bool { return l[i].ID < l[j].ID })
		}

		// Find untracked files in cfg.DocsDir
		var untracked []string
		if cfg.DocsDir != "" {
			_ = filepath.Walk(cfg.DocsDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					if strings.HasPrefix(info.Name(), ".") && path != cfg.DocsDir {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasPrefix(info.Name(), ".") {
					return nil
				}

				rel, err := filepath.Rel(cfg.DocsDir, path)
				if err != nil {
					return nil
				}
				slashRel := filepath.ToSlash(rel)
				if index.IsIgnoredDoc(index.TrimDocExt(slashRel), slashRel) {
					return nil
				}
				id := index.TrimDocExt(slashRel)

				if _, ok := idx.Get(id); !ok {
					untracked = append(untracked, slashRel)
				}
				return nil
			})
		}

		totalActionable := len(dirty) + len(diverged) + len(stale) + len(missing) + len(gone) + len(untracked)
		if totalActionable == 0 {
			ui.PrintOk("everything is up to date", fmt.Sprintf("%d documents in sync", clean), nil)
			os.Exit(0)
		}

		if len(diverged) > 0 {
			ui.Heading("diverged", &[]int{len(diverged)}[0], nil)
			for _, e := range diverged {
				ui.PrintErr(e.ID, "remote changed since your last sync", nil)
			}
			ui.Hint("resolve diverged documents before pushing:", nil)
			ui.Command("pagebox pull <id> --force", nil)
		}

		if len(stale) > 0 {
			ui.Heading("stale", &[]int{len(stale)}[0], nil)
			for _, e := range stale {
				ui.PrintWarn(e.ID, "newer version on remote", nil)
			}
			ui.Hint("update stale documents:", nil)
			ui.Command("pagebox pull", nil)
		}

		if len(missing) > 0 {
			ui.Heading("not downloaded", &[]int{len(missing)}[0], nil)
			for _, e := range missing {
				ui.PrintStep(e.ID, "on remote only", nil)
			}
			ui.Hint("download missing documents:", nil)
			ui.Command("pagebox pull", nil)
		}

		if len(gone) > 0 {
			ui.Heading("removed remotely", &[]int{len(gone)}[0], nil)
			for _, e := range gone {
				ui.PrintWarn(e.ID, "no longer on remote", nil)
			}
		}

		if len(dirty) > 0 {
			ui.Heading("dirty", &[]int{len(dirty)}[0], nil)
			for _, e := range dirty {
				stateDetail := "modified locally"
				if e.State == index.StateNew {
					stateDetail = "created locally"
				}
				ui.PrintStep(e.ID, stateDetail, nil)
			}
			ui.Hint("push local changes:", nil)
			ui.Command("pagebox push", nil)
		}

		if len(untracked) > 0 {
			ui.Heading("untracked", &[]int{len(untracked)}[0], nil)
			for _, u := range untracked {
				ui.PrintStep(u, "not in index", nil)
			}
			ui.Hint("add files to index:", nil)
			ui.Command("pagebox add", nil)
		}
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Explicitly delete a document from S3 and locally",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		
		idx, err := index.NewStore()
		if err != nil {
			ui.PrintErr("failed to open index", err.Error(), nil)
			os.Exit(1)
		}
		
		entry, ok := idx.Get(id)
		if !ok {
			ui.PrintErr("document not found in index", id, nil)
			os.Exit(1)
		}

		ui.PrintWarn(fmt.Sprintf("this will permanently delete '%s' locally and move it to trash remotely", id), "", nil)
		
		confirmed, err := ui.Confirm(fmt.Sprintf("delete %s?", id), false)
		if err != nil || !confirmed {
			if err != nil {
				ui.PrintInfo("cancelled", err.Error(), nil)
			} else {
				ui.PrintInfo("cancelled", "", nil)
			}
			os.Exit(0)
		}

		cfg, _ := pbconfig.Load()
		store, _ := storage.New(cfg)

		sp := ui.NewSpinner(fmt.Sprintf("Deleting %s", id))
		
		// Remove from remote (moves to _trash)
		if err := store.Delete(context.Background(), entry.Key); err != nil {
			sp.Stop(false)
			ui.PrintErr("failed to delete from remote", err.Error(), nil)
			os.Exit(2)
		}

		// Remove from index
		idx.Delete(id)

		// Remove local file
		localPath := filepath.Join(cfg.DocsDir, filepath.FromSlash(entry.Key))
		os.Remove(localPath)

		updateAndPushIndexHTML(cfg, store, idx)

		sp.Stop(true)
		ui.PrintOk("deleted", id, nil)
	},
}

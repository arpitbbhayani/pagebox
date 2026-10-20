package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	
	"github.com/spf13/cobra"
	
	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/server"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Summary of dirty, diverged, and stale documents",
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

		entries := idx.List()
		var dirty, diverged, stale, missing []index.Entry
		
		for _, e := range entries {
			switch e.State {
			case index.StateDirty, index.StateNew:
				dirty = append(dirty, e)
			case index.StateDiverged:
				diverged = append(diverged, e)
			case index.StateStale:
				stale = append(stale, e)
			case index.StatePlaceholder:
				// The PRD mentions that a key that disappeared from S3 is flagged in 'status'
				// We don't have a specific explicit missing state yet, but we'll show actionable items
			}
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

		totalActionable := len(dirty) + len(diverged) + len(stale) + len(missing) + len(untracked)
		if totalActionable == 0 {
			ui.PrintOk("everything is up to date", "", nil)
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

		_ = server.WriteIndexFile(cfg, idx)

		sp.Stop(true)
		ui.PrintOk("deleted", id, nil)
	},
}

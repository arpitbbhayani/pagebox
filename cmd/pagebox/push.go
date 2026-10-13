package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var pushCmd = &cobra.Command{
	Use:   "push [id]",
	Short: "Push local changes",
	Run: func(cmd *cobra.Command, args []string) {
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
			pushOne(args[0], cfg, store, idx)
		} else {
			pushDirty(cfg, store, idx)
		}
	},
}

func pushOne(id string, cfg pbconfig.Config, store storage.Storage, idx *index.Store) {
	entry, ok := idx.Get(id)
	if !ok {
		ui.PrintErr("document not found in index", id, nil)
		os.Exit(1)
	}

	if entry.State != index.StateDirty && entry.State != index.StateNew {
		ui.PrintInfo("nothing to push", fmt.Sprintf("state is %s", entry.State), nil)
		return
	}

	srcPath := filepath.Join(cfg.DocsDir, entry.Key)
	f, err := os.Open(srcPath)
	if err != nil {
		ui.PrintErr("could not open local file", err.Error(), nil)
		os.Exit(1)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		ui.PrintErr("could not stat local file", err.Error(), nil)
		os.Exit(1)
	}

	sp := ui.NewSpinner(fmt.Sprintf("Pushing %s", id))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use conditional write via ETags
	expectedEtag := entry.BaseETag
	if entry.State == index.StateNew {
		expectedEtag = ""
	}

	newEtag, err := store.Put(ctx, entry.Key, f, stat.Size(), expectedEtag)
	if err != nil {
		sp.Stop(false)
		if errors.Is(err, storage.ErrPreconditionFailed) {
			idx.Update(id, func(e *index.Entry) {
				e.State = index.StateDiverged
			})
			ui.PrintErr("remote changed since your last sync", "", nil)
			ui.Command(fmt.Sprintf("pagebox pull %s", id), nil)
			os.Exit(3)
		}
		ui.PrintErr("push failed", err.Error(), nil)
		os.Exit(2)
	}

	idx.Update(id, func(e *index.Entry) {
		e.State = index.StateCached
		e.BaseETag = newEtag
		e.RemoteETag = newEtag
		e.LastSyncedAt = time.Now()
	})

	sp.Stop(true)
	ui.PrintOk("pushed document", id, nil)
}

func pushDirty(cfg pbconfig.Config, store storage.Storage, idx *index.Store) {
	entries := idx.List()
	pushedCount := 0
	
	for _, e := range entries {
		if e.State == index.StateDirty || e.State == index.StateNew {
			pushedCount++
			pushOne(e.ID, cfg, store, idx)
		}
	}

	if pushedCount == 0 {
		ui.PrintInfo("no dirty documents to push", "", nil)
	}
}



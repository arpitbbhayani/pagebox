package main

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
	"github.com/arpitbhayani/pagebox/internal/ui"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Paginated list of documents",
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

		if cfg.IsS3Configured() || cfg.Backend == "local" {
			store, err := storageNew(cfg)
			if err != nil {
				ui.PrintErr("failed to init storage", err.Error(), nil)
				os.Exit(1)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			objects, err := store.List(ctx)
			if err != nil {
				ui.PrintErr("failed to list remote objects", err.Error(), nil)
				os.Exit(1)
			}

			syncRemoteToIndex(idx, objects)
		}

		entries := idx.List()
		
		if len(entries) == 0 {
			ui.PrintInfo("no documents found", "", nil)
			return
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ID < entries[j].ID
		})

		ui.Heading("documents", nil, nil)

		var rows [][]interface{}
		for _, e := range entries {
			sizeStr := humanize.Bytes(uint64(e.Size))
			if e.State == index.StatePlaceholder {
				sizeStr = "-"
			}
			rows = append(rows, []interface{}{e.ID, e.State, sizeStr})
		}

		// Apply semantic colors to the state column (column 1)
		colorFn := func(cell string) string {
			switch index.DocumentState(cell) {
			case index.StateCached:
				return ui.Ok
			case index.StatePlaceholder:
				return ui.Info
			case index.StateStale:
				return ui.Warn
			case index.StateDirty, index.StateDiverged:
				return ui.Err
			case index.StateNew:
				return ui.Accent
			default:
				return ""
			}
		}

		// Color column index 1, dim column 2 (size)
		ui.Table(rows, &ui.ColorCol{Index: 1, Fn: colorFn}, []int{2}, nil)
	},
}

func isIgnoredRemoteKey(key string) bool {
	if key == "" || strings.HasSuffix(key, "/") || strings.HasPrefix(key, "_trash/") || key == "_trash" {
		return true
	}
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
}

func syncRemoteToIndex(idx *index.Store, objects []storage.Object) {
	remoteMap := make(map[string]storage.Object)
	for _, obj := range objects {
		key := strings.TrimPrefix(obj.Key, "/")
		if isIgnoredRemoteKey(key) {
			continue
		}
		id := index.TrimDocExt(key)
		remoteMap[id] = obj
	}

	for _, e := range idx.List() {
		rem, exists := remoteMap[e.ID]
		if !exists {
			rem, exists = remoteMap[index.TrimDocExt(e.Key)]
		}
		if !exists {
			continue
		}
		delete(remoteMap, e.ID)
		delete(remoteMap, index.TrimDocExt(e.Key))

		_ = idx.Update(e.ID, func(entry *index.Entry) {
			entry.LastPolledAt = time.Now()
			if entry.RemoteETag != rem.ETag {
				entry.RemoteETag = rem.ETag
				if entry.State == index.StateCached {
					entry.State = index.StateStale
				} else if entry.State == index.StateDirty || entry.State == index.StateNew {
					entry.State = index.StateDiverged
				}
			}
		})
	}

	for id, rem := range remoteMap {
		_ = idx.Update(id, func(entry *index.Entry) {
			entry.ID = id
			entry.Key = rem.Key
			entry.RemoteETag = rem.ETag
			entry.LastPolledAt = time.Now()
			if entry.State == "" {
				entry.State = index.StatePlaceholder
				entry.Size = 0
			} else if entry.RemoteETag != rem.ETag {
				if entry.State == index.StateCached {
					entry.State = index.StateStale
				} else if entry.State == index.StateDirty || entry.State == index.StateNew {
					entry.State = index.StateDiverged
				}
			}
		})
	}
}



package main

import (
	"context"
	"fmt"
	"os"
	"path"
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

var mvCmd = &cobra.Command{
	Use:   "mv <source>... <destination>",
	Short: "Rename or move documents and folders locally and on S3",
	Long: `Renames a document or moves documents and folders, like mv(1).

A source is a document (by id or file name) or a folder. If the destination is
an existing folder, or ends with "/", sources are moved into it; otherwise the
single source is renamed to the destination. A destination without a document
extension keeps the source's extension.

S3 has no rename, so each document is copied to its new key and the old key is
moved to _trash.`,
	Args: cobra.MinimumNArgs(2),
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

		srcs, dst := args[:len(args)-1], args[len(args)-1]
		entries := idx.List()
		moves, err := planMoves(entries, srcs, dst, destIsDir(cfg.DocsDir, entries, dst))
		if err != nil {
			ui.PrintErr("cannot move", err.Error(), nil)
			os.Exit(1)
		}

		failed := 0
		for _, m := range moves {
			if err := moveDoc(m, force, cfg, store, idx); err != nil {
				ui.PrintErr("move failed", fmt.Sprintf("%s: %v", m.from.ID, err), nil)
				failed++
				continue
			}
			ui.PrintOk("moved", fmt.Sprintf("%s → %s", m.from.Key, m.toKey), nil)
		}

		updateAndPushIndexHTML(cfg, store, idx)
		if failed > 0 {
			os.Exit(2)
		}
	},
}

// move is a planned relocation of one document to a new key.
type move struct {
	from  index.Entry
	toKey string
}

// destIsDir reports whether dst names a folder rather than a new file name.
func destIsDir(docsDir string, entries []index.Entry, dst string) bool {
	if strings.HasSuffix(dst, "/") {
		return true
	}
	clean := cleanKey(dst)
	if clean == "" {
		return true
	}
	if fi, err := os.Stat(filepath.Join(docsDir, filepath.FromSlash(clean))); err == nil && fi.IsDir() {
		return true
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Key, clean+"/") {
			return true
		}
	}
	return false
}

func cleanKey(p string) string {
	p = path.Clean("/" + filepath.ToSlash(strings.TrimSpace(p)))
	return strings.TrimPrefix(p, "/")
}

// planMoves resolves sources (documents or folders) against the index and computes
// the destination key of every affected document, rejecting conflicts.
func planMoves(entries []index.Entry, srcs []string, dst string, dstIsDir bool) ([]move, error) {
	if len(srcs) > 1 && !dstIsDir {
		return nil, fmt.Errorf("destination %q is not a folder", dst)
	}
	dstKey := cleanKey(dst)

	var moves []move
	for _, src := range srcs {
		s := cleanKey(src)
		if s == "" {
			return nil, fmt.Errorf("invalid source %q", src)
		}

		var file *index.Entry
		for i := range entries {
			e := entries[i]
			if e.Key == s || e.ID == s || index.TrimDocExt(e.Key) == index.TrimDocExt(s) {
				file = &entries[i]
				break
			}
		}

		if file != nil {
			target := dstKey
			if dstIsDir {
				target = path.Join(dstKey, path.Base(file.Key))
			} else if index.TrimDocExt(target) == target {
				target += path.Ext(file.Key)
			}
			moves = append(moves, move{from: *file, toKey: target})
			continue
		}

		prefix := s + "/"
		found := false
		base := dstKey
		if dstIsDir {
			base = path.Join(dstKey, path.Base(s))
		}
		if base == s || strings.HasPrefix(base+"/", prefix) {
			return nil, fmt.Errorf("cannot move %q into itself", src)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Key, prefix) {
				moves = append(moves, move{from: e, toKey: path.Join(base, strings.TrimPrefix(e.Key, prefix))})
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no document or folder named %q", src)
		}
	}

	moving := make(map[string]bool, len(moves))
	for _, m := range moves {
		moving[m.from.Key] = true
	}
	taken := make(map[string]string)
	for _, e := range entries {
		if !moving[e.Key] {
			taken[index.TrimDocExt(e.Key)] = e.Key
		}
	}
	seen := make(map[string]bool)
	for _, m := range moves {
		id := index.TrimDocExt(m.toKey)
		if m.toKey == "" || strings.HasPrefix(m.toKey, "..") || index.IsIgnoredDoc(id, m.toKey) {
			return nil, fmt.Errorf("invalid destination %q", m.toKey)
		}
		if other, ok := taken[id]; ok {
			return nil, fmt.Errorf("%s already exists", other)
		}
		if seen[id] {
			return nil, fmt.Errorf("multiple documents would be moved to %s", m.toKey)
		}
		seen[id] = true
	}

	sort.Slice(moves, func(i, j int) bool { return moves[i].from.Key < moves[j].from.Key })
	return moves, nil
}

// moveDoc relocates one document: copy to the new S3 key, re-key the index, rename
// the local file, then trash the old S3 key. Failures before the rename leave the
// original untouched.
func moveDoc(m move, force bool, cfg pbconfig.Config, store storage.Storage, idx *index.Store) error {
	e := m.from
	switch e.State {
	case index.StateDiverged:
		if !force {
			return fmt.Errorf("diverged from remote (use --force to move your local copy)")
		}
	case index.StatePlaceholder, index.StateStale:
		// Local copy is missing or outdated: fetch the remote version before moving it.
		if err := downloadDoc(e, cfg, store, idx); err != nil {
			return err
		}
		e, _ = idx.Get(e.ID)
	}

	oldPath := filepath.Join(cfg.DocsDir, filepath.FromSlash(e.Key))
	newPath := filepath.Join(cfg.DocsDir, filepath.FromSlash(m.toKey))
	newID := index.TrimDocExt(m.toKey)

	f, err := os.Open(oldPath)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	sha, _ := hashLocal(oldPath)

	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("%s already exists locally", m.toKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	newETag := ""
	remote := e.BaseETag != "" || e.RemoteETag != ""
	if remote {
		if newETag, err = store.Put(ctx, m.toKey, f, fi.Size(), ""); err != nil {
			return fmt.Errorf("upload to %s failed: %w", m.toKey, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	// Register the new entry before the rename so the daemon's file watcher sees it as tracked.
	idx.Update(newID, func(n *index.Entry) {
		*n = e
		n.ID, n.Key = newID, m.toKey
		n.LocalSHA256 = sha
		n.Size = fi.Size()
		if remote {
			n.State = index.StateCached
			n.BaseETag, n.RemoteETag = newETag, newETag
			n.LastSyncedAt = time.Now()
		} else {
			n.State = index.StateNew
		}
	})
	if err := os.Rename(oldPath, newPath); err != nil {
		if newID != e.ID {
			idx.Delete(newID)
		} else {
			idx.Update(e.ID, func(n *index.Entry) { *n = e })
		}
		return err
	}
	if newID != e.ID {
		idx.Delete(e.ID)
	}
	pruneEmptyDirs(cfg.DocsDir, filepath.Dir(oldPath))

	if remote {
		if err := store.Delete(ctx, e.Key); err != nil && err != storage.ErrNotFound {
			ui.PrintWarn("moved, but could not remove old remote key", fmt.Sprintf("%s: %v", e.Key, err), nil)
		}
	}
	return nil
}

// pruneEmptyDirs removes dir and its parents while they are empty, stopping at root.
func pruneEmptyDirs(root, dir string) {
	for dir != root && strings.HasPrefix(dir, root) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

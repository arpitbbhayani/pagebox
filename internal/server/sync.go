package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

type SyncLoop struct {
	cfg     pbconfig.Config
	idx     *index.Store
	store   storage.Storage
	watcher *fsnotify.Watcher
	mu      sync.Mutex
	pending map[string]time.Time
}

func NewSyncLoop(cfg pbconfig.Config, idx *index.Store, store storage.Storage) (*SyncLoop, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &SyncLoop{
		cfg:     cfg,
		idx:     idx,
		store:   store,
		watcher: watcher,
		pending: make(map[string]time.Time),
	}, nil
}

func (s *SyncLoop) Start(ctx context.Context) error {
	defer s.watcher.Close()

	if err := os.MkdirAll(s.cfg.DocsDir, 0755); err != nil {
		return err
	}

	if err := s.setupWatcher(s.cfg.DocsDir); err != nil {
		return err
	}

	pollInterval, err := time.ParseDuration(s.cfg.PollInterval)
	if err != nil {
		pollInterval = 5 * time.Minute
	}
	
	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	debounceTicker := time.NewTicker(1 * time.Second)
	defer debounceTicker.Stop()

	// Initial poll
	go s.pollRemote(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
			go s.pollRemote(ctx)
		case event, ok := <-s.watcher.Events:
			if !ok {
				return nil
			}
			s.handleFSEvent(event)
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)
		case <-debounceTicker.C:
			s.processPending(ctx)
		}
	}
}

func (s *SyncLoop) setupWatcher(dir string) error {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden dirs (like .git, .pagebox)
			if strings.HasPrefix(info.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return s.watcher.Add(path)
		}
		return nil
	})
	return err
}

func (s *SyncLoop) handleFSEvent(event fsnotify.Event) {
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		_ = s.watcher.Add(event.Name)
		return
	}

	rel, err := filepath.Rel(s.cfg.DocsDir, event.Name)
	if err != nil {
		return
	}
	slashRel := filepath.ToSlash(rel)
	if strings.HasPrefix(slashRel, ".") || strings.Contains(slashRel, "/.") {
		return
	}
	
	id := index.TrimDocExt(slashRel)

	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		s.markDirtyAndSchedule(id, slashRel)
	}
}

func (s *SyncLoop) markDirtyAndSchedule(id, key string) {
	s.idx.Update(id, func(e *index.Entry) {
		if e.ID == "" {
			e.ID = id
			e.Key = key
			e.State = index.StateNew
		} else if e.State == index.StateCached {
			e.State = index.StateDirty
		}
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	
	debounceStr := s.cfg.PushDebounce
	if debounceStr == "" {
		debounceStr = "10s"
	}
	d, err := time.ParseDuration(debounceStr)
	if err != nil {
		d = 10 * time.Second
	}
	
	s.pending[id] = time.Now().Add(d)
	log.Printf("scheduled push for %s in %v", id, d)
}

func (s *SyncLoop) processPending(ctx context.Context) {
	s.mu.Lock()
	now := time.Now()
	var ready []string
	for id, triggerAt := range s.pending {
		if now.After(triggerAt) {
			ready = append(ready, id)
			delete(s.pending, id)
		}
	}
	s.mu.Unlock()

	for _, id := range ready {
		go s.push(ctx, id)
	}
}

func (s *SyncLoop) push(ctx context.Context, id string) {
	entry, ok := s.idx.Get(id)
	if !ok || (entry.State != index.StateDirty && entry.State != index.StateNew) {
		return
	}

	localPath := filepath.Join(s.cfg.DocsDir, filepath.FromSlash(entry.Key))
	f, err := os.Open(localPath)
	if err != nil {
		log.Printf("failed to open %s for push: %v", id, err)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		log.Printf("failed to stat %s for push: %v", id, err)
		return
	}

	expectedEtag := entry.BaseETag
	if entry.State == index.StateNew {
		expectedEtag = ""
	}

	newEtag, err := s.store.Put(ctx, entry.Key, f, stat.Size(), expectedEtag)
	if err != nil {
		if err == storage.ErrPreconditionFailed {
			log.Printf("push rejected for %s: remote changed", id)
			s.idx.Update(id, func(e *index.Entry) {
				e.State = index.StateDiverged
			})
		} else {
			log.Printf("push failed for %s: %v", id, err)
		}
		return
	}

	log.Printf("successfully pushed %s", id)
	s.idx.Update(id, func(e *index.Entry) {
		e.State = index.StateCached
		e.BaseETag = newEtag
		e.RemoteETag = newEtag
		e.LastSyncedAt = time.Now()
	})
}

func (s *SyncLoop) pollRemote(ctx context.Context) {
	log.Println("polling remote state")
	objects, err := s.store.List(ctx)
	if err != nil {
		log.Printf("poll failed: %v", err)
		return
	}

	remoteMap := make(map[string]storage.Object)
	for _, obj := range objects {
		key := strings.TrimPrefix(obj.Key, "/")
		id := index.TrimDocExt(key)
		remoteMap[id] = obj
	}

	// Update index based on poll
	// Need to lock file updates here but we do it row by row
	entries := s.idx.List()
	
	for _, e := range entries {
		s.idx.Update(e.ID, func(entry *index.Entry) {
			entry.LastPolledAt = time.Now()
			
			rem, exists := remoteMap[e.ID]
			if !exists {
				// PRD: "A key that disappeared from S3 is flagged in pagebox status and never deleted locally."
				// We don't automatically delete. We just leave it.
				return
			}
			
			if entry.RemoteETag != rem.ETag {
				entry.RemoteETag = rem.ETag
				
				if entry.State == index.StateCached {
					entry.State = index.StateStale
				} else if entry.State == index.StateDirty {
					entry.State = index.StateDiverged
				}
			}
		})
		delete(remoteMap, e.ID)
	}

	// Any remaining in remoteMap are new placeholders
	for id, rem := range remoteMap {
		s.idx.Update(id, func(entry *index.Entry) {
			if entry.ID == "" {
				entry.ID = id
				entry.Key = rem.Key
				entry.State = index.StatePlaceholder
				entry.RemoteETag = rem.ETag
				entry.Size = 0 // local size is zero until fetched
				entry.LastPolledAt = time.Now()
				
				// Ensure placeholder file exists locally
				localPath := filepath.Join(s.cfg.DocsDir, filepath.FromSlash(rem.Key))
				os.MkdirAll(filepath.Dir(localPath), 0755)
				if _, err := os.Stat(localPath); os.IsNotExist(err) {
					os.WriteFile(localPath, []byte{}, 0644)
				}
			}
		})
	}
}

func hashFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	hash := sha256.New()
	io.Copy(hash, f)
	return hex.EncodeToString(hash.Sum(nil))
}

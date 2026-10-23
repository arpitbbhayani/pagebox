package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// DocumentState represents the synchronization state of a document.
type DocumentState string

const (
	StatePlaceholder DocumentState = "placeholder"
	StateCached      DocumentState = "cached"
	StateStale       DocumentState = "stale"
	StateDirty       DocumentState = "dirty"
	StateDiverged    DocumentState = "diverged"
	StateNew         DocumentState = "new"
)

// Entry holds the state for a single document.
type Entry struct {
	ID           string        `json:"id"`
	Key          string        `json:"key"`
	State        DocumentState `json:"state"`
	BaseETag     string        `json:"base_etag"`
	RemoteETag   string        `json:"remote_etag"`
	Size         int64         `json:"size"`
	LocalSHA256  string        `json:"local_sha256"`
	LastSyncedAt time.Time     `json:"last_synced_at"`
	LastPolledAt time.Time     `json:"last_polled_at"`
}

// Timestamp returns the most recent activity timestamp for the entry.
// It checks LastSyncedAt first, then LastPolledAt, and falls back to
// local file modification time if docsDir is provided and the file exists.
func (e Entry) Timestamp(docsDir string) time.Time {
	if !e.LastSyncedAt.IsZero() {
		return e.LastSyncedAt
	}
	if !e.LastPolledAt.IsZero() {
		return e.LastPolledAt
	}
	if docsDir != "" && e.Key != "" {
		p := filepath.Join(docsDir, filepath.FromSlash(e.Key))
		if fi, err := os.Stat(p); err == nil {
			return fi.ModTime()
		}
	}
	return time.Time{}
}

// Store manages the index file.
type Store struct {
	mu      sync.RWMutex
	path    string
	lock    *flock.Flock
	entries map[string]Entry
	lastMod time.Time
}

// NewStore initializes a new index store.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".pagebox")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "index.json")
	lockPath := filepath.Join(dir, "index.lock")

	s := &Store{
		path:    path,
		lock:    flock.New(lockPath),
		entries: make(map[string]Entry),
	}

	return s, s.load()
}

// load reads the index from disk. Requires no lock, internal use only.
func (s *Store) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = make(map[string]Entry)
			return nil
		}
		return err
	}
	defer f.Close()

	if stat, err := f.Stat(); err == nil {
		s.lastMod = stat.ModTime()
	}

	var list []Entry
	if err := json.NewDecoder(f).Decode(&list); err != nil {
		return fmt.Errorf("corrupt index: %w", err)
	}

	s.entries = make(map[string]Entry, len(list))
	for _, e := range list {
		if IsIgnoredDoc(e.ID, e.Key) {
			continue
		}
		s.entries[e.ID] = e
	}
	return nil
}

func (s *Store) reloadIfModified() {
	stat, err := os.Stat(s.path)
	if err != nil {
		return
	}
	if stat.ModTime().After(s.lastMod) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if stat.ModTime().After(s.lastMod) {
			_ = s.load()
		}
	}
}

// save writes the index to disk atomically. Requires the flock.
func (s *Store) save() error {
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}

	tempPath := s.path + ".tmp"
	f, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(list); err != nil {
		f.Close()
		os.Remove(tempPath)
		return err
	}
	
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempPath)
		return err
	}
	
	f.Close()
	return os.Rename(tempPath, s.path)
}

// TrimDocExt removes known document extensions (.html, .md, .markdown) case-insensitively.
func TrimDocExt(s string) string {
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".html") {
		return s[:len(s)-5]
	}
	if strings.HasSuffix(lower, ".markdown") {
		return s[:len(s)-9]
	}
	if strings.HasSuffix(lower, ".md") {
		return s[:len(s)-3]
	}
	return s
}

// IsIgnoredDoc returns true if the document (by ID or Key) is an internal system file
// (such as index.html or robots.txt) or hidden file that should not be tracked, listed, or shown as a document.
func IsIgnoredDoc(id, key string) bool {
	check := func(s string) bool {
		raw := strings.TrimSpace(s)
		if strings.HasSuffix(raw, "/") {
			return true
		}
		s = strings.TrimPrefix(filepath.ToSlash(raw), "/")
		if s == "" {
			return true
		}
		lower := strings.ToLower(s)
		if lower == "index.html" || strings.HasSuffix(lower, "/index.html") ||
			lower == "robots.txt" || strings.HasSuffix(lower, "/robots.txt") ||
			lower == "index" || strings.HasSuffix(lower, "/index") ||
			lower == "robots" || strings.HasSuffix(lower, "/robots") ||
			lower == "_trash" || strings.HasPrefix(lower, "_trash/") ||
			strings.HasSuffix(lower, ".pagebox-tmp") {
			return true
		}
		parts := strings.Split(s, "/")
		for _, p := range parts {
			if strings.HasPrefix(p, ".") {
				return true
			}
		}
		return false
	}

	return (id != "" && check(id)) || (key != "" && check(key))
}

// Get returns an entry by ID or Key.
func (s *Store) Get(id string) (Entry, bool) {
	if IsIgnoredDoc(id, "") {
		return Entry{}, false
	}
	s.reloadIfModified()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.entries[id]; ok {
		if IsIgnoredDoc(e.ID, e.Key) {
			return Entry{}, false
		}
		return e, true
	}
	trimmed := TrimDocExt(id)
	if e, ok := s.entries[trimmed]; ok {
		if IsIgnoredDoc(e.ID, e.Key) {
			return Entry{}, false
		}
		return e, true
	}
	for _, ext := range []string{".html", ".md", ".markdown"} {
		if e, ok := s.entries[trimmed+ext]; ok {
			if IsIgnoredDoc(e.ID, e.Key) {
				return Entry{}, false
			}
			return e, true
		}
	}
	for _, e := range s.entries {
		if IsIgnoredDoc(e.ID, e.Key) {
			continue
		}
		if e.Key == id || e.ID == id || e.ID == trimmed || TrimDocExt(e.Key) == trimmed || TrimDocExt(e.ID) == trimmed {
			return e, true
		}
	}
	return Entry{}, false
}

// List returns all entries.
func (s *Store) List() []Entry {
	s.reloadIfModified()
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if IsIgnoredDoc(e.ID, e.Key) {
			continue
		}
		list = append(list, e)
	}
	return list
}

// Update mutates an entry within a file lock to guarantee cross-process consistency.
func (s *Store) Update(id string, mutate func(*Entry)) error {
	if IsIgnoredDoc(id, "") {
		return nil
	}
	locked, err := s.lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		// Wait for lock if daemon or another CLI is holding it.
		// Since writes are fast (atomic rename), we shouldn't wait long.
		err = s.lock.Lock()
		if err != nil {
			return err
		}
	}
	defer s.lock.Unlock()

	// Reload inside lock to get the latest state written by other processes
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}

	actualID := id
	e, ok := s.entries[actualID]
	if !ok {
		trimmed := TrimDocExt(id)
		if existing, found := s.entries[trimmed]; found {
			e = existing
			ok = true
			actualID = trimmed
		} else {
			for k, existing := range s.entries {
				if existing.Key == id || existing.ID == id || existing.ID == trimmed || TrimDocExt(existing.Key) == trimmed || TrimDocExt(existing.ID) == trimmed {
					e = existing
					ok = true
					actualID = k
					break
				}
			}
		}
		if !ok {
			e = Entry{ID: id}
		}
	}
	
	mutate(&e)
	if IsIgnoredDoc(e.ID, e.Key) {
		delete(s.entries, actualID)
		delete(s.entries, id)
		return s.save()
	}
	s.entries[actualID] = e

	return s.save()
}

// Delete removes an entry from the index.
func (s *Store) Delete(id string) error {
	locked, err := s.lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		if err = s.lock.Lock(); err != nil {
			return err
		}
	}
	defer s.lock.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}

	if _, ok := s.entries[id]; ok {
		delete(s.entries, id)
	} else {
		trimmed := TrimDocExt(id)
		if _, ok := s.entries[trimmed]; ok {
			delete(s.entries, trimmed)
		} else {
			for k, e := range s.entries {
				if e.Key == id || e.ID == id || e.ID == trimmed || TrimDocExt(e.Key) == trimmed || TrimDocExt(e.ID) == trimmed {
					delete(s.entries, k)
					break
				}
			}
		}
	}
	return s.save()
}

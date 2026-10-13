package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Store manages the index file.
type Store struct {
	mu      sync.RWMutex
	path    string
	lock    *flock.Flock
	entries map[string]Entry
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

	var list []Entry
	if err := json.NewDecoder(f).Decode(&list); err != nil {
		return fmt.Errorf("corrupt index: %w", err)
	}

	s.entries = make(map[string]Entry, len(list))
	for _, e := range list {
		s.entries[e.ID] = e
	}
	return nil
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

// Get returns an entry by ID.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	return e, ok
}

// List returns all entries.
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	return list
}

// Update mutates an entry within a file lock to guarantee cross-process consistency.
func (s *Store) Update(id string, mutate func(*Entry)) error {
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

	e, ok := s.entries[id]
	if !ok {
		e = Entry{ID: id}
	}
	
	mutate(&e)
	s.entries[id] = e

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

	delete(s.entries, id)
	return s.save()
}

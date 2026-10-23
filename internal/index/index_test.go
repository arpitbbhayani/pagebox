package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsIgnoredDoc(t *testing.T) {
	cases := []struct {
		id       string
		key      string
		expected bool
	}{
		{"robots", "", true},
		{"robots.txt", "", true},
		{"", "robots.txt", true},
		{"", "guides/robots.txt", true},
		{"index", "", true},
		{"index.html", "", true},
		{"", "index.html", true},
		{"", "guides/index.html", true},
		{"", "guides/index", true},
		{"", "_trash/file.html", true},
		{"", ".hidden.html", true},
		{"", "folder/", true},
		{"guide", "guide.html", false},
		{"architecture", "architecture.md", false},
		{"intro", "intro.markdown", false},
	}

	for _, c := range cases {
		got := IsIgnoredDoc(c.id, c.key)
		if got != c.expected {
			t.Errorf("IsIgnoredDoc(%q, %q) = %v; want %v", c.id, c.key, got, c.expected)
		}
	}
}

func TestStoreExcludesIgnoredDocs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	s, err := NewStore()
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Adding normal document
	_ = s.Update("doc1", func(e *Entry) {
		e.ID = "doc1"
		e.Key = "doc1.html"
		e.State = StateCached
	})

	// Attempting to add robots and index
	_ = s.Update("robots", func(e *Entry) {
		e.ID = "robots"
		e.Key = "robots.txt"
		e.State = StateCached
	})
	_ = s.Update("index", func(e *Entry) {
		e.ID = "index"
		e.Key = "index.html"
		e.State = StateCached
	})

	entries := s.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "doc1" {
		t.Errorf("expected entry ID 'doc1', got %q", entries[0].ID)
	}

	// Verify Get returns false for ignored docs
	if _, ok := s.Get("robots"); ok {
		t.Errorf("expected Get(robots) to return false")
	}
	if _, ok := s.Get("index"); ok {
		t.Errorf("expected Get(index) to return false")
	}

	// Reload store and verify persistence excludes them
	s2, err := NewStore()
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	entries2 := s2.List()
	if len(entries2) != 1 || entries2[0].ID != "doc1" {
		t.Errorf("expected reloaded store to only have doc1, got %v", entries2)
	}

	// Manually write corrupt index.json containing robots and index to verify load() filters them
	indexPath := filepath.Join(tmpHome, ".pagebox", "index.json")
	corruptJSON := `[
		{"id": "doc1", "key": "doc1.html", "state": "cached"},
		{"id": "robots", "key": "robots.txt", "state": "cached"},
		{"id": "index", "key": "index.html", "state": "cached"}
	]`
	if err := os.WriteFile(indexPath, []byte(corruptJSON), 0644); err != nil {
		t.Fatalf("failed to write corrupt json: %v", err)
	}

	s3, err := NewStore()
	if err != nil {
		t.Fatalf("failed to load store with corrupt json: %v", err)
	}
	entries3 := s3.List()
	if len(entries3) != 1 || entries3[0].ID != "doc1" {
		t.Errorf("expected store to filter out corrupt robots/index on load, got %v", entries3)
	}
}

func TestIsIgnoredDocTempFiles(t *testing.T) {
	if !IsIgnoredDoc("a.html.pagebox-tmp", "a.html.pagebox-tmp") {
		t.Fatal("pull temp files must be ignored")
	}
}

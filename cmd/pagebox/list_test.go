package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

type mockStorageForList struct {
	objects []storage.Object
	err     error
}

func (m *mockStorageForList) Put(ctx context.Context, key string, body io.Reader, size int64, etag string) (string, error) {
	return "mock-etag", nil
}

func (m *mockStorageForList) Get(ctx context.Context, key string, etag string) (io.ReadCloser, string, error) {
	return nil, "", storage.ErrNotFound
}

func (m *mockStorageForList) Delete(ctx context.Context, key string) error {
	return nil
}

func (m *mockStorageForList) List(ctx context.Context) ([]storage.Object, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.objects, nil
}

func TestPageboxListFetchesFromS3(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	cfg.Backend = "s3"
	cfg.Bucket = "test-bucket"
	if err := pbconfig.Save(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	mock := &mockStorageForList{
		objects: []storage.Object{
			{Key: "remote-doc.md", ETag: "etag-1", Size: 1024},
			{Key: "guides/tutorial.html", ETag: "etag-2", Size: 2048},
			{Key: "_trash/deleted.md", ETag: "etag-trash", Size: 500},
			{Key: ".hidden.md", ETag: "etag-hidden", Size: 100},
			{Key: "folder/", ETag: "etag-dir", Size: 0},
		},
	}

	oldStorageNew := storageNew
	storageNew = func(c pbconfig.Config) (storage.Storage, error) {
		return mock, nil
	}
	defer func() { storageNew = oldStorageNew }()

	out := captureStdout(func() {
		listCmd.SetArgs([]string{})
		_ = listCmd.Execute()
	})

	if !strings.Contains(out, "remote-doc") {
		t.Errorf("expected out to list remote-doc, got:\n%s", out)
	}
	if !strings.Contains(out, "guides/tutorial") {
		t.Errorf("expected out to list guides/tutorial, got:\n%s", out)
	}
	if strings.Contains(out, "deleted") || strings.Contains(out, "hidden") || strings.Contains(out, "folder") {
		t.Errorf("out should not contain ignored remote keys, got:\n%s", out)
	}

	// Verify placeholder entries in index
	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}

	e1, ok1 := idx.Get("remote-doc")
	if !ok1 {
		t.Fatalf("expected remote-doc in index")
	}
	if e1.State != index.StatePlaceholder {
		t.Errorf("expected state StatePlaceholder, got %s", e1.State)
	}
	if e1.RemoteETag != "etag-1" {
		t.Errorf("expected RemoteETag 'etag-1', got '%s'", e1.RemoteETag)
	}

	e2, ok2 := idx.Get("guides/tutorial")
	if !ok2 {
		t.Fatalf("expected guides/tutorial in index")
	}
	if e2.State != index.StatePlaceholder {
		t.Errorf("expected state StatePlaceholder, got %s", e2.State)
	}
}

func TestPageboxListDetectsStaleAndDiverged(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	cfg.Backend = "s3"
	cfg.Bucket = "test-bucket"
	_ = pbconfig.Save(cfg)

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}

	// 1. Cached document whose remote ETag changes -> Stale
	_ = idx.Update("cached-doc", func(e *index.Entry) {
		e.ID = "cached-doc"
		e.Key = "cached-doc.md"
		e.State = index.StateCached
		e.BaseETag = "etag-old"
		e.RemoteETag = "etag-old"
		e.Size = 512
		e.LastSyncedAt = time.Now()
	})

	// 2. Dirty document whose remote ETag changes -> Diverged
	_ = idx.Update("dirty-doc", func(e *index.Entry) {
		e.ID = "dirty-doc"
		e.Key = "dirty-doc.md"
		e.State = index.StateDirty
		e.BaseETag = "etag-old"
		e.RemoteETag = "etag-old"
		e.Size = 1024
	})

	mock := &mockStorageForList{
		objects: []storage.Object{
			{Key: "cached-doc.md", ETag: "etag-new", Size: 600},
			{Key: "dirty-doc.md", ETag: "etag-conflict", Size: 1200},
		},
	}

	oldStorageNew := storageNew
	storageNew = func(c pbconfig.Config) (storage.Storage, error) {
		return mock, nil
	}
	defer func() { storageNew = oldStorageNew }()

	out := captureStdout(func() {
		listCmd.SetArgs([]string{})
		_ = listCmd.Execute()
	})

	if !strings.Contains(out, "stale") {
		t.Errorf("expected output to contain stale state, got:\n%s", out)
	}
	if !strings.Contains(out, "diverged") {
		t.Errorf("expected output to contain diverged state, got:\n%s", out)
	}

	// Verify updated index
	idxReloaded, _ := index.NewStore()
	eCached, _ := idxReloaded.Get("cached-doc")
	if eCached.State != index.StateStale {
		t.Errorf("expected cached-doc state StateStale, got %s", eCached.State)
	}
	if eCached.RemoteETag != "etag-new" {
		t.Errorf("expected RemoteETag 'etag-new', got '%s'", eCached.RemoteETag)
	}

	eDirty, _ := idxReloaded.Get("dirty-doc")
	if eDirty.State != index.StateDiverged {
		t.Errorf("expected dirty-doc state StateDiverged, got %s", eDirty.State)
	}
}

func TestPageboxListEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	cfg.Backend = "s3"
	cfg.Bucket = "test-bucket"
	_ = pbconfig.Save(cfg)

	mock := &mockStorageForList{
		objects: []storage.Object{},
	}

	oldStorageNew := storageNew
	storageNew = func(c pbconfig.Config) (storage.Storage, error) {
		return mock, nil
	}
	defer func() { storageNew = oldStorageNew }()

	out := captureStdout(func() {
		listCmd.SetArgs([]string{})
		_ = listCmd.Execute()
	})

	if !strings.Contains(out, "no documents found") {
		t.Errorf("expected 'no documents found', got:\n%s", out)
	}
}

func TestPageboxListOrdersMostRecentFirst(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	_ = pbconfig.Save(cfg)

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}

	now := time.Now()
	_ = idx.Update("alpha-older", func(e *index.Entry) {
		e.ID = "alpha-older"
		e.Key = "alpha-older.md"
		e.State = index.StateCached
		e.Size = 100
		e.LastSyncedAt = now.Add(-3 * time.Hour)
	})
	_ = idx.Update("omega-newer", func(e *index.Entry) {
		e.ID = "omega-newer"
		e.Key = "omega-newer.md"
		e.State = index.StateCached
		e.Size = 200
		e.LastSyncedAt = now
	})

	out := captureStdout(func() {
		listCmd.SetArgs([]string{})
		_ = listCmd.Execute()
	})

	newerIdx := strings.Index(out, "omega-newer")
	olderIdx := strings.Index(out, "alpha-older")

	if newerIdx == -1 || olderIdx == -1 {
		t.Fatalf("expected both omega-newer and alpha-older in output, got:\n%s", out)
	}
	if newerIdx > olderIdx {
		t.Errorf("expected omega-newer to appear before alpha-older in list, but got newer at %d, older at %d", newerIdx, olderIdx)
	}
}

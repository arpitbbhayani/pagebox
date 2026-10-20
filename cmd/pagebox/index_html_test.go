package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

func TestAddAndPushUpdatesIndexHTML(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	cfg.Backend = "s3"
	cfg.Bucket = "test-bucket"
	_ = pbconfig.Save(cfg)

	mock := &mockStorage{}
	oldStorageNew := storageNew
	storageNew = func(c pbconfig.Config) (storage.Storage, error) {
		return mock, nil
	}
	defer func() { storageNew = oldStorageNew }()

	// 1. Create a doc file in docsDir
	docFile := filepath.Join(docsDir, "architecture.html")
	_ = os.WriteFile(docFile, []byte("<h1>Architecture Overview</h1>"), 0644)

	// 2. Run add
	_ = captureStdout(func() {
		addCmd.SetArgs([]string{"architecture.html"})
		_ = addCmd.Execute()
	})

	// Verify index.html is generated locally in docsDir
	indexHTMLPath := filepath.Join(docsDir, "index.html")
	content, err := os.ReadFile(indexHTMLPath)
	if err != nil {
		t.Fatalf("expected index.html to be created in docsDir: %v", err)
	}

	htmlStr := string(content)
	if !strings.Contains(htmlStr, "architecture") {
		t.Errorf("expected index.html to contain 'architecture', got:\n%s", htmlStr)
	}

	// Ensure index.html does not contain itself or robots.txt as a document in the table
	if strings.Contains(htmlStr, `data-id="index"`) || strings.Contains(htmlStr, `data-text="index.html"`) {
		t.Errorf("expected index.html to exclude itself from document table")
	}
	if strings.Contains(htmlStr, `data-id="robots"`) || strings.Contains(htmlStr, `data-text="robots.txt"`) {
		t.Errorf("expected index.html to exclude robots.txt from document table")
	}

	// 3. Mark another file dirty and run push
	_ = os.WriteFile(filepath.Join(docsDir, "guide.html"), []byte("<h1>Guide</h1>"), 0644)
	idx, _ := index.NewStore()
	_ = idx.Update("guide", func(e *index.Entry) {
		e.ID = "guide"
		e.Key = "guide.html"
		e.State = index.StateNew
		e.Size = 120
		e.LastSyncedAt = time.Now()
	})

	_ = captureStdout(func() {
		pushCmd.SetArgs([]string{})
		_ = pushCmd.Execute()
	})

	// Verify index.html was pushed to remote storage
	foundPushedIndexHTML := false
	foundPushedRobotsTxt := false
	for _, key := range mock.putKeys {
		if key == "index.html" {
			foundPushedIndexHTML = true
		}
		if key == "robots.txt" {
			foundPushedRobotsTxt = true
		}
	}
	if !foundPushedIndexHTML {
		t.Errorf("expected index.html to be pushed to remote storage on push, got keys: %v", mock.putKeys)
	}
	if !foundPushedRobotsTxt {
		t.Errorf("expected robots.txt to be pushed to remote storage on push, got keys: %v", mock.putKeys)
	}

	// Verify robots.txt content
	robotsPath := filepath.Join(docsDir, "robots.txt")
	robotsContent, err := os.ReadFile(robotsPath)
	if err != nil {
		t.Fatalf("failed to read robots.txt: %v", err)
	}
	if !strings.Contains(string(robotsContent), "User-agent: *") || !strings.Contains(string(robotsContent), "Disallow: /") {
		t.Errorf("expected robots.txt to disallow all crawlers, got:\n%s", string(robotsContent))
	}
}

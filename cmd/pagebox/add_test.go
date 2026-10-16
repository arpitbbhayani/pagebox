package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestResolveFilesToInspect(t *testing.T) {
	docsDir := t.TempDir()

	// Create nested structure in docsDir
	// docsDir/
	//   intro.html
	//   guides/
	//     setup.html
	//     nested/
	//       advanced.html
	//   .hidden/
	//     secret.html
	//   .ignored.html

	os.WriteFile(filepath.Join(docsDir, "intro.html"), []byte("intro"), 0644)
	os.MkdirAll(filepath.Join(docsDir, "guides", "nested"), 0755)
	os.WriteFile(filepath.Join(docsDir, "guides", "setup.html"), []byte("setup"), 0644)
	os.WriteFile(filepath.Join(docsDir, "guides", "nested", "advanced.html"), []byte("advanced"), 0644)
	os.MkdirAll(filepath.Join(docsDir, ".hidden"), 0755)
	os.WriteFile(filepath.Join(docsDir, ".hidden", "secret.html"), []byte("secret"), 0644)
	os.WriteFile(filepath.Join(docsDir, ".ignored.html"), []byte("ignored"), 0644)

	// Case 1: no args -> inspect all (skipping hidden)
	files, err := resolveFilesToInspect(docsDir, "", nil, false)
	if err != nil {
		t.Fatalf("resolveFilesToInspect failed: %v", err)
	}

	foundMap := make(map[string]bool)
	for _, f := range files {
		foundMap[f] = true
	}

	if !foundMap["intro.html"] {
		t.Errorf("expected intro.html to be found")
	}
	if !foundMap["guides/setup.html"] {
		t.Errorf("expected guides/setup.html to be found")
	}
	if !foundMap["guides/nested/advanced.html"] {
		t.Errorf("expected guides/nested/advanced.html to be found")
	}
	if foundMap[".ignored.html"] || foundMap[".hidden/secret.html"] {
		t.Errorf("hidden files should be skipped")
	}

	// Case 2: specific directory arg
	subFiles, err := resolveFilesToInspect(docsDir, "", []string{"guides/nested"}, false)
	if err != nil {
		t.Fatalf("resolveFilesToInspect failed: %v", err)
	}
	if len(subFiles) != 1 || subFiles[0] != "guides/nested/advanced.html" {
		t.Errorf("expected only guides/nested/advanced.html, got %v", subFiles)
	}

	// Case 3: specific file arg
	oneFile, err := resolveFilesToInspect(docsDir, "", []string{"guides/setup.html"}, false)
	if err != nil {
		t.Fatalf("resolveFilesToInspect failed: %v", err)
	}
	if len(oneFile) != 1 || oneFile[0] != "guides/setup.html" {
		t.Errorf("expected only guides/setup.html, got %v", oneFile)
	}
}

func TestPageboxAddCommandConfiguredDir(t *testing.T) {
	// Setup custom home dir for test config and index
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "custom_docs_dir")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}

	// Save custom config pointing to docsDir
	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	if err := pbconfig.Save(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Create nested files in configured docsDir
	if err := os.MkdirAll(filepath.Join(docsDir, "architecture"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "architecture", "overview.html"), []byte("<h1>Architecture</h1>"), 0644); err != nil {
		t.Fatalf("failed to write doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.html"), []byte("<h1>Readme</h1>"), 0644); err != nil {
		t.Fatalf("failed to write doc: %v", err)
	}

	// Set current working directory to a completely different directory outside docsDir
	differentDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	_ = os.Chdir(differentDir)

	// Run addCmd with no args
	output := captureStdout(func() {
		addCmd.SetArgs([]string{})
		_ = addCmd.Execute()
	})

	// Verify git-like output
	if !strings.Contains(output, "add 'architecture/overview.html'") {
		t.Errorf("expected output to contain add 'architecture/overview.html', got:\n%s", output)
	}
	if !strings.Contains(output, "add 'readme.html'") {
		t.Errorf("expected output to contain add 'readme.html', got:\n%s", output)
	}

	// Verify files are now in the index
	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to load index: %v", err)
	}

	entry1, ok1 := idx.Get("architecture/overview")
	if !ok1 || entry1.State != index.StateNew {
		t.Errorf("expected architecture/overview in index with StateNew, got ok=%v, state=%s", ok1, entry1.State)
	}
	if entry1.Key != "architecture/overview.html" {
		t.Errorf("expected key 'architecture/overview.html', got '%s'", entry1.Key)
	}

	entry2, ok2 := idx.Get("readme")
	if !ok2 || entry2.State != index.StateNew {
		t.Errorf("expected readme in index with StateNew, got ok=%v, state=%s", ok2, entry2.State)
	}

	// Running pagebox add again when no files changed should report nothing to add
	outputSecond := captureStdout(func() {
		addCmd.SetArgs([]string{})
		_ = addCmd.Execute()
	})
	if !strings.Contains(outputSecond, "nothing to add") {
		t.Errorf("expected second run to output 'nothing to add', got:\n%s", outputSecond)
	}

	// Modify one of the files
	_ = os.WriteFile(filepath.Join(docsDir, "readme.html"), []byte("<h1>Readme Updated</h1>"), 0644)

	// Now running add should detect the modification and add it
	outputThird := captureStdout(func() {
		addCmd.SetArgs([]string{})
		_ = addCmd.Execute()
	})
	if !strings.Contains(outputThird, "add 'readme.html'") {
		t.Errorf("expected third run to add 'readme.html', got:\n%s", outputThird)
	}
	if strings.Contains(outputThird, "overview.html") {
		t.Errorf("third run should not re-add unchanged overview.html")
	}
}

func TestPageboxAddMarkdownFiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "markdown_docs_dir")
	_ = os.MkdirAll(docsDir, 0755)

	configContent := "docs_dir = \"" + docsDir + "\"\nport = 8732\n"
	_ = os.MkdirAll(filepath.Join(tmpHome, ".pagebox"), 0700)
	_ = os.WriteFile(filepath.Join(tmpHome, ".pagebox", "config.toml"), []byte(configContent), 0644)

	// Create markdown files
	_ = os.WriteFile(filepath.Join(docsDir, "spec.md"), []byte("# Spec\n\nContent here"), 0644)
	_ = os.MkdirAll(filepath.Join(docsDir, "rfcs"), 0755)
	_ = os.WriteFile(filepath.Join(docsDir, "rfcs", "rfc-001.markdown"), []byte("# RFC 1"), 0644)

	out := captureStdout(func() {
		addCmd.SetArgs([]string{})
		_ = addCmd.Execute()
	})

	if !strings.Contains(out, "add 'rfcs/rfc-001.markdown'") {
		t.Errorf("expected out to add 'rfcs/rfc-001.markdown', got:\n%s", out)
	}
	if !strings.Contains(out, "add 'spec.md'") {
		t.Errorf("expected out to add 'spec.md', got:\n%s", out)
	}

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}

	e1, ok1 := idx.Get("spec")
	if !ok1 || e1.Key != "spec.md" {
		t.Errorf("expected spec in index with key 'spec.md', got ok=%v, key=%s", ok1, e1.Key)
	}

	e2, ok2 := idx.Get("rfcs/rfc-001")
	if !ok2 || e2.Key != "rfcs/rfc-001.markdown" {
		t.Errorf("expected rfcs/rfc-001 in index with key 'rfcs/rfc-001.markdown', got ok=%v, key=%s", ok2, e2.Key)
	}
}

func TestPageboxAddExternalFileSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	_ = pbconfig.Save(cfg)

	// Create an external document outside docsDir
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "verification-bottleneck.md")
	_ = os.WriteFile(externalFile, []byte("# Verification Bottleneck"), 0644)

	out := captureStdout(func() {
		addCmd.SetArgs([]string{externalFile})
		_ = addCmd.Execute()
	})

	if !strings.Contains(out, "verification-bottleneck.md") {
		t.Errorf("expected out to mention verification-bottleneck.md, got:\n%s", out)
	}

	// Verify symlink was created in docsDir
	expectedSymlink := filepath.Join(docsDir, "verification-bottleneck.md")
	linfo, err := os.Lstat(expectedSymlink)
	if err != nil {
		t.Fatalf("expected symlink at %s, but got error: %v", expectedSymlink, err)
	}
	if linfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink", expectedSymlink)
	}
	target, err := os.Readlink(expectedSymlink)
	if err != nil || target != externalFile {
		t.Errorf("expected symlink target %s, got %s (err: %v)", externalFile, target, err)
	}

	// Verify entry in index
	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}
	entry, ok := idx.Get("verification-bottleneck")
	if !ok {
		t.Fatalf("expected verification-bottleneck in index")
	}
	if entry.Key != "verification-bottleneck.md" {
		t.Errorf("expected key 'verification-bottleneck.md', got '%s'", entry.Key)
	}
	if entry.State != index.StateNew {
		t.Errorf("expected StateNew when S3 not configured, got %s", entry.State)
	}
}

func TestPageboxAddExternalFileWithTilde(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	_ = pbconfig.Save(cfg)

	// Create a file in ~/docs/test.md
	homeDocs := filepath.Join(tmpHome, "docs")
	_ = os.MkdirAll(homeDocs, 0755)
	_ = os.WriteFile(filepath.Join(homeDocs, "test.md"), []byte("# Test"), 0644)

	out := captureStdout(func() {
		addCmd.SetArgs([]string{"~/docs/test.md"})
		_ = addCmd.Execute()
	})

	if !strings.Contains(out, "test.md") {
		t.Errorf("expected out to mention test.md, got:\n%s", out)
	}

	symlink := filepath.Join(docsDir, "test.md")
	linfo, err := os.Lstat(symlink)
	if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", symlink)
	}
}

func TestPageboxAddExternalDirectorySymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	_ = pbconfig.Save(cfg)

	// Create an external directory with files outside docsDir
	externalDir := t.TempDir()
	projectDocs := filepath.Join(externalDir, "internals")
	_ = os.MkdirAll(projectDocs, 0755)
	_ = os.WriteFile(filepath.Join(projectDocs, "bottleneck.md"), []byte("# Bottleneck"), 0644)
	_ = os.WriteFile(filepath.Join(projectDocs, "overview.html"), []byte("<h1>Overview</h1>"), 0644)

	out := captureStdout(func() {
		addCmd.SetArgs([]string{projectDocs})
		_ = addCmd.Execute()
	})

	if !strings.Contains(out, "add 'internals/bottleneck.md'") {
		t.Errorf("expected out to add 'internals/bottleneck.md', got:\n%s", out)
	}
	if !strings.Contains(out, "add 'internals/overview.html'") {
		t.Errorf("expected out to add 'internals/overview.html', got:\n%s", out)
	}

	// Verify directory symlink in docsDir
	symlink := filepath.Join(docsDir, "internals")
	linfo, err := os.Lstat(symlink)
	if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", symlink)
	}

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}
	if _, ok := idx.Get("internals/bottleneck"); !ok {
		t.Errorf("expected internals/bottleneck in index")
	}
	if _, ok := idx.Get("internals/overview"); !ok {
		t.Errorf("expected internals/overview in index")
	}
}

type mockStorage struct {
	putCalled bool
	putKey    string
}

func (m *mockStorage) Put(ctx context.Context, key string, body io.Reader, size int64, etag string) (string, error) {
	m.putCalled = true
	m.putKey = key
	return "mock-etag-123", nil
}

func (m *mockStorage) Get(ctx context.Context, key string, etag string) (io.ReadCloser, string, error) {
	return nil, "", storage.ErrNotFound
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (m *mockStorage) List(ctx context.Context) ([]storage.Object, error) {
	return nil, nil
}

func TestPageboxAddUploadsToS3IfConfigured(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	docsDir := filepath.Join(tmpHome, "pagebox")
	_ = os.MkdirAll(docsDir, 0755)

	cfg := pbconfig.DefaultConfig()
	cfg.DocsDir = docsDir
	cfg.Backend = "s3"
	cfg.Bucket = "test-bucket"
	_ = pbconfig.Save(cfg)

	// Mock storage
	mock := &mockStorage{}
	oldStorageNew := storageNew
	storageNew = func(c pbconfig.Config) (storage.Storage, error) {
		return mock, nil
	}
	defer func() { storageNew = oldStorageNew }()

	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "architecture.md")
	_ = os.WriteFile(externalFile, []byte("# Architecture"), 0644)

	out := captureStdout(func() {
		addCmd.SetArgs([]string{externalFile})
		_ = addCmd.Execute()
	})

	if !mock.putCalled {
		t.Errorf("expected S3 Put to be called when S3 is configured, but was not")
	}
	if mock.putKey != "architecture.md" {
		t.Errorf("expected putKey 'architecture.md', got '%s'", mock.putKey)
	}
	if !strings.Contains(out, "pushed document") {
		t.Errorf("expected output to contain 'pushed document', got:\n%s", out)
	}

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to open index: %v", err)
	}
	entry, ok := idx.Get("architecture")
	if !ok {
		t.Fatalf("expected architecture in index")
	}
	if entry.State != index.StateCached {
		t.Errorf("expected entry state StateCached after upload, got %s", entry.State)
	}
	if entry.BaseETag != "mock-etag-123" {
		t.Errorf("expected BaseETag 'mock-etag-123', got '%s'", entry.BaseETag)
	}
}

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
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

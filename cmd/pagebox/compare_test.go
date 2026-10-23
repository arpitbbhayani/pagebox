package main

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

func TestCompareDoc(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello")
	sum := md5.Sum(content)
	etag := hex.EncodeToString(sum[:])
	rem := storage.Object{Key: "a.md", ETag: etag, Size: int64(len(content))}

	if st, _ := compareDoc(dir, index.Entry{}, rem); st != statusRemoteOnly {
		t.Fatalf("missing file: got %v", st)
	}

	path := filepath.Join(dir, "a.md")
	os.WriteFile(path, content, 0644)
	st, sha := compareDoc(dir, index.Entry{}, rem)
	if st != statusClean {
		t.Fatalf("identical file: got %v", st)
	}

	// Local edit, remote unchanged since base.
	os.WriteFile(path, []byte("edited"), 0644)
	e := index.Entry{BaseETag: etag, LocalSHA256: sha}
	if st, _ := compareDoc(dir, e, rem); st != statusDirty {
		t.Fatalf("local edit: got %v", st)
	}

	// Remote moved on, local untouched.
	os.WriteFile(path, []byte("old"), 0644)
	oldSha, _ := hashLocal(path)
	e = index.Entry{BaseETag: "old-etag", LocalSHA256: oldSha}
	if st, _ := compareDoc(dir, e, rem); st != statusStale {
		t.Fatalf("remote change: got %v", st)
	}

	// Both changed.
	os.WriteFile(path, []byte("mine"), 0644)
	if st, _ := compareDoc(dir, e, rem); st != statusDiverged {
		t.Fatalf("both changed: got %v", st)
	}
}

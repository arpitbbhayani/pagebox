package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

type dummyStorage struct{}

func (d *dummyStorage) Put(ctx context.Context, key string, body io.Reader, size int64, etag string) (string, error) {
	return "dummy-etag", nil
}

func (d *dummyStorage) Get(ctx context.Context, key string, etag string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("<h1>Dummy Remote</h1>")), "dummy-etag", nil
}

func (d *dummyStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (d *dummyStorage) List(ctx context.Context) ([]storage.Object, error) {
	return nil, nil
}

func setupTestServer(t *testing.T) (*Server, string, func()) {
	tmpDir, err := os.MkdirTemp("", "pagebox-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create test html file
	doc1Path := filepath.Join(tmpDir, "sample-doc.html")
	if err := os.WriteFile(doc1Path, []byte("<!DOCTYPE html><html><head><title>Sample Doc Title</title></head><body><h1>Hello World</h1></body></html>"), 0644); err != nil {
		t.Fatalf("failed to create sample doc: %v", err)
	}

	cfg := pbconfig.Config{
		DocsDir:      tmpDir,
		Port:         8730,
		PollInterval: "5m",
	}

	idx, err := index.NewStore()
	if err != nil {
		t.Fatalf("failed to create index store: %v", err)
	}

	// Add sample-doc to index
	_ = idx.Update("sample-doc", func(e *index.Entry) {
		e.ID = "sample-doc"
		e.Key = "sample-doc.html"
		e.State = index.StateCached
		e.Size = 100
		e.LastSyncedAt = time.Now()
	})

	srv := New(cfg, idx, &dummyStorage{})

	cleanup := func() {
		_ = idx.Delete("sample-doc")
		_ = os.RemoveAll(tmpDir)
	}

	return srv, tmpDir, cleanup
}

func TestIndexPage(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Verify Editorial ape-design elements
	if !strings.Contains(body, "/pagebox") {
		t.Errorf("expected body to contain '/pagebox'")
	}
	if !strings.Contains(body, "Document Index") {
		t.Errorf("expected body to contain 'Document Index'")
	}
	if !strings.Contains(body, "sample-doc") {
		t.Errorf("expected body to list 'sample-doc'")
	}
	if !strings.Contains(body, "aui-search-input") {
		t.Errorf("expected search box")
	}
	if !strings.Contains(body, "aui-filter-btn") {
		t.Errorf("expected filter tabs")
	}
}

func TestDocumentViewer(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Test HTML viewer page (not raw)
	req := httptest.NewRequest(http.MethodGet, "/sample-doc", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Verify "/pagebox" header with back arrow
	if !strings.Contains(body, "/pagebox") {
		t.Errorf("expected body to contain '/pagebox'")
	}
	if !strings.Contains(body, "←") {
		t.Errorf("expected back arrow '←' in viewer header")
	}
	if !strings.Contains(body, `href="/"`) {
		t.Errorf("expected back arrow link to point to index '/'")
	}

	// Verify iframe loading raw document
	if !strings.Contains(body, `<iframe id="doc-frame"`) {
		t.Errorf("expected iframe with id='doc-frame'")
	}
	if !strings.Contains(body, `src="/sample-doc?raw=1"`) {
		t.Errorf("expected iframe src='/sample-doc?raw=1', got body: %s", body)
	}

	// 2. Test raw document loading
	rawReq := httptest.NewRequest(http.MethodGet, "/sample-doc?raw=1", nil)
	rawRec := httptest.NewRecorder()

	srv.ServeHTTP(rawRec, rawReq)

	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for raw doc, got %d", rawRec.Code)
	}

	rawBody := rawRec.Body.String()
	if !strings.Contains(rawBody, "Hello World") {
		t.Errorf("expected raw body to contain 'Hello World', got: %s", rawBody)
	}
}

func TestDocumentNotFound(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/non-existent", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "404") {
		t.Errorf("expected 404 in body")
	}
	if !strings.Contains(body, "Return to /pagebox Index") {
		t.Errorf("expected return to index button")
	}
}

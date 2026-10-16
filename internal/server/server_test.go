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
		Port:         8732,
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
	if strings.Contains(body, "mirror active") {
		t.Errorf("expected body to NOT contain 'mirror active'")
	}
	if strings.Contains(body, "Local-first Static Mirror") {
		t.Errorf("expected body to NOT contain 'Local-first Static Mirror'")
	}
	if strings.Contains(body, "Document Index") {
		t.Errorf("expected body to NOT contain 'Document Index'")
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
	if !strings.Contains(body, "aui-icon-sun") || !strings.Contains(body, "aui-icon-moon") {
		t.Errorf("expected theme toggle to contain SVG sun and moon icons")
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
	if !strings.Contains(body, "aui-icon-sun") || !strings.Contains(body, "aui-icon-moon") {
		t.Errorf("expected viewer header to contain theme toggle SVG icons")
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

func TestNestedFilesAndFoldersUI(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Add nested entries to index
	_ = srv.idx.Update("docs/guides/setup", func(e *index.Entry) {
		e.ID = "docs/guides/setup"
		e.Key = "docs/guides/setup.html"
		e.State = index.StateCached
		e.Size = 256
		e.LastSyncedAt = time.Now()
	})
	_ = srv.idx.Update("docs/api/v1", func(e *index.Entry) {
		e.ID = "docs/api/v1"
		e.Key = "docs/api/v1.html"
		e.State = index.StateCached
		e.Size = 512
		e.LastSyncedAt = time.Now()
	})
	defer func() {
		_ = srv.idx.Delete("docs/guides/setup")
		_ = srv.idx.Delete("docs/api/v1")
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Verify folder rows and nested structure
	if !strings.Contains(body, "aui-tr-folder") {
		t.Errorf("expected HTML table to contain folder rows")
	}
	if !strings.Contains(body, "aui-folder-svg") {
		t.Errorf("expected folder SVG icons")
	}
	if !strings.Contains(body, `data-folder-path="docs"`) {
		t.Errorf("expected data-folder-path='docs'")
	}
	if !strings.Contains(body, `data-folder-path="docs/guides"`) {
		t.Errorf("expected data-folder-path='docs/guides'")
	}
	if !strings.Contains(body, `data-folder-path="docs/api"`) {
		t.Errorf("expected data-folder-path='docs/api'")
	}
	if !strings.Contains(body, "setup") || !strings.Contains(body, "v1") {
		t.Errorf("expected nested documents setup and v1 in table")
	}
}

func TestNestedDocumentViewerAndLazyLoading(t *testing.T) {
	srv, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()

	// Add placeholder document in nested directory
	docID := "cloud/services/database"
	docKey := "cloud/services/database.html"
	_ = srv.idx.Update(docID, func(e *index.Entry) {
		e.ID = docID
		e.Key = docKey
		e.State = index.StatePlaceholder
		e.RemoteETag = "dummy-etag"
		e.Size = 0
	})
	defer func() {
		_ = srv.idx.Delete(docID)
	}()

	// Verify file does not exist locally yet
	localExpected := filepath.Join(tmpDir, "cloud", "services", "database.html")
	if _, err := os.Stat(localExpected); !os.IsNotExist(err) {
		t.Fatalf("placeholder file should not exist on disk initially")
	}

	// Request document viewer for nested placeholder
	req := httptest.NewRequest(http.MethodGet, "/cloud/services/database", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()

	// 1. Verify lazy loading fetched remote content to local disk
	if _, err := os.Stat(localExpected); err != nil {
		t.Errorf("expected file to be lazy-loaded onto disk at %s: %v", localExpected, err)
	}
	diskContent, _ := os.ReadFile(localExpected)
	if !strings.Contains(string(diskContent), "Dummy Remote") {
		t.Errorf("expected disk content from dummy storage, got: %s", string(diskContent))
	}

	// 2. Verify index state transitioned to cached
	entry, ok := srv.idx.Get(docID)
	if !ok || entry.State != index.StateCached {
		t.Errorf("expected entry state to be cached after lazy load, got: %s", entry.State)
	}

	// 3. Verify breadcrumbs in header
	if !strings.Contains(body, "aui-breadcrumb-dir") {
		t.Errorf("expected breadcrumbs in header for nested document")
	}
	if !strings.Contains(body, "cloud") || !strings.Contains(body, "services") {
		t.Errorf("expected breadcrumbs to contain cloud and services")
	}

	// 4. Verify iframe points to raw nested URL without %2F in path slashes
	if !strings.Contains(body, `src="/cloud/services/database?raw=1"`) {
		t.Errorf("expected iframe src='/cloud/services/database?raw=1', got: %s", body)
	}

	// 5. Verify streaming loader is rendered
	if !strings.Contains(body, "frame-loader") {
		t.Errorf("expected frame-loader element in viewer")
	}

	// 6. Test raw fetch directly
	rawReq := httptest.NewRequest(http.MethodGet, "/cloud/services/database?raw=1", nil)
	rawRec := httptest.NewRecorder()
	srv.ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for raw, got %d", rawRec.Code)
	}
	if !strings.Contains(rawRec.Body.String(), "Dummy Remote") {
		t.Errorf("expected raw response to contain 'Dummy Remote', got: %s", rawRec.Body.String())
	}
}

func TestMarkdownDocumentServing(t *testing.T) {
	srv, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create a markdown document on disk
	mdContent := "# Getting Started with Pagebox\n\nThis is a **client-side** rendered markdown document.\n\n```bash\npagebox serve\n```\n\n| Feature | Status |\n| --- | --- |\n| Client-side Markdown | Supported |\n"
	mdPath := filepath.Join(tmpDir, "guides", "getting-started.md")
	_ = os.MkdirAll(filepath.Dir(mdPath), 0755)
	if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write test md file: %v", err)
	}

	// 2. Add to index
	_ = srv.idx.Update("guides/getting-started", func(e *index.Entry) {
		e.ID = "guides/getting-started"
		e.Key = "guides/getting-started.md"
		e.State = index.StateCached
		e.Size = int64(len(mdContent))
		e.LastSyncedAt = time.Now()
	})
	defer srv.idx.Delete("guides/getting-started")

	// 3. Test marked.min.js asset route
	assetReq := httptest.NewRequest(http.MethodGet, "/_pagebox/assets/marked.min.js", nil)
	assetRec := httptest.NewRecorder()
	srv.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for marked.min.js, got %d", assetRec.Code)
	}
	if ct := assetRec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content-type, got: %s", ct)
	}
	if !strings.Contains(assetRec.Body.String(), "marked") {
		t.Errorf("expected marked.min.js content to contain 'marked'")
	}

	// 4. Test accessing viewer via /guides/getting-started
	viewerReq := httptest.NewRequest(http.MethodGet, "/guides/getting-started", nil)
	viewerRec := httptest.NewRecorder()
	srv.ServeHTTP(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for viewer, got %d", viewerRec.Code)
	}
	viewerBody := viewerRec.Body.String()

	// Verify viewer contains /pagebox header and back link
	if !strings.Contains(viewerBody, "/pagebox") || !strings.Contains(viewerBody, "aui-back-link") {
		t.Errorf("expected viewer to contain /pagebox header and back link")
	}
	// Verify iframe points to render=1
	if !strings.Contains(viewerBody, `src="/guides/getting-started?render=1"`) {
		t.Errorf("expected iframe src to point to ?render=1, got: %s", viewerBody)
	}
	// Verify raw link points to ?raw=1
	if !strings.Contains(viewerBody, `href="/guides/getting-started?raw=1"`) {
		t.Errorf("expected raw link to point to ?raw=1, got: %s", viewerBody)
	}

	// 5. Test accessing client-side renderer via /guides/getting-started?render=1
	renderReq := httptest.NewRequest(http.MethodGet, "/guides/getting-started?render=1", nil)
	renderRec := httptest.NewRecorder()
	srv.ServeHTTP(renderRec, renderReq)
	if renderRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ?render=1, got %d", renderRec.Code)
	}
	renderBody := renderRec.Body.String()

	// Verify pure client-side architecture: raw markdown is embedded, marked.min.js is included
	if !strings.Contains(renderBody, `id="raw-markdown"`) {
		t.Errorf("expected raw-markdown script element in rendered template")
	}
	if !strings.Contains(renderBody, "Getting Started with Pagebox") {
		t.Errorf("expected raw markdown content to be present in script element")
	}
	if !strings.Contains(renderBody, "/_pagebox/assets/marked.min.js") {
		t.Errorf("expected script tag referencing /_pagebox/assets/marked.min.js")
	}
	if !strings.Contains(renderBody, "aui-prose") {
		t.Errorf("expected .aui-prose container in render template")
	}
	if !strings.Contains(renderBody, "aui-code-copy-btn") {
		t.Errorf("expected code copy button script in render template")
	}

	// 6. Test accessing raw markdown via /guides/getting-started?raw=1
	rawReq := httptest.NewRequest(http.MethodGet, "/guides/getting-started?raw=1", nil)
	rawRec := httptest.NewRecorder()
	srv.ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ?raw=1, got %d", rawRec.Code)
	}
	if ct := rawRec.Header().Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("expected text/markdown content type, got: %s", ct)
	}
	if rawRec.Body.String() != mdContent {
		t.Errorf("expected exact raw markdown content, got: %s", rawRec.Body.String())
	}

	// 7. Test index page lists the markdown document
	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	srv.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for index, got %d", indexRec.Code)
	}
	if !strings.Contains(indexRec.Body.String(), "getting-started") {
		t.Errorf("expected index page to list getting-started document")
	}
}


package server

import (
	"context"
	"fmt"
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
	if strings.HasPrefix(key, "non-existent") {
		return nil, "", storage.ErrNotFound
	}
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
	t.Setenv("HOME", tmpDir)

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

	// Verify robots and index files are not shown in UI document table
	if strings.Contains(body, `data-id="robots"`) || strings.Contains(body, `data-text="robots.txt"`) {
		t.Errorf("expected body to NOT list robots in document table")
	}
	if strings.Contains(body, `data-id="index"`) || strings.Contains(body, `data-text="index.html"`) {
		t.Errorf("expected body to NOT list index in document table")
	}
}

func TestRobotsAndIndexRoutes(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Test /robots.txt
	reqRobots := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	recRobots := httptest.NewRecorder()
	srv.ServeHTTP(recRobots, reqRobots)

	if recRobots.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /robots.txt, got %d", recRobots.Code)
	}
	if !strings.Contains(recRobots.Body.String(), "User-agent: *") || !strings.Contains(recRobots.Body.String(), "Disallow: /") {
		t.Errorf("expected robots.txt to disallow all crawlers, got:\n%s", recRobots.Body.String())
	}
	if !strings.HasPrefix(recRobots.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("expected text/plain Content-Type for robots.txt, got %s", recRobots.Header().Get("Content-Type"))
	}

	// 2. Test /index.html serves index page
	reqIndex := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	recIndex := httptest.NewRecorder()
	srv.ServeHTTP(recIndex, reqIndex)

	if recIndex.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /index.html, got %d", recIndex.Code)
	}
	if !strings.Contains(recIndex.Body.String(), "/pagebox") {
		t.Errorf("expected /index.html to serve index page")
	}

	// 3. Test /index serves index page
	reqIndex2 := httptest.NewRequest(http.MethodGet, "/index", nil)
	recIndex2 := httptest.NewRecorder()
	srv.ServeHTTP(recIndex2, reqIndex2)

	if recIndex2.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /index, got %d", recIndex2.Code)
	}
	if !strings.Contains(recIndex2.Body.String(), "/pagebox") {
		t.Errorf("expected /index to serve index page")
	}

	// Verify robots and index were never added to srv.idx
	if _, ok := srv.idx.Get("robots"); ok {
		t.Errorf("expected robots to NOT be stored in index")
	}
	if _, ok := srv.idx.Get("index"); ok {
		t.Errorf("expected index to NOT be stored in index")
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
	if !strings.Contains(body, "aui-file-svg") {
		t.Errorf("expected file SVG icons")
	}
	if strings.Contains(body, "aui-doc-glyph") {
		t.Errorf("did not expect aui-doc-glyph right arrow glyph")
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

func TestMarkdownFrontmatterAndTableRendering(t *testing.T) {
	srv, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()

	docID := "internals/verification-bottleneck"
	docKey := "internals/verification-bottleneck.md"
	docPath := filepath.Join(tmpDir, "internals", "verification-bottleneck.md")
	_ = os.MkdirAll(filepath.Dir(docPath), 0755)

	content := `---
title: "The Verification Bottleneck"
description: "Whitepaper on why code understanding limits speed."
---

Why the future of software engineering belongs to code understanding.

| Grade | Meaning |
| --- | --- |
| Confirmed | Proven by test |
`
	if err := os.WriteFile(docPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test doc: %v", err)
	}

	_ = srv.idx.Update(docID, func(e *index.Entry) {
		e.ID = docID
		e.Key = docKey
		e.State = index.StateCached
		e.RemoteETag = "dummy-etag"
		e.Size = int64(len(content))
	})

	// 1. Test viewer page extracts title into document title and breadcrumb
	viewerReq := httptest.NewRequest(http.MethodGet, "/"+docID, nil)
	viewerRec := httptest.NewRecorder()
	srv.ServeHTTP(viewerRec, viewerReq)

	if viewerRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", viewerRec.Code)
	}
	viewerBody := viewerRec.Body.String()
	if !strings.Contains(viewerBody, "<title>The Verification Bottleneck · /pagebox</title>") {
		t.Errorf("expected title to be 'The Verification Bottleneck', got:\n%s", viewerBody)
	}
	if !strings.Contains(viewerBody, "The Verification Bottleneck</span>") {
		t.Errorf("expected breadcrumb to show 'The Verification Bottleneck'")
	}

	// 2. Test viewer page with trailing slash
	trailingReq := httptest.NewRequest(http.MethodGet, "/"+docID+"/", nil)
	trailingRec := httptest.NewRecorder()
	srv.ServeHTTP(trailingRec, trailingReq)
	if trailingRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for trailing slash, got %d", trailingRec.Code)
	}

	// 3. Test rendered markdown page (?render=1)
	renderReq := httptest.NewRequest(http.MethodGet, "/"+docID+"?render=1", nil)
	renderRec := httptest.NewRecorder()
	srv.ServeHTTP(renderRec, renderReq)

	if renderRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ?render=1, got %d", renderRec.Code)
	}
	renderBody := renderRec.Body.String()

	// Should contain styled header
	if !strings.Contains(renderBody, `<h1 class="aui-prose-title">The Verification Bottleneck</h1>`) {
		t.Errorf("expected aui-prose-title with 'The Verification Bottleneck'")
	}
	if !strings.Contains(renderBody, `<meta name="description" content="Whitepaper on why code understanding limits speed.">`) {
		t.Errorf("expected meta description tag with description")
	}

	// Raw markdown script block must NOT contain the YAML delimiters
	if strings.Contains(renderBody, `<script type="text/markdown" id="raw-markdown">---`) {
		t.Errorf("expected raw-markdown script block to have frontmatter stripped")
	}

	// Must include table-wrap logic, heading anchor logic, layout, and TOC removed
	if !strings.Contains(renderBody, "aui-table-wrap") {
		t.Errorf("expected aui-table-wrap in render script")
	}
	if !strings.Contains(renderBody, "aui-heading-anchor") {
		t.Errorf("expected aui-heading-anchor in render script")
	}
	if !strings.Contains(renderBody, "aui-doc-layout") {
		t.Errorf("expected aui-doc-layout in render template")
	}
	if !strings.Contains(renderBody, "max-width: 1080px;") {
		t.Errorf("expected max-width: 1080px in render template")
	}
	if strings.Contains(renderBody, "aui-toc-container") || strings.Contains(renderBody, "On this page") {
		t.Errorf("expected aui-toc-container ('On this page') to be removed from render template")
	}
}

func TestFaviconAndMainIcon(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	expectedIconURL := "https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4"

	// 1. Test /favicon.ico redirects to the custom icon URL
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 Found for /favicon.ico, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != expectedIconURL {
		t.Errorf("expected Location header %s, got %s", expectedIconURL, loc)
	}

	// 2. Test Index page contains favicon link and main icon image
	idxReq := httptest.NewRequest(http.MethodGet, "/", nil)
	idxRec := httptest.NewRecorder()
	srv.ServeHTTP(idxRec, idxReq)

	idxBody := idxRec.Body.String()
	if !strings.Contains(idxBody, fmt.Sprintf(`rel="icon" type="image/png" href="%s"`, expectedIconURL)) {
		t.Errorf("expected index page to contain favicon link")
	}
	if !strings.Contains(idxBody, fmt.Sprintf(`<img src="%s" alt="pagebox" class="aui-brand-icon">`, expectedIconURL)) {
		t.Errorf("expected index page to contain main brand icon")
	}

	// 3. Test document viewer contains favicon link and header brand icon
	viewReq := httptest.NewRequest(http.MethodGet, "/sample-doc", nil)
	viewRec := httptest.NewRecorder()
	srv.ServeHTTP(viewRec, viewReq)

	viewBody := viewRec.Body.String()
	if !strings.Contains(viewBody, fmt.Sprintf(`rel="icon" type="image/png" href="%s"`, expectedIconURL)) {
		t.Errorf("expected viewer page to contain favicon link")
	}
	if !strings.Contains(viewBody, fmt.Sprintf(`<img src="%s" alt="pagebox" class="aui-brand-icon">`, expectedIconURL)) {
		t.Errorf("expected viewer page to contain header brand icon")
	}
}

func TestFileOrderingMostRecentFirst(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	// Add older doc ("alpha-older" is alphabetically first, but older)
	_ = srv.idx.Update("alpha-older", func(e *index.Entry) {
		e.ID = "alpha-older"
		e.Key = "alpha-older.html"
		e.State = index.StateCached
		e.LastSyncedAt = now.Add(-2 * time.Hour)
	})
	// Add newer doc ("omega-newer" is alphabetically last, but newer)
	_ = srv.idx.Update("omega-newer", func(e *index.Entry) {
		e.ID = "omega-newer"
		e.Key = "omega-newer.html"
		e.State = index.StateCached
		e.LastSyncedAt = now
	})
	defer func() {
		_ = srv.idx.Delete("alpha-older")
		_ = srv.idx.Delete("omega-newer")
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	newerIdx := strings.Index(body, "omega-newer")
	olderIdx := strings.Index(body, "alpha-older")

	if newerIdx == -1 || olderIdx == -1 {
		t.Fatalf("expected both omega-newer and alpha-older in HTML table")
	}
	if newerIdx > olderIdx {
		t.Errorf("expected omega-newer (more recent) to appear before alpha-older in table, but got newer at %d, older at %d", newerIdx, olderIdx)
	}
}

func TestEmptyKeyHealingAndNoDirectoryListing(t *testing.T) {
	srv, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()

	// Create test html file
	docPath := filepath.Join(tmpDir, "vm-sandbox-filesystem-explained.html")
	if err := os.WriteFile(docPath, []byte("<!DOCTYPE html><html><body><h1>Fast Filesystems for VMs</h1></body></html>"), 0644); err != nil {
		t.Fatalf("failed to create doc: %v", err)
	}

	// Add entry with empty Key (simulating the state where key was blank)
	docID := "vm-sandbox-filesystem-explained"
	_ = srv.idx.Update(docID, func(e *index.Entry) {
		e.ID = docID
		e.Key = ""
		e.State = ""
		e.Size = 87850
	})
	defer func() {
		_ = srv.idx.Delete(docID)
	}()

	// 1. Request with trailing slash: /vm-sandbox-filesystem-explained/
	req := httptest.NewRequest(http.MethodGet, "/vm-sandbox-filesystem-explained/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	// Should serve the viewer, NOT a directory listing
	if !strings.Contains(body, "aui-viewer") {
		t.Errorf("expected response to be the document viewer UI")
	}
	if !strings.Contains(body, `src="/vm-sandbox-filesystem-explained?raw=1"`) {
		t.Errorf("expected iframe to load /vm-sandbox-filesystem-explained?raw=1, got: %s", body)
	}
	// Must NOT contain standard directory listing markers
	if strings.Contains(body, "<pre><a href=") {
		t.Errorf("expected response to NOT be a Go directory listing")
	}

	// 2. Request raw content: /vm-sandbox-filesystem-explained?raw=1
	rawReq := httptest.NewRequest(http.MethodGet, "/vm-sandbox-filesystem-explained?raw=1", nil)
	rawRec := httptest.NewRecorder()
	srv.ServeHTTP(rawRec, rawReq)

	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for raw doc, got %d", rawRec.Code)
	}
	rawBody := rawRec.Body.String()
	if !strings.Contains(rawBody, "Fast Filesystems for VMs") {
		t.Errorf("expected raw body to contain document content, got: %s", rawBody)
	}

	// 3. Verify entry.Key is healed in the index
	entry, ok := srv.idx.Get(docID)
	if !ok {
		t.Fatalf("expected entry to exist in index")
	}
	if entry.Key != "vm-sandbox-filesystem-explained.html" {
		t.Errorf("expected entry.Key to be healed to 'vm-sandbox-filesystem-explained.html', got '%s'", entry.Key)
	}
}

func TestThemeBridgeAndLocalStorageSync(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/sample-doc", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "pagebox-theme-bridge") {
		t.Errorf("expected viewer to contain pagebox-theme-bridge style injector")
	}
	if !strings.Contains(body, "localStorage.setItem('theme'") {
		t.Errorf("expected viewer to sync theme key in localStorage")
	}
	if !strings.Contains(body, "onload=\"onFrameLoaded(this)\"") {
		t.Errorf("expected iframe to have onload attribute calling onFrameLoaded")
	}
}

func TestAPIFetchAndLazyLoad(t *testing.T) {
	srv, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Add entry to index with state cached, but file does not exist on disk
	docID := "consistent-hashing-live"
	docKey := "consistent-hashing-live.html"
	_ = srv.idx.Update(docID, func(e *index.Entry) {
		e.ID = docID
		e.Key = docKey
		e.State = index.StateCached
		e.RemoteETag = "etag-chl"
		e.Size = 64762
	})
	defer func() {
		_ = srv.idx.Delete(docID)
	}()

	localExpected := filepath.Join(tmpDir, docKey)
	if _, err := os.Stat(localExpected); !os.IsNotExist(err) {
		t.Fatalf("file should not exist on disk initially")
	}

	// 2. Trigger lazy load via API endpoint
	apiReq := httptest.NewRequest(http.MethodPost, "/_pagebox/api/fetch?id="+docID, nil)
	apiRec := httptest.NewRecorder()
	srv.ServeHTTP(apiRec, apiReq)

	if apiRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from API fetch, got %d: %s", apiRec.Code, apiRec.Body.String())
	}

	if !strings.Contains(apiRec.Body.String(), `"status":"ok"`) {
		t.Errorf("expected json status ok, got: %s", apiRec.Body.String())
	}

	// 3. Verify file was saved onto local disk
	if _, err := os.Stat(localExpected); err != nil {
		t.Fatalf("expected file to exist on disk after API fetch: %v", err)
	}

	content, _ := os.ReadFile(localExpected)
	if !strings.Contains(string(content), "Dummy Remote") {
		t.Errorf("expected file content from dummy storage, got: %s", string(content))
	}

	// 4. Verify entry in index is cached
	e, ok := srv.idx.Get(docID)
	if !ok || e.State != index.StateCached {
		t.Errorf("expected index entry to be cached, got: %v", e.State)
	}

	// 5. Test direct GET to /consistent-hashing-live serves 200 OK document viewer
	docReq := httptest.NewRequest(http.MethodGet, "/"+docID, nil)
	docRec := httptest.NewRecorder()
	srv.ServeHTTP(docRec, docReq)

	if docRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from document viewer, got %d", docRec.Code)
	}

	// 6. Test API fetch for non-existent document
	badReq := httptest.NewRequest(http.MethodPost, "/_pagebox/api/fetch?id=non-existent-doc", nil)
	badRec := httptest.NewRecorder()
	srv.ServeHTTP(badRec, badReq)

	if badRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent doc fetch, got %d", badRec.Code)
	}
}


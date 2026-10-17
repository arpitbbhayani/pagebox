package server

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
	"github.com/arpitbhayani/pagebox/internal/storage"
)

const mainIconURL = "https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4"

//go:embed assets/marked.min.js
var markedJS []byte

type Server struct {
	cfg   pbconfig.Config
	idx   *index.Store
	store storage.Storage
}

func New(cfg pbconfig.Config, idx *index.Store, store storage.Storage) *Server {
	return &Server{
		cfg:   cfg,
		idx:   idx,
		store: store,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.Path)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
		return
	}

	if r.URL.Path == "/favicon.ico" {
		http.Redirect(w, r, mainIconURL, http.StatusFound)
		return
	}

	if r.URL.Path == "/_pagebox/assets/marked.min.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(markedJS)
		return
	}

	if r.URL.Path == "/" || r.URL.Path == "/pagebox" {
		s.serveIndex(w, r)
		return
	}

	// Reject path traversal
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	id := strings.Trim(r.URL.Path, "/")
	id = index.TrimDocExt(id)

	if id == "" {
		s.serveIndex(w, r)
		return
	}

	s.serveDocument(w, r, id)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	entries := s.idx.List()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	var cachedCount, staleCount, dirtyCount, divergedCount, placeholderCount, newCount int
	for _, e := range entries {
		switch e.State {
		case index.StateCached:
			cachedCount++
		case index.StateStale:
			staleCount++
		case index.StateDirty:
			dirtyCount++
		case index.StateDiverged:
			divergedCount++
		case index.StatePlaceholder:
			placeholderCount++
		case index.StateNew:
			newCount++
		}
	}

	tree := buildFileTree(entries)
	var rows strings.Builder
	renderTreeRows(tree, &rows)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>/pagebox</title>
<link rel="icon" type="image/png" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="apple-touch-icon" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Assistant:wght@300;400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&family=Lora:ital,wght@1,400;1,500&display=swap" rel="stylesheet">
<script>
(function() {
	var saved = localStorage.getItem('aui-theme');
	if (saved) {
		document.documentElement.setAttribute('data-theme', saved);
	} else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
		document.documentElement.setAttribute('data-theme', 'dark');
	}
})();
</script>
<style>
%s
</style>
</head>
<body>
<div class="aui-layout">
	<!-- Navbar -->
	<header class="aui-navbar">
		<div class="aui-nav-inner">
			<div class="aui-brand-group">
				<a href="/" class="aui-brand">
					<img src="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4" alt="pagebox" class="aui-brand-icon">
					<span class="aui-brand-symbol">/</span>pagebox
				</a>
			</div>
			<div class="aui-nav-tools">
				<span class="aui-doc-counter">%d documents</span>
				<button id="theme-btn" class="aui-theme-toggle" type="button" aria-label="Toggle theme" title="Toggle theme">
					<svg class="aui-icon-sun" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
						<circle cx="12" cy="12" r="4"/>
						<path d="M12 2v2"/>
						<path d="M12 20v2"/>
						<path d="m4.93 4.93 1.41 1.41"/>
						<path d="m17.66 17.66 1.41 1.41"/>
						<path d="M2 12h2"/>
						<path d="M20 12h2"/>
						<path d="m6.34 17.66-1.41 1.41"/>
						<path d="m19.07 4.93-1.41 1.41"/>
					</svg>
					<svg class="aui-icon-moon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
						<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>
					</svg>
				</button>
			</div>
		</div>
	</header>

	<!-- Main Content -->
	<main class="aui-main">
		<!-- Stats / Telemetry -->
		<section class="aui-hero">
			<div class="aui-telemetry-row">
				<span class="aui-pill aui-pill-cached"><span class="aui-pill-dot"></span>%d cached</span>
				%s
				%s
				%s
				%s
				%s
				<span class="aui-pill aui-mono-pill">:port %d</span>
				<span class="aui-pill aui-mono-pill">%s</span>
			</div>
		</section>

		<!-- Search & Filter Controls -->
		<section class="aui-controls-panel">
			<div class="aui-search-box">
				<span class="aui-search-icon">›</span>
				<input type="text" id="search-input" class="aui-search-input" placeholder="Search documents... (press /)" autocomplete="off" spellcheck="false">
				<kbd class="aui-kbd">/</kbd>
			</div>
			<div class="aui-filter-tabs">
				<button type="button" class="aui-filter-btn active" data-filter="all">All <span class="aui-filter-count">(%d)</span></button>
				<button type="button" class="aui-filter-btn" data-filter="cached">Cached <span class="aui-filter-count">(%d)</span></button>
				%s
				%s
				%s
			</div>
		</section>

		<!-- Data Table -->
		<div class="aui-table-container">
			<table class="aui-table">
				<thead>
					<tr>
						<th class="aui-th-doc">Document</th>
						<th class="aui-th-state">State</th>
						<th class="aui-th-size">Size</th>
						<th class="aui-th-time">Last Synced</th>
						<th class="aui-th-action"></th>
					</tr>
				</thead>
				<tbody id="doc-tbody">
					%s
				</tbody>
			</table>
			<div id="empty-state" class="aui-empty-box" style="display: %s;">
				<div class="aui-empty-glyph">∅</div>
				<div class="aui-empty-title">No documents found</div>
				<div class="aui-empty-hint">Try adjusting your search query or clear the active filter.</div>
			</div>
		</div>
	</main>

	<!-- Restrained Editorial Footer -->
	<footer class="aui-footer">
		<div class="aui-footer-inner">
			<span>/pagebox · local-first document sync</span>
			<span>explicit pull · push on change</span>
		</div>
	</footer>
</div>

<script>
(function() {
	// Theme toggle
	var themeBtn = document.getElementById('theme-btn');
	if (themeBtn) {
		themeBtn.addEventListener('click', function() {
			var current = document.documentElement.getAttribute('data-theme') ||
				(window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
			var next = current === 'dark' ? 'light' : 'dark';
			document.documentElement.setAttribute('data-theme', next);
			localStorage.setItem('aui-theme', next);
		});
	}

	// Filter and Search logic with Folder Tree support
	var searchInput = document.getElementById('search-input');
	var filterBtns = document.querySelectorAll('.aui-filter-btn');
	var tbody = document.getElementById('doc-tbody');
	var folderRows = tbody.querySelectorAll('.aui-tr-folder');
	var fileRows = tbody.querySelectorAll('.aui-tr-file');
	var emptyState = document.getElementById('empty-state');
	var activeFilter = 'all';

	function applyFilters() {
		var term = (searchInput.value || '').trim().toLowerCase();
		var visibleCount = 0;

		if (term) {
			// Search mode: show all matching files and reveal ancestor folders
			var matchingFolders = new Set();

			fileRows.forEach(function(row) {
				var id = (row.getAttribute('data-id') || '').toLowerCase();
				var name = (row.getAttribute('data-name') || '').toLowerCase();
				var state = (row.getAttribute('data-state') || '').toLowerCase();

				var matchesFilter = (activeFilter === 'all') || (state === activeFilter);
				var matchesSearch = id.indexOf(term) !== -1 || name.indexOf(term) !== -1;

				if (matchesFilter && matchesSearch) {
					row.style.display = '';
					visibleCount++;
					var parent = row.getAttribute('data-parent-folder') || '';
					var parts = parent.split('/');
					var cur = '';
					for (var i = 0; i < parts.length; i++) {
						if (parts[i]) {
							cur = cur ? (cur + '/' + parts[i]) : parts[i];
							matchingFolders.add(cur);
						}
					}
				} else {
					row.style.display = 'none';
				}
			});

			folderRows.forEach(function(fRow) {
				var fPath = fRow.getAttribute('data-folder-path');
				if (matchingFolders.has(fPath)) {
					fRow.style.display = '';
					var caret = fRow.querySelector('.aui-folder-caret');
					if (caret) caret.textContent = '▾';
				} else {
					fRow.style.display = 'none';
				}
			});
		} else {
			// Tree mode: honor folder expand/collapse and active filter
			var collapsed = new Set();
			folderRows.forEach(function(fRow) {
				if (fRow.getAttribute('data-expanded') === 'false') {
					collapsed.add(fRow.getAttribute('data-folder-path'));
				}
			});

			var filterFolders = new Set();
			if (activeFilter !== 'all') {
				fileRows.forEach(function(row) {
					var state = (row.getAttribute('data-state') || '').toLowerCase();
					if (state === activeFilter) {
						var parent = row.getAttribute('data-parent-folder') || '';
						var parts = parent.split('/');
						var cur = '';
						for (var i = 0; i < parts.length; i++) {
							if (parts[i]) {
								cur = cur ? (cur + '/' + parts[i]) : parts[i];
								filterFolders.add(cur);
							}
						}
					}
				});
			}

			fileRows.forEach(function(row) {
				var state = (row.getAttribute('data-state') || '').toLowerCase();
				var matchesFilter = (activeFilter === 'all') || (state === activeFilter);
				var parent = row.getAttribute('data-parent-folder') || '';

				var isHiddenByParent = false;
				var parts = parent.split('/');
				var cur = '';
				for (var i = 0; i < parts.length; i++) {
					if (parts[i]) {
						cur = cur ? (cur + '/' + parts[i]) : parts[i];
						if (collapsed.has(cur)) {
							isHiddenByParent = true;
							break;
						}
					}
				}

				if (matchesFilter && !isHiddenByParent) {
					row.style.display = '';
					visibleCount++;
				} else {
					row.style.display = 'none';
				}
			});

			folderRows.forEach(function(fRow) {
				var fPath = fRow.getAttribute('data-folder-path');
				var parent = fRow.getAttribute('data-parent-folder') || '';

				if (activeFilter !== 'all' && !filterFolders.has(fPath)) {
					fRow.style.display = 'none';
					return;
				}

				var isHiddenByParent = false;
				var parts = parent.split('/');
				var cur = '';
				for (var i = 0; i < parts.length; i++) {
					if (parts[i]) {
						cur = cur ? (cur + '/' + parts[i]) : parts[i];
						if (collapsed.has(cur)) {
							isHiddenByParent = true;
							break;
						}
					}
				}

				if (isHiddenByParent) {
					fRow.style.display = 'none';
				} else {
					fRow.style.display = '';
					var isExpanded = fRow.getAttribute('data-expanded') !== 'false';
					var caret = fRow.querySelector('.aui-folder-caret');
					if (caret) caret.textContent = isExpanded ? '▾' : '▸';
				}
			});
		}

		if (visibleCount === 0 && (term || activeFilter !== 'all')) {
			emptyState.style.display = 'block';
		} else {
			emptyState.style.display = 'none';
		}
	}

	folderRows.forEach(function(fRow) {
		fRow.addEventListener('click', function(e) {
			var isExp = fRow.getAttribute('data-expanded') !== 'false';
			fRow.setAttribute('data-expanded', isExp ? 'false' : 'true');
			var caret = fRow.querySelector('.aui-folder-caret');
			if (caret) caret.textContent = isExp ? '▸' : '▾';
			applyFilters();
		});
	});

	if (searchInput) {
		searchInput.addEventListener('input', applyFilters);
	}

	filterBtns.forEach(function(btn) {
		btn.addEventListener('click', function() {
			filterBtns.forEach(function(b) { b.classList.remove('active'); });
			btn.classList.add('active');
			activeFilter = btn.getAttribute('data-filter');
			applyFilters();
		});
	});

	// Keyboard shortcuts
	window.addEventListener('keydown', function(e) {
		if (e.key === '/' && document.activeElement !== searchInput) {
			e.preventDefault();
			if (searchInput) {
				searchInput.focus();
				searchInput.select();
			}
		} else if (e.key === 'Escape' && document.activeElement === searchInput) {
			searchInput.value = '';
			searchInput.blur();
			applyFilters();
		} else if (e.key === 'Enter' && document.activeElement === searchInput) {
			for (var i = 0; i < fileRows.length; i++) {
				if (fileRows[i].style.display !== 'none') {
					var link = fileRows[i].querySelector('.aui-doc-link');
					if (link) {
						window.location.href = link.href;
						break;
					}
				}
			}
		}
	});

	applyFilters();
})();
</script>
</body>
</html>`,
		sharedCSS(),
		len(entries),
		cachedCount,
		renderOptionalPill(staleCount, "stale", "stale"),
		renderOptionalPill(dirtyCount, "dirty", "local edits"),
		renderOptionalPill(divergedCount, "diverged", "diverged"),
		renderOptionalPill(placeholderCount, "placeholder", "remote"),
		renderOptionalPill(newCount, "new", "new"),
		s.cfg.Port,
		html.EscapeString(s.cfg.DocsDir),
		len(entries),
		cachedCount,
		renderOptionalFilterTab(staleCount, "stale", "Stale"),
		renderOptionalFilterTab(dirtyCount, "dirty", "Local"),
		renderOptionalFilterTab(placeholderCount, "placeholder", "Remote"),
		rows.String(),
		emptyDisplay(len(entries)),
	)

	w.Write([]byte(htmlContent))
}

func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, id string) {
	entry, ok := s.idx.Get(id)
	var localPath string

	if !ok {
		// Could be a new file on disk not yet in the index
		candidates := []string{
			id + ".html",
			id + ".md",
			id + ".markdown",
			id,
		}
		found := false
		for _, cand := range candidates {
			p := filepath.Join(s.cfg.DocsDir, filepath.FromSlash(cand))
			if _, err := os.Stat(p); err == nil {
				localPath = p
				entry = index.Entry{
					ID:    id,
					Key:   cand,
					State: index.StateNew,
				}
				found = true
				break
			}
		}
		if !found {
			s.serveNotFound(w, r, id)
			return
		}
	} else {
		localPath = filepath.Join(s.cfg.DocsDir, filepath.FromSlash(entry.Key))
		// Lazy fetch if placeholder or stale
		if entry.State == index.StatePlaceholder || entry.State == index.StateStale {
			err := s.fetch(id, entry, localPath)
			if err != nil {
				if _, statErr := os.Stat(localPath); statErr != nil {
					s.serveError(w, r, id, fmt.Sprintf("Failed to fetch document from remote storage: %v", err))
					return
				}
				log.Printf("fetch failed for %s, serving stale local copy: %v", id, err)
			}
		}
	}

	lowerPath := strings.ToLower(localPath)
	lowerKey := strings.ToLower(entry.Key)

	isHTML := strings.HasSuffix(lowerPath, ".html") || strings.HasSuffix(lowerPath, ".htm") || strings.HasSuffix(lowerKey, ".html")
	isMarkdown := strings.HasSuffix(lowerPath, ".md") || strings.HasSuffix(lowerPath, ".markdown") || strings.HasSuffix(lowerKey, ".md") || strings.HasSuffix(lowerKey, ".markdown")

	// If raw document is requested
	if r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("raw") == "true" {
		if isHTML {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else if isMarkdown {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		}
		http.ServeFile(w, r, localPath)
		return
	}

	// If client-side rendered markdown is requested (e.g. inside the iframe or standalone)
	if isMarkdown && (r.URL.Query().Get("render") == "1" || r.URL.Query().Get("render") == "true") {
		s.serveMarkdownRenderer(w, r, id, entry, localPath)
		return
	}

	// If non-HTML, non-Markdown file is requested directly (e.g. image or pdf)
	if !isHTML && !isMarkdown {
		http.ServeFile(w, r, localPath)
		return
	}

	// Serve the editorial viewer with the required "/pagebox" header, back arrow, and iframe
	s.serveDocumentViewer(w, r, id, entry, isMarkdown)
}

func (s *Server) serveDocumentViewer(w http.ResponseWriter, r *http.Request, id string, entry index.Entry, isMarkdown bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	state := string(entry.State)
	if state == "" {
		state = "cached"
	}

	displayTitle := id
	if isMarkdown {
		localPath := filepath.Join(s.cfg.DocsDir, filepath.FromSlash(entry.Key))
		if b, err := os.ReadFile(localPath); err == nil {
			meta, _ := parseFrontmatter(string(b))
			if t, ok := meta["title"]; ok && strings.TrimSpace(t) != "" {
				displayTitle = strings.TrimSpace(t)
			}
		}
	}

	escapedID := html.EscapeString(displayTitle)
	rawURL := "/" + escapeURLPath(id) + "?raw=1"
	frameURL := rawURL
	if isMarkdown {
		frameURL = "/" + escapeURLPath(id) + "?render=1"
	}
	breadcrumbs := renderBreadcrumbs(id, displayTitle)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s · /pagebox</title>
<link rel="icon" type="image/png" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="apple-touch-icon" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Assistant:wght@300;400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<script>
// Breakout check: if this viewer is ever loaded inside an iframe, navigate top window instead
if (window.self !== window.top) {
	window.top.location.href = window.location.pathname;
}

(function() {
	var saved = localStorage.getItem('aui-theme');
	if (saved) {
		document.documentElement.setAttribute('data-theme', saved);
	} else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
		document.documentElement.setAttribute('data-theme', 'dark');
	}
})();
</script>
<style>
%s

/* Document Viewer Layout */
html, body {
	margin: 0;
	padding: 0;
	width: 100%%;
	height: 100%%;
	overflow: hidden;
	background: var(--aui-bg);
	color: var(--aui-text);
	font-family: var(--aui-font-sans);
	-webkit-font-smoothing: antialiased;
}

.aui-viewer {
	display: flex;
	flex-direction: column;
	width: 100vw;
	height: 100vh;
}

.aui-viewer-header {
	height: 44px;
	border-bottom: 1px solid var(--aui-border);
	background: var(--aui-bg-card);
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0 16px;
	flex-shrink: 0;
	box-sizing: border-box;
	z-index: 50;
}

.aui-header-left {
	display: flex;
	align-items: center;
	gap: 8px;
	min-width: 0;
	overflow: hidden;
}

.aui-back-link {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	text-decoration: none;
	font-family: var(--aui-font-mono);
	font-size: 13.5px;
	font-weight: 600;
	color: var(--aui-text);
	padding: 4px 8px;
	border-radius: 4px;
	border: 1px solid transparent;
	transition: all 0.15s ease;
	white-space: nowrap;
}

.aui-back-link:hover {
	background: var(--aui-bg-subtle);
	border-color: var(--aui-border);
	color: var(--aui-crimson);
}

.aui-back-arrow {
	color: var(--aui-crimson);
	font-size: 14px;
	line-height: 1;
	transition: transform 0.15s ease;
}

.aui-back-link:hover .aui-back-arrow {
	transform: translateX(-3px);
}

.aui-header-brand-name {
	letter-spacing: -0.01em;
}

.aui-header-sep {
	color: var(--aui-text-faint);
	font-family: var(--aui-font-mono);
	font-size: 13px;
	user-select: none;
}

.aui-header-doc-title {
	font-family: var(--aui-font-mono);
	font-size: 13px;
	font-weight: 500;
	color: var(--aui-text-muted);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.aui-header-right {
	display: flex;
	align-items: center;
	gap: 10px;
	flex-shrink: 0;
}

.aui-raw-btn {
	display: inline-flex;
	align-items: center;
	gap: 4px;
	font-family: var(--aui-font-mono);
	font-size: 11.5px;
	padding: 3px 8px;
	border-radius: 4px;
	background: var(--aui-bg-subtle);
	border: 1px solid var(--aui-border);
	color: var(--aui-text-muted);
	text-decoration: none;
	transition: all 0.15s ease;
}

.aui-raw-btn:hover {
	color: var(--aui-text);
	border-color: var(--aui-border-subtle);
	background: var(--aui-bg-hover);
}

.aui-frame-container {
	flex: 1;
	width: 100%%;
	height: calc(100vh - 44px);
	position: relative;
	background: var(--aui-bg);
}

.aui-doc-iframe {
	width: 100%%;
	height: 100%%;
	border: none;
	display: block;
	background: var(--aui-bg);
}
</style>
</head>
<body>
<div class="aui-viewer">
	<!-- Fixed Header: "/pagebox" with back arrow taking to index -->
	<header class="aui-viewer-header">
		<div class="aui-header-left">
			<a href="/" class="aui-back-link" title="Return to Index">
				<span class="aui-back-arrow">←</span>
				<img src="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4" alt="pagebox" class="aui-brand-icon">
				<span class="aui-header-brand-name">/pagebox</span>
			</a>
			<span class="aui-header-sep">/</span>
			%s
		</div>
		<div class="aui-header-right">
			<span class="aui-pill aui-pill-%s">
				<span class="aui-pill-dot"></span>
				<span>%s</span>
			</span>
			<a href="%s" target="_blank" class="aui-raw-btn" title="Open raw document in new tab">raw ↗</a>
			<button id="theme-btn" class="aui-theme-toggle" type="button" aria-label="Toggle theme" title="Toggle theme">
				<svg class="aui-icon-sun" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<circle cx="12" cy="12" r="4"/>
					<path d="M12 2v2"/>
					<path d="M12 20v2"/>
					<path d="m4.93 4.93 1.41 1.41"/>
					<path d="m17.66 17.66 1.41 1.41"/>
					<path d="M2 12h2"/>
					<path d="M20 12h2"/>
					<path d="m6.34 17.66-1.41 1.41"/>
					<path d="m19.07 4.93-1.41 1.41"/>
				</svg>
				<svg class="aui-icon-moon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>
				</svg>
			</button>
		</div>
	</header>

	<!-- Document Body: Loaded in an iframe -->
	<div class="aui-frame-container">
		<div id="frame-loader" class="aui-frame-loader">
			<span class="aui-loader-spinner"></span>
			<span>Streaming document...</span>
		</div>
		<iframe id="doc-frame" class="aui-doc-iframe" src="%s" title="%s"></iframe>
	</div>
</div>

<script>
(function() {
	var themeBtn = document.getElementById('theme-btn');
	var frame = document.getElementById('doc-frame');

	function getTheme() {
		return document.documentElement.getAttribute('data-theme') ||
			(window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
	}

	function applyThemeToFrame(theme) {
		try {
			if (frame && frame.contentDocument && frame.contentDocument.documentElement) {
				frame.contentDocument.documentElement.setAttribute('data-theme', theme);
			}
		} catch (e) {}
	}

	if (themeBtn) {
		themeBtn.addEventListener('click', function() {
			var current = getTheme();
			var next = current === 'dark' ? 'light' : 'dark';
			document.documentElement.setAttribute('data-theme', next);
			localStorage.setItem('aui-theme', next);
			applyThemeToFrame(next);
		});
	}

	if (frame) {
		frame.addEventListener('load', function() {
			var loader = document.getElementById('frame-loader');
			if (loader) { loader.style.display = 'none'; }

			var theme = getTheme();
			applyThemeToFrame(theme);

			try {
				if (frame.contentDocument && frame.contentDocument.title) {
					document.title = frame.contentDocument.title + ' · /pagebox';
				}
			} catch (e) {}
		});

		// Forward initial hash to iframe if present
		if (window.location.hash) {
			var baseSrc = frame.getAttribute('src');
			if (baseSrc && baseSrc.indexOf('#') === -1) {
				frame.src = baseSrc + window.location.hash;
			}
		}

		// Listen for hash sync from iframe
		window.addEventListener('message', function(e) {
			if (e.data && e.data.type === 'pagebox-hashchange') {
				if (window.location.hash !== e.data.hash) {
					history.replaceState(null, null, e.data.hash || window.location.pathname);
				}
			}
		});
	}
})();
</script>
</body>
</html>`,
		escapedID,
		sharedCSS(),
		breadcrumbs,
		html.EscapeString(state),
		html.EscapeString(state),
		rawURL,
		frameURL,
		escapedID,
	)

	w.Write([]byte(htmlContent))
}

func parseFrontmatter(content string) (map[string]string, string) {
	meta := make(map[string]string)
	trimmed := strings.TrimLeft(content, "\r\n\t ")
	if !strings.HasPrefix(trimmed, "---") {
		return meta, content
	}

	rest := trimmed[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return meta, content
	}

	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return meta, content
	}

	yamlBlock := rest[:idx]
	markdown := rest[idx+4:]
	if strings.HasPrefix(markdown, "\r\n") {
		markdown = markdown[2:]
	} else if strings.HasPrefix(markdown, "\n") {
		markdown = markdown[1:]
	}

	lines := strings.Split(yamlBlock, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx != -1 {
			k := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
			v := strings.TrimSpace(line[colonIdx+1:])
			if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
				(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
				if len(v) >= 2 {
					v = v[1 : len(v)-1]
				}
			}
			meta[k] = v
		}
	}

	return meta, markdown
}

func startsWithH1(md string) bool {
	lines := strings.Split(strings.TrimSpace(md), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		return strings.HasPrefix(l, "# ")
	}
	return false
}

func (s *Server) serveMarkdownRenderer(w http.ResponseWriter, r *http.Request, id string, entry index.Entry, localPath string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	content, err := os.ReadFile(localPath)
	if err != nil {
		http.Error(w, "Failed to read document", http.StatusInternalServerError)
		return
	}

	meta, cleanMarkdown := parseFrontmatter(string(content))

	docTitle := id
	if t, ok := meta["title"]; ok && strings.TrimSpace(t) != "" {
		docTitle = strings.TrimSpace(t)
	}

	var headerHTML string
	if t, ok := meta["title"]; ok && strings.TrimSpace(t) != "" && !startsWithH1(cleanMarkdown) {
		headerHTML = fmt.Sprintf(`<header class="aui-prose-header"><h1 class="aui-prose-title">%s</h1></header>`,
			html.EscapeString(strings.TrimSpace(t)))
	}

	var metaDescTag string
	if desc, ok := meta["description"]; ok && strings.TrimSpace(desc) != "" {
		metaDescTag = fmt.Sprintf(`<meta name="description" content="%s">`, html.EscapeString(strings.TrimSpace(desc)))
	}

	escapedScript := escapeScriptContent(cleanMarkdown)
	escapedTitle := html.EscapeString(docTitle)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
%s
<title>%s · /pagebox</title>
<link rel="icon" type="image/png" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="apple-touch-icon" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Assistant:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&family=Lora:ital,wght@1,400;1,500&display=swap" rel="stylesheet">
<script>
(function() {
	var saved = localStorage.getItem('aui-theme');
	if (saved) {
		document.documentElement.setAttribute('data-theme', saved);
	} else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
		document.documentElement.setAttribute('data-theme', 'dark');
	}
})();
</script>
<style>
%s

/* Refined Editorial Markdown Styles */
:root {
	--aui-font-prose: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", "Noto Sans", Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji";
}

html, body {
	margin: 0;
	padding: 0;
	background: var(--aui-bg);
	color: var(--aui-text);
	font-family: var(--aui-font-prose);
	line-height: 1.75;
	-webkit-font-smoothing: antialiased;
}

.aui-doc-layout {
	max-width: 760px;
	margin: 0 auto;
	padding: 40px 24px 96px;
	box-sizing: border-box;
}

.aui-prose-container {
	width: 100%%;
}

.aui-prose {
	font-size: 16px;
	line-height: 1.75;
	color: var(--aui-text);
	word-break: break-word;
}

.aui-prose > *:first-child {
	margin-top: 0 !important;
}

/* Document Header */
.aui-prose-header {
	margin-bottom: 24px;
}

.aui-prose-title {
	font-family: var(--aui-font-prose);
	font-size: 36px;
	font-weight: 700;
	letter-spacing: -0.025em;
	line-height: 1.2;
	color: var(--aui-text);
	margin: 0 0 16px 0 !important;
}

/* Subtitle and Author Byline */
.aui-prose-subtitle {
	font-size: 18.5px;
	line-height: 1.55;
	color: var(--aui-text);
	font-weight: 500;
	margin-top: 0;
	margin-bottom: 12px;
}

.aui-prose-byline {
	font-size: 13.5px;
	line-height: 1.5;
	color: var(--aui-text-muted);
	font-family: var(--aui-font-mono);
	margin-top: 0;
	margin-bottom: 36px;
	padding-bottom: 20px;
	border-bottom: 1px solid var(--aui-border-subtle);
}

/* Headings */
.aui-prose h1, .aui-prose h2, .aui-prose h3, .aui-prose h4, .aui-prose h5, .aui-prose h6 {
	font-family: var(--aui-font-prose);
	color: var(--aui-text);
	letter-spacing: -0.02em;
	line-height: 1.28;
	position: relative;
	scroll-margin-top: 28px;
}

.aui-prose h1 {
	font-size: 32px;
	font-weight: 700;
	margin-top: 48px;
	margin-bottom: 20px;
}

.aui-prose h2 {
	font-size: 23px;
	font-weight: 600;
	margin-top: 48px;
	margin-bottom: 16px;
}

.aui-prose h3 {
	font-size: 17.5px;
	font-weight: 600;
	margin-top: 32px;
	margin-bottom: 12px;
}

.aui-prose h4, .aui-prose h5, .aui-prose h6 {
	font-size: 14px;
	font-weight: 600;
	color: var(--aui-text-muted);
	text-transform: uppercase;
	letter-spacing: 0.04em;
	margin-top: 24px;
	margin-bottom: 8px;
}

.aui-heading-anchor {
	opacity: 0;
	margin-left: 8px;
	color: var(--aui-text-faint);
	text-decoration: none !important;
	font-weight: 400;
	font-size: 0.85em;
	transition: opacity 0.15s ease, color 0.15s ease;
}

.aui-prose h1:hover .aui-heading-anchor,
.aui-prose h2:hover .aui-heading-anchor,
.aui-prose h3:hover .aui-heading-anchor,
.aui-prose h4:hover .aui-heading-anchor {
	opacity: 1;
}

.aui-heading-anchor:hover {
	color: var(--aui-crimson) !important;
}

/* Paragraphs & Text */
.aui-prose p {
	margin-top: 0;
	margin-bottom: 20px;
}

.aui-prose a {
	color: var(--aui-blue);
	text-decoration: underline;
	text-decoration-thickness: 1px;
	text-underline-offset: 2.5px;
	text-decoration-color: var(--aui-blue-border);
	transition: all 0.15s ease;
}

.aui-prose a:hover {
	color: var(--aui-crimson);
	text-decoration-color: var(--aui-crimson);
}

.aui-prose strong {
	font-weight: 600;
	color: var(--aui-text);
}

.aui-prose em {
	font-family: var(--aui-font-serif);
	font-style: italic;
}

/* Blockquotes */
.aui-prose blockquote {
	margin: 28px 0;
	padding: 14px 22px;
	background: var(--aui-bg-subtle);
	border-left: 3px solid var(--aui-crimson);
	border-radius: 0 4px 4px 0;
	font-family: var(--aui-font-serif);
	font-style: italic;
	font-size: 16.5px;
	line-height: 1.7;
	color: var(--aui-text);
}

.aui-prose blockquote p {
	margin: 0;
}

.aui-prose blockquote p + p {
	margin-top: 10px;
}

/* Inline Code & Blocks */
.aui-prose code {
	font-family: var(--aui-font-mono);
	font-size: 13px;
	background: var(--aui-bg-subtle);
	border: 1px solid var(--aui-border);
	border-radius: 4px;
	padding: 2px 6px;
	color: var(--aui-text);
	white-space: nowrap;
}

.aui-prose pre code {
	white-space: pre;
}

.aui-prose pre {
	position: relative;
	background: var(--aui-bg-subtle);
	border: 1px solid var(--aui-border);
	border-radius: 6px;
	padding: 18px 20px;
	margin: 24px 0;
	overflow-x: auto;
	line-height: 1.55;
}

.aui-prose pre code {
	background: none;
	border: none;
	padding: 0;
	font-size: 13.5px;
	color: var(--aui-text);
}

.aui-code-copy-btn {
	position: absolute;
	top: 8px;
	right: 8px;
	display: inline-flex;
	align-items: center;
	gap: 4px;
	font-family: var(--aui-font-mono);
	font-size: 11px;
	padding: 3px 8px;
	border-radius: 4px;
	background: var(--aui-bg-card);
	border: 1px solid var(--aui-border);
	color: var(--aui-text-muted);
	cursor: pointer;
	opacity: 0.7;
	transition: all 0.15s ease;
	user-select: none;
}

.aui-prose pre:hover .aui-code-copy-btn {
	opacity: 1;
}

.aui-code-copy-btn:hover {
	color: var(--aui-text);
	border-color: var(--aui-crimson);
}

.aui-code-copy-btn.copied {
	color: var(--aui-emerald);
	border-color: var(--aui-emerald);
	opacity: 1;
}

/* Lists */
.aui-prose ul, .aui-prose ol {
	padding-left: 24px;
	margin: 0 0 22px;
}

.aui-prose li {
	margin: 8px 0;
	line-height: 1.7;
}

.aui-prose ol > li {
	padding-left: 4px;
}

.aui-prose ol > li::marker {
	font-family: var(--aui-font-mono);
	font-weight: 600;
	color: var(--aui-text-muted);
}

.aui-prose li > ul, .aui-prose li > ol {
	margin-top: 6px;
	margin-bottom: 6px;
}

.aui-prose input[type="checkbox"] {
	accent-color: var(--aui-crimson);
	margin-right: 6px;
}

/* Clean Editorial Tables */
.aui-table-wrap {
	width: 100%%;
	overflow-x: auto;
	margin: 32px 0;
	-webkit-overflow-scrolling: touch;
	border: 1px solid var(--aui-border);
	border-radius: 6px;
	background: var(--aui-bg-card);
}

.aui-table-wrap table {
	width: 100%%;
	border-collapse: collapse;
	margin: 0 !important;
	border: none !important;
	font-size: 13.5px;
	line-height: 1.5;
}

.aui-table-wrap th {
	background: var(--aui-bg-subtle);
	font-family: var(--aui-font-mono);
	font-size: 11.5px;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.05em;
	color: var(--aui-text-muted);
	padding: 10px 16px;
	border-bottom: 1px solid var(--aui-border);
	text-align: left;
	white-space: nowrap;
}

.aui-table-wrap td {
	padding: 12px 16px;
	border-bottom: 1px solid var(--aui-border-subtle);
	color: var(--aui-text);
	font-size: 13.5px;
	line-height: 1.55;
	vertical-align: top;
}

.aui-table-wrap tr:last-child td {
	border-bottom: none;
}

.aui-table-wrap tr:hover td {
	background: var(--aui-bg-subtle);
}

/* Horizontal Rule */
.aui-prose hr {
	border: none;
	border-top: 1px solid var(--aui-border);
	margin: 40px 0;
}

/* Images */
.aui-prose img {
	max-width: 100%%;
	height: auto;
	border-radius: 4px;
	border: 1px solid var(--aui-border);
	margin: 20px 0;
}

@media (max-width: 768px) {
	.aui-doc-layout {
		padding: 24px 16px 64px;
	}
}
</style>
<script src="/_pagebox/assets/marked.min.js"></script>
</head>
<body>
<div class="aui-doc-layout">
	<div class="aui-prose-container">
		<article class="aui-prose" id="content">
			%s
			<div id="prose-body">
				<div style="font-family: var(--aui-font-mono); color: var(--aui-text-muted); font-size: 13px;">Rendering document...</div>
			</div>
		</article>
	</div>
</div>

<script type="text/markdown" id="raw-markdown">%s</script>

<script>
(function() {
	function updateHash(hash) {
		try {
			if (window.parent && window.parent !== window) {
				window.parent.postMessage({ type: 'pagebox-hashchange', hash: hash }, '*');
			}
		} catch (e) {}
	}

	function renderMarkdown() {
		var scriptEl = document.getElementById('raw-markdown');
		var raw = scriptEl ? scriptEl.textContent : '';
		raw = raw.replace(/<\\\/script/gi, '<' + '/script>');

		var bodyEl = document.getElementById('prose-body');
		if (typeof marked !== 'undefined') {
			marked.setOptions({
				gfm: true,
				breaks: false
			});
			bodyEl.innerHTML = marked.parse(raw);
		} else {
			// Fallback if marked is loading or unavailable
			fetch(window.location.pathname + '?raw=1')
				.then(function(r) { return r.text(); })
				.then(function(txt) {
					var clean = txt.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, '');
					if (typeof marked !== 'undefined') {
						bodyEl.innerHTML = marked.parse(clean);
					} else {
						bodyEl.innerHTML = '<pre><code>' + clean.replace(/[&<>'"]/g, function(c) {
							return {'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c];
						}) + '</code></pre>';
					}
					enhanceProse();
				})
				.catch(function(err) {
					bodyEl.innerHTML = '<p style="color:var(--aui-rose);">Failed to render markdown: ' + err + '</p>';
				});
			return;
		}

		enhanceProse();
	}

	function enhanceProse() {
		var contentEl = document.getElementById('content');
		if (!contentEl) return;

		// Extract first h1 as document title if not set
		var h1 = contentEl.querySelector('h1');
		if (h1 && h1.textContent && !document.title) {
			document.title = h1.textContent.trim() + ' · /pagebox';
		}

		// Style subtitle and byline if present between title and first h2
		var firstH2 = contentEl.querySelector('h2');
		var firstP = contentEl.querySelector('#prose-body > p:first-of-type');
		if (firstP && (!firstH2 || (firstP.compareDocumentPosition(firstH2) & Node.DOCUMENT_POSITION_FOLLOWING))) {
			firstP.classList.add('aui-prose-subtitle');
			var secondP = firstP.nextElementSibling;
			if (secondP && secondP.tagName === 'P' && (!firstH2 || (secondP.compareDocumentPosition(firstH2) & Node.DOCUMENT_POSITION_FOLLOWING))) {
				secondP.classList.add('aui-prose-byline');
			}
		}

		// Ensure all headings have IDs for anchor links
		var headings = contentEl.querySelectorAll('h1, h2, h3, h4, h5, h6');
		headings.forEach(function(h) {
			if (!h.id) {
				var text = h.textContent.trim();
				var slug = text.toLowerCase()
					.replace(/<[^>]+>/g, '')
					.replace(/[^\w\s-]/g, '')
					.replace(/\s+/g, '-');
				if (slug) h.id = slug;
			}
			if (h.id && !h.querySelector('.aui-heading-anchor') && !h.classList.contains('aui-prose-title')) {
				var a = document.createElement('a');
				a.className = 'aui-heading-anchor';
				a.href = '#' + h.id;
				a.setAttribute('aria-label', 'Link to section');
				a.textContent = '#';
				h.appendChild(a);
			}
		});


		// Wrap tables for responsive horizontal scrolling
		contentEl.querySelectorAll('table').forEach(function(table) {
			if (table.parentElement && table.parentElement.classList.contains('aui-table-wrap')) return;
			var wrap = document.createElement('div');
			wrap.className = 'aui-table-wrap';
			table.parentNode.insertBefore(wrap, table);
			wrap.appendChild(table);
		});

		// Add copy buttons to pre blocks
		contentEl.querySelectorAll('pre').forEach(function(pre) {
			if (pre.querySelector('.aui-code-copy-btn')) return;
			var btn = document.createElement('button');
			btn.className = 'aui-code-copy-btn';
			btn.type = 'button';
			btn.title = 'Copy code';
			btn.innerHTML = '<span class="aui-copy-text">Copy</span>';
			btn.addEventListener('click', function() {
				var code = pre.querySelector('code');
				var text = code ? code.innerText : pre.innerText;
				navigator.clipboard.writeText(text).then(function() {
					btn.classList.add('copied');
					btn.querySelector('.aui-copy-text').textContent = 'Copied!';
					setTimeout(function() {
						btn.classList.remove('copied');
						btn.querySelector('.aui-copy-text').textContent = 'Copy';
					}, 2000);
				});
			});
			pre.appendChild(btn);
		});

		// External links open in new tab, anchor links smooth scroll, internal links navigate top window
		contentEl.querySelectorAll('a').forEach(function(a) {
			var href = a.getAttribute('href');
			if (!href) return;
			if (href.startsWith('http://') || href.startsWith('https://')) {
				a.setAttribute('target', '_blank');
				a.setAttribute('rel', 'noopener noreferrer');
			} else if (href.startsWith('#')) {
				a.addEventListener('click', function(e) {
					var targetId = href.slice(1);
					var targetEl = document.getElementById(targetId);
					if (targetEl) {
						e.preventDefault();
						targetEl.scrollIntoView({ behavior: 'smooth' });
						updateHash(href);
						history.pushState(null, null, href);
					}
				});
			} else if (href.startsWith('/')) {
				if (window.self !== window.top) {
					a.setAttribute('target', '_top');
				}
			}
		});

		// Scroll to hash if loaded with anchor
		var initialHash = window.location.hash;
		if (initialHash) {
			var initialTarget = document.getElementById(initialHash.slice(1));
			if (initialTarget) {
				setTimeout(function() {
					initialTarget.scrollIntoView({ behavior: 'smooth' });
				}, 100);
			}
		}
	}

	// Listen for theme mutations from parent viewer frame
	var observer = new MutationObserver(function(mutations) {
		mutations.forEach(function(mutation) {
			if (mutation.type === 'attributes' && mutation.attributeName === 'data-theme') {
				var theme = document.documentElement.getAttribute('data-theme');
				if (theme) {
					localStorage.setItem('aui-theme', theme);
				}
			}
		});
	});
	observer.observe(document.documentElement, { attributes: true });

	// Initial render
	renderMarkdown();
})();
</script>
</body>
</html>`,
		metaDescTag,
		escapedTitle,
		sharedCSS(),
		headerHTML,
		escapedScript,
	)

	w.Write([]byte(htmlContent))
}

func escapeScriptContent(s string) string {
	s = strings.ReplaceAll(s, "</script", "<\\/script")
	s = strings.ReplaceAll(s, "</Script", "<\\/Script")
	s = strings.ReplaceAll(s, "</SCRIPT", "<\\/SCRIPT")
	s = strings.ReplaceAll(s, "<!--", "<\\!--")
	return s
}

func (s *Server) serveNotFound(w http.ResponseWriter, r *http.Request, id string) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	escapedID := html.EscapeString(id)
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Not Found · /pagebox</title>
<link rel="icon" type="image/png" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="apple-touch-icon" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Assistant:wght@400;600;700&family=IBM+Plex+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
%s
body { margin: 0; background: var(--aui-bg); color: var(--aui-text); font-family: var(--aui-font-sans); }
.aui-center { max-width: 600px; margin: 80px auto; padding: 24px; text-align: center; }
.aui-card-box { background: var(--aui-bg-card); border: 1px solid var(--aui-border); border-radius: 4px; padding: 36px 24px; }
.aui-error-code { font-family: var(--aui-font-mono); font-size: 13px; color: var(--aui-crimson); font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }
.aui-error-title { font-size: 22px; font-weight: 700; margin: 8px 0 12px; }
.aui-error-desc { font-size: 14.5px; color: var(--aui-text-muted); margin-bottom: 24px; }
</style>
</head>
<body>
<div class="aui-center">
	<div class="aui-card-box">
		<div class="aui-error-code">404 · Not Found</div>
		<h1 class="aui-error-title">Document Not Found</h1>
		<p class="aui-error-desc">The requested document <code class="aui-mono">%s</code> could not be found in the local index or docs folder.</p>
		<a href="/" class="aui-action-btn">← Return to /pagebox Index</a>
	</div>
</div>
</body>
</html>`, sharedCSS(), escapedID)

	w.Write([]byte(htmlContent))
}

func (s *Server) serveError(w http.ResponseWriter, r *http.Request, id string, message string) {
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	escapedID := html.EscapeString(id)
	escapedMsg := html.EscapeString(message)
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fetch Error · /pagebox</title>
<link rel="icon" type="image/png" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="apple-touch-icon" href="https://github.com/user-attachments/assets/3ed0b526-191c-40be-ba63-7ce6a32188f4">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Assistant:wght@400;600;700&family=IBM+Plex+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
%s
body { margin: 0; background: var(--aui-bg); color: var(--aui-text); font-family: var(--aui-font-sans); }
.aui-center { max-width: 600px; margin: 80px auto; padding: 24px; text-align: center; }
.aui-card-box { background: var(--aui-bg-card); border: 1px solid var(--aui-border); border-radius: 4px; padding: 36px 24px; }
.aui-error-code { font-family: var(--aui-font-mono); font-size: 13px; color: var(--aui-rose); font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }
.aui-error-title { font-size: 22px; font-weight: 700; margin: 8px 0 12px; }
.aui-error-desc { font-size: 14.5px; color: var(--aui-text-muted); margin-bottom: 24px; line-height: 1.5; }
</style>
</head>
<body>
<div class="aui-center">
	<div class="aui-card-box">
		<div class="aui-error-code">503 · Service Unavailable</div>
		<h1 class="aui-error-title">Remote Sync Error</h1>
		<p class="aui-error-desc">Failed to fetch <code class="aui-mono">%s</code> from remote storage.<br><small>%s</small></p>
		<a href="/" class="aui-action-btn">← Return to /pagebox Index</a>
	</div>
</div>
</body>
</html>`, sharedCSS(), escapedID, escapedMsg)

	w.Write([]byte(htmlContent))
}

func (s *Server) fetch(id string, entry index.Entry, localPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body, etag, err := s.store.Get(ctx, entry.Key, entry.RemoteETag)
	if err != nil {
		return err
	}
	defer body.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	hash := sha256.New()
	writer := io.MultiWriter(f, hash)

	written, err := io.Copy(writer, body)
	if err != nil {
		return err
	}

	sha256Hex := hex.EncodeToString(hash.Sum(nil))

	s.idx.Update(id, func(e *index.Entry) {
		e.State = index.StateCached
		e.BaseETag = etag
		e.RemoteETag = etag
		e.Size = written
		e.LocalSHA256 = sha256Hex
		e.LastSyncedAt = time.Now()
	})

	return nil
}

func sharedCSS() string {
	return `
:root {
	--aui-bg: #f9f8f5;
	--aui-bg-card: #ffffff;
	--aui-bg-subtle: #f2f0eb;
	--aui-bg-hover: #eae7df;
	--aui-border: #dfded9;
	--aui-border-subtle: #e8e6e0;
	--aui-text: #2b2a30;
	--aui-text-muted: #6b6964;
	--aui-text-faint: #9c9a92;

	--aui-crimson: #e5000f;
	--aui-crimson-bg: rgba(229, 0, 15, 0.08);
	--aui-crimson-border: rgba(229, 0, 15, 0.25);

	--aui-amber: #cc9900;
	--aui-amber-bg: rgba(204, 153, 0, 0.1);
	--aui-amber-border: rgba(204, 153, 0, 0.3);

	--aui-emerald: #10b981;
	--aui-emerald-bg: rgba(16, 185, 129, 0.1);
	--aui-emerald-border: rgba(16, 185, 129, 0.3);

	--aui-rose: #f43f5e;
	--aui-rose-bg: rgba(244, 63, 94, 0.1);
	--aui-rose-border: rgba(244, 63, 94, 0.3);

	--aui-blue: #2563eb;
	--aui-blue-bg: rgba(37, 99, 235, 0.1);
	--aui-blue-border: rgba(37, 99, 235, 0.3);

	--aui-violet: #8b5cf6;
	--aui-violet-bg: rgba(139, 92, 246, 0.1);
	--aui-violet-border: rgba(139, 92, 246, 0.3);

	--aui-font-sans: 'Assistant', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
	--aui-font-mono: 'IBM Plex Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
	--aui-font-serif: 'Lora', Georgia, serif;
}

[data-theme="dark"] {
	--aui-bg: #111113;
	--aui-bg-card: #18181b;
	--aui-bg-subtle: #141416;
	--aui-bg-hover: #222227;
	--aui-border: #27272c;
	--aui-border-subtle: #1f1f23;
	--aui-text: #d6d6dc;
	--aui-text-muted: #8e8d98;
	--aui-text-faint: #5a5963;

	--aui-crimson: #e60012;
	--aui-crimson-bg: rgba(230, 0, 18, 0.12);
	--aui-crimson-border: rgba(230, 0, 18, 0.35);

	--aui-amber: #eab308;
	--aui-amber-bg: rgba(234, 179, 8, 0.12);
	--aui-amber-border: rgba(234, 179, 8, 0.35);

	--aui-emerald: #10b981;
	--aui-emerald-bg: rgba(16, 185, 129, 0.12);
	--aui-emerald-border: rgba(16, 185, 129, 0.35);

	--aui-rose: #f43f5e;
	--aui-rose-bg: rgba(244, 63, 94, 0.12);
	--aui-rose-border: rgba(244, 63, 94, 0.35);

	--aui-blue: #3b82f6;
	--aui-blue-bg: rgba(59, 130, 246, 0.12);
	--aui-blue-border: rgba(59, 130, 246, 0.35);

	--aui-violet: #a78bfa;
	--aui-violet-bg: rgba(167, 139, 250, 0.12);
	--aui-violet-border: rgba(167, 139, 250, 0.35);
}

@media (prefers-color-scheme: dark) {
	:root:not([data-theme="light"]) {
		--aui-bg: #111113;
		--aui-bg-card: #18181b;
		--aui-bg-subtle: #141416;
		--aui-bg-hover: #222227;
		--aui-border: #27272c;
		--aui-border-subtle: #1f1f23;
		--aui-text: #d6d6dc;
		--aui-text-muted: #8e8d98;
		--aui-text-faint: #5a5963;

		--aui-crimson: #e60012;
		--aui-crimson-bg: rgba(230, 0, 18, 0.12);
		--aui-crimson-border: rgba(230, 0, 18, 0.35);

		--aui-amber: #eab308;
		--aui-amber-bg: rgba(234, 179, 8, 0.12);
		--aui-amber-border: rgba(234, 179, 8, 0.35);

		--aui-emerald: #10b981;
		--aui-emerald-bg: rgba(16, 185, 129, 0.12);
		--aui-emerald-border: rgba(16, 185, 129, 0.35);

		--aui-rose: #f43f5e;
		--aui-rose-bg: rgba(244, 63, 94, 0.12);
		--aui-rose-border: rgba(244, 63, 94, 0.35);

		--aui-blue: #3b82f6;
		--aui-blue-bg: rgba(59, 130, 246, 0.12);
		--aui-blue-border: rgba(59, 130, 246, 0.35);

		--aui-violet: #a78bfa;
		--aui-violet-bg: rgba(167, 139, 250, 0.12);
		--aui-violet-border: rgba(167, 139, 250, 0.35);
	}
}

*, *::before, *::after {
	box-sizing: border-box;
}

body {
	margin: 0;
	padding: 0;
	background-color: var(--aui-bg);
	color: var(--aui-text);
	font-family: var(--aui-font-sans);
	font-size: 15px;
	line-height: 1.5;
	letter-spacing: -0.015em;
	-webkit-font-smoothing: antialiased;
}

.aui-mono {
	font-family: var(--aui-font-mono);
}

/* Layout */
.aui-layout {
	min-height: 100vh;
	display: flex;
	flex-direction: column;
}

.aui-navbar {
	border-bottom: 1px solid var(--aui-border);
	background: var(--aui-bg-card);
	position: sticky;
	top: 0;
	z-index: 50;
}

.aui-nav-inner {
	max-width: 1080px;
	margin: 0 auto;
	padding: 0 24px;
	height: 52px;
	display: flex;
	align-items: center;
	justify-content: space-between;
}

.aui-brand-group {
	display: flex;
	align-items: center;
	gap: 12px;
}

.aui-brand {
	font-family: var(--aui-font-mono);
	font-weight: 600;
	font-size: 14px;
	color: var(--aui-text);
	text-decoration: none;
	letter-spacing: -0.02em;
	display: flex;
	align-items: center;
	gap: 6px;
}

.aui-brand-icon {
	width: 20px;
	height: 20px;
	border-radius: 4px;
	object-fit: contain;
	display: inline-block;
	vertical-align: middle;
	flex-shrink: 0;
}

.aui-brand-symbol {
	color: var(--aui-crimson);
}

.aui-brand:hover {
	color: var(--aui-crimson);
}



.aui-nav-tools {
	display: flex;
	align-items: center;
	gap: 14px;
}

.aui-doc-counter {
	font-family: var(--aui-font-mono);
	font-size: 12px;
	color: var(--aui-text-muted);
}

.aui-theme-toggle {
	background: transparent;
	border: 1px solid var(--aui-border);
	color: var(--aui-text-muted);
	width: 28px;
	height: 28px;
	border-radius: 4px;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	padding: 0;
	transition: all 0.15s ease;
}

.aui-theme-toggle:hover {
	background: var(--aui-bg-subtle);
	color: var(--aui-text);
	border-color: var(--aui-border-subtle);
}

.aui-theme-toggle svg {
	width: 14px;
	height: 14px;
	stroke-width: 2;
}

.aui-theme-toggle .aui-icon-sun {
	display: block;
}

.aui-theme-toggle .aui-icon-moon {
	display: none;
}

[data-theme="dark"] .aui-theme-toggle .aui-icon-sun {
	display: none;
}

[data-theme="dark"] .aui-theme-toggle .aui-icon-moon {
	display: block;
}

@media (prefers-color-scheme: dark) {
	:root:not([data-theme="light"]) .aui-theme-toggle .aui-icon-sun {
		display: none;
	}
	:root:not([data-theme="light"]) .aui-theme-toggle .aui-icon-moon {
		display: block;
	}
}

.aui-main {
	flex: 1;
	max-width: 1080px;
	width: 100%;
	margin: 0 auto;
	padding: 36px 24px 60px;
}

.aui-hero {
	margin-bottom: 20px;
}

.aui-telemetry-row {
	display: flex;
	flex-wrap: wrap;
	gap: 8px;
	align-items: center;
}

.aui-pill {
	display: inline-flex;
	align-items: center;
	gap: 5px;
	padding: 3px 8px;
	border-radius: 4px;
	font-family: var(--aui-font-mono);
	font-size: 11.5px;
	line-height: 1.3;
	border: 1px solid var(--aui-border);
	background: var(--aui-bg-subtle);
	color: var(--aui-text-muted);
	white-space: nowrap;
}

.aui-mono-pill {
	background: var(--aui-bg-card);
	color: var(--aui-text-faint);
}

.aui-pill-dot {
	width: 5px;
	height: 5px;
	border-radius: 50%;
	flex-shrink: 0;
}

.aui-pill-cached { background: var(--aui-emerald-bg); border-color: var(--aui-emerald-border); color: var(--aui-emerald); }
.aui-pill-cached .aui-pill-dot { background: var(--aui-emerald); }

.aui-pill-stale { background: var(--aui-amber-bg); border-color: var(--aui-amber-border); color: var(--aui-amber); }
.aui-pill-stale .aui-pill-dot { background: var(--aui-amber); }

.aui-pill-dirty { background: var(--aui-rose-bg); border-color: var(--aui-rose-border); color: var(--aui-rose); }
.aui-pill-dirty .aui-pill-dot { background: var(--aui-rose); }

.aui-pill-diverged { background: var(--aui-rose-bg); border-color: var(--aui-rose-border); color: var(--aui-rose); font-weight: 600; }
.aui-pill-diverged .aui-pill-dot { background: var(--aui-rose); }

.aui-pill-placeholder { background: var(--aui-blue-bg); border-color: var(--aui-blue-border); color: var(--aui-blue); }
.aui-pill-placeholder .aui-pill-dot { background: var(--aui-blue); }

.aui-pill-new { background: var(--aui-violet-bg); border-color: var(--aui-violet-border); color: var(--aui-violet); }
.aui-pill-new .aui-pill-dot { background: var(--aui-violet); }

/* Controls */
.aui-controls-panel {
	display: flex;
	flex-direction: column;
	gap: 12px;
	margin-bottom: 16px;
}

@media (min-width: 640px) {
	.aui-controls-panel {
		flex-direction: row;
		align-items: center;
		justify-content: space-between;
	}
}

.aui-search-box {
	position: relative;
	flex: 1;
	max-width: 440px;
}

.aui-search-input {
	width: 100%;
	background: var(--aui-bg-card);
	border: 1px solid var(--aui-border);
	color: var(--aui-text);
	font-family: var(--aui-font-mono);
	font-size: 13px;
	padding: 8px 36px 8px 30px;
	border-radius: 4px;
	outline: none;
	transition: all 0.15s ease;
}

.aui-search-input:focus {
	border-color: var(--aui-crimson);
	box-shadow: 0 0 0 1px var(--aui-crimson);
}

.aui-search-icon {
	position: absolute;
	left: 10px;
	top: 50%;
	transform: translateY(-50%);
	color: var(--aui-crimson);
	font-family: var(--aui-font-mono);
	font-weight: 600;
	font-size: 14px;
	pointer-events: none;
}

.aui-kbd {
	position: absolute;
	right: 10px;
	top: 50%;
	transform: translateY(-50%);
	font-family: var(--aui-font-mono);
	font-size: 10.5px;
	padding: 1px 5px;
	border-radius: 3px;
	background: var(--aui-bg-subtle);
	border: 1px solid var(--aui-border);
	color: var(--aui-text-muted);
	pointer-events: none;
}

.aui-filter-tabs {
	display: flex;
	gap: 4px;
	overflow-x: auto;
}

.aui-filter-btn {
	background: transparent;
	border: 1px solid transparent;
	padding: 5px 10px;
	border-radius: 4px;
	font-family: var(--aui-font-mono);
	font-size: 12px;
	color: var(--aui-text-muted);
	cursor: pointer;
	transition: all 0.15s ease;
	white-space: nowrap;
}

.aui-filter-btn:hover {
	background: var(--aui-bg-subtle);
	color: var(--aui-text);
}

.aui-filter-btn.active {
	background: var(--aui-bg-card);
	border-color: var(--aui-border);
	color: var(--aui-text);
	font-weight: 600;
}

.aui-filter-count {
	font-size: 10.5px;
	color: var(--aui-text-faint);
}

/* Table */
.aui-table-container {
	background: var(--aui-bg-card);
	border: 1px solid var(--aui-border);
	border-radius: 4px;
	overflow: hidden;
}

.aui-table {
	width: 100%;
	border-collapse: collapse;
	text-align: left;
}

.aui-table th {
	padding: 10px 16px;
	font-family: var(--aui-font-mono);
	font-size: 11px;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.05em;
	color: var(--aui-text-muted);
	background: var(--aui-bg-subtle);
	border-bottom: 1px solid var(--aui-border);
}

.aui-table td {
	padding: 11px 16px;
	border-bottom: 1px solid var(--aui-border-subtle);
	vertical-align: middle;
}

.aui-table tr:last-child td {
	border-bottom: none;
}

.aui-table tbody tr {
	transition: background 0.1s ease;
}

.aui-table tbody tr:hover {
	background: var(--aui-bg-subtle);
}

.aui-doc-link {
	display: inline-flex;
	align-items: center;
	gap: 8px;
	text-decoration: none;
	color: var(--aui-text);
	font-weight: 500;
	font-family: var(--aui-font-mono);
	font-size: 13.5px;
	letter-spacing: -0.01em;
}

.aui-doc-glyph {
	color: var(--aui-crimson);
	font-family: var(--aui-font-mono);
	font-size: 13px;
	transition: transform 0.15s ease;
}

.aui-doc-link:hover {
	color: var(--aui-crimson);
}

.aui-doc-link:hover .aui-doc-glyph {
	transform: translateX(2px);
}

.aui-td-size, .aui-td-time {
	font-size: 12.5px;
	color: var(--aui-text-muted);
	white-space: nowrap;
}

.aui-td-action {
	text-align: right;
	white-space: nowrap;
}

.aui-action-btn {
	display: inline-flex;
	align-items: center;
	gap: 4px;
	padding: 4px 10px;
	border-radius: 4px;
	font-family: var(--aui-font-mono);
	font-size: 12px;
	text-decoration: none;
	color: var(--aui-text-muted);
	background: var(--aui-bg-subtle);
	border: 1px solid var(--aui-border);
	transition: all 0.15s ease;
}

.aui-action-btn:hover {
	color: var(--aui-crimson);
	border-color: var(--aui-crimson);
	background: var(--aui-crimson-bg);
}

.aui-empty-box {
	padding: 56px 24px;
	text-align: center;
	border-top: 1px solid var(--aui-border);
	background: var(--aui-bg-card);
}

.aui-empty-glyph {
	font-family: var(--aui-font-mono);
	font-size: 26px;
	color: var(--aui-text-faint);
	margin-bottom: 8px;
}

.aui-empty-title {
	font-size: 15px;
	font-weight: 600;
	margin-bottom: 4px;
	color: var(--aui-text);
}

.aui-empty-hint {
	font-size: 13px;
	color: var(--aui-text-muted);
}

/* Tree and Folder Styling */
.aui-tr-folder {
	cursor: pointer;
	user-select: none;
	background: var(--aui-bg-subtle);
}

.aui-tr-folder:hover {
	background: var(--aui-bg-hover) !important;
}

.aui-folder-toggle {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	font-family: var(--aui-font-mono);
	font-size: 13.5px;
	font-weight: 600;
	color: var(--aui-text);
	text-decoration: none;
	border: none;
	background: transparent;
	padding: 0;
	cursor: pointer;
}

.aui-folder-caret {
	font-size: 11px;
	width: 14px;
	display: inline-block;
	color: var(--aui-crimson);
	transition: transform 0.15s ease;
}

.aui-folder-svg {
	color: var(--aui-amber);
	flex-shrink: 0;
}

.aui-folder-name {
	font-weight: 600;
	letter-spacing: -0.01em;
}

.aui-folder-count {
	font-size: 11px;
	font-weight: 400;
	color: var(--aui-text-faint);
	margin-left: 2px;
}

.aui-breadcrumb-dir {
	color: var(--aui-text-faint);
	font-family: var(--aui-font-mono);
	font-size: 13px;
}

.aui-frame-loader {
	position: absolute;
	top: 50%;
	left: 50%;
	transform: translate(-50%, -50%);
	display: flex;
	align-items: center;
	gap: 10px;
	font-family: var(--aui-font-mono);
	font-size: 13px;
	color: var(--aui-text-muted);
	pointer-events: none;
	z-index: 10;
}

.aui-loader-spinner {
	width: 16px;
	height: 16px;
	border: 2px solid var(--aui-border);
	border-top-color: var(--aui-crimson);
	border-radius: 50%;
	animation: aui-spin 0.8s linear infinite;
}

@keyframes aui-spin {
	to { transform: rotate(360deg); }
}

/* Footer */
.aui-footer {
	border-top: 1px solid var(--aui-border);
	padding: 20px 24px;
	background: var(--aui-bg-card);
	margin-top: auto;
}

.aui-footer-inner {
	max-width: 1080px;
	margin: 0 auto;
	display: flex;
	align-items: center;
	justify-content: space-between;
	font-family: var(--aui-font-mono);
	font-size: 12px;
	color: var(--aui-text-faint);
}
`
}

func renderOptionalPill(count int, state string, label string) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf(`<span class="aui-pill aui-pill-%s"><span class="aui-pill-dot"></span>%d %s</span>`,
		state, count, label)
}

func renderOptionalFilterTab(count int, filter string, label string) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf(`<button type="button" class="aui-filter-btn" data-filter="%s">%s <span class="aui-filter-count">(%d)</span></button>`,
		filter, label, count)
}

func emptyDisplay(totalEntries int) string {
	if totalEntries == 0 {
		return "block"
	}
	return "none"
}

func formatBytes(b int64) string {
	if b <= 0 {
		return "—"
	}
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}

func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	}
	if diff < 7*24*time.Hour {
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
	return t.Format("Jan 02")
}

type FileNode struct {
	Name       string
	Path       string
	IsDir      bool
	Entry      *index.Entry
	Children   []*FileNode
	DocCount   int
	ParentPath string
	Depth      int
}

func escapeURLPath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

func renderBreadcrumbs(id, title string) string {
	parts := strings.Split(id, "/")
	display := id
	if title != "" {
		display = title
	}
	if len(parts) <= 1 {
		return fmt.Sprintf(`<span class="aui-header-doc-title" id="header-doc-label">%s</span>`, html.EscapeString(display))
	}
	var sb strings.Builder
	for i, p := range parts {
		if i == len(parts)-1 {
			sb.WriteString(fmt.Sprintf(`<span class="aui-header-doc-title" id="header-doc-label">%s</span>`, html.EscapeString(display)))
		} else {
			sb.WriteString(fmt.Sprintf(`<span class="aui-breadcrumb-dir">%s</span><span class="aui-header-sep">/</span>`, html.EscapeString(p)))
		}
	}
	return sb.String()
}

func buildFileTree(entries []index.Entry) *FileNode {
	root := &FileNode{IsDir: true, Depth: -1}

	for i := range entries {
		e := entries[i]
		parts := strings.Split(e.ID, "/")
		curr := root

		for d := 0; d < len(parts)-1; d++ {
			part := parts[d]
			dirPath := strings.Join(parts[:d+1], "/")
			parentPath := ""
			if d > 0 {
				parentPath = strings.Join(parts[:d], "/")
			}

			var found *FileNode
			for _, ch := range curr.Children {
				if ch.IsDir && ch.Name == part {
					found = ch
					break
				}
			}
			if found == nil {
				found = &FileNode{
					Name:       part,
					Path:       dirPath,
					IsDir:      true,
					ParentPath: parentPath,
					Depth:      d,
				}
				curr.Children = append(curr.Children, found)
			}
			curr = found
		}

		fileName := parts[len(parts)-1]
		parentPath := ""
		if len(parts) > 1 {
			parentPath = strings.Join(parts[:len(parts)-1], "/")
		}

		fileNode := &FileNode{
			Name:       fileName,
			Path:       e.ID,
			IsDir:      false,
			Entry:      &e,
			ParentPath: parentPath,
			Depth:      len(parts) - 1,
		}
		curr.Children = append(curr.Children, fileNode)
	}

	computeDocCounts(root)
	sortTree(root)
	return root
}

func computeDocCounts(node *FileNode) int {
	if !node.IsDir {
		return 1
	}
	count := 0
	for _, ch := range node.Children {
		count += computeDocCounts(ch)
	}
	node.DocCount = count
	return count
}

func sortTree(node *FileNode) {
	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir // folders first
		}
		return node.Children[i].Name < node.Children[j].Name
	})
	for _, ch := range node.Children {
		if ch.IsDir {
			sortTree(ch)
		}
	}
}

func renderTreeRows(node *FileNode, sb *strings.Builder) {
	for _, ch := range node.Children {
		if ch.IsDir {
			padLeft := 16 + ch.Depth*22
			escapedPath := html.EscapeString(ch.Path)
			escapedParent := html.EscapeString(ch.ParentPath)
			escapedName := html.EscapeString(ch.Name)
			docPlural := "doc"
			if ch.DocCount != 1 {
				docPlural = "docs"
			}
			sb.WriteString(fmt.Sprintf(`
		<tr class="aui-tr aui-tr-folder" data-folder-path="%s" data-parent-folder="%s" data-depth="%d" data-expanded="true">
			<td class="aui-td-doc" style="padding-left: %dpx;">
				<span class="aui-folder-toggle">
					<span class="aui-folder-caret">▾</span>
					<svg class="aui-folder-svg" xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>
					<span class="aui-folder-name">%s</span>
					<span class="aui-folder-count">(%d %s)</span>
				</span>
			</td>
			<td class="aui-td-state">
				<span class="aui-pill aui-mono-pill">folder</span>
			</td>
			<td class="aui-td-size aui-mono">—</td>
			<td class="aui-td-time aui-mono">—</td>
			<td class="aui-td-action"></td>
		</tr>`,
				escapedPath,
				escapedParent,
				ch.Depth,
				padLeft,
				escapedName,
				ch.DocCount,
				docPlural,
			))
			renderTreeRows(ch, sb)
		} else {
			e := ch.Entry
			state := string(e.State)
			if state == "" {
				state = "cached"
			}
			escapedID := html.EscapeString(e.ID)
			escapedName := html.EscapeString(ch.Name)
			escapedParent := html.EscapeString(ch.ParentPath)
			urlPath := "/" + escapeURLPath(e.ID)
			sizeStr := formatBytes(e.Size)
			timeStr := formatRelativeTime(e.LastSyncedAt)
			padLeft := 16 + ch.Depth*22

			sb.WriteString(fmt.Sprintf(`
		<tr class="aui-tr aui-tr-file" data-id="%s" data-name="%s" data-state="%s" data-parent-folder="%s" data-depth="%d">
			<td class="aui-td-doc" style="padding-left: %dpx;">
				<a href="%s" class="aui-doc-link">
					<span class="aui-doc-glyph">›</span>
					<span class="aui-doc-name">%s</span>
				</a>
			</td>
			<td class="aui-td-state">
				<span class="aui-pill aui-pill-%s">
					<span class="aui-pill-dot"></span>
					<span>%s</span>
				</span>
			</td>
			<td class="aui-td-size aui-mono">%s</td>
			<td class="aui-td-time aui-mono">%s</td>
			<td class="aui-td-action">
				<a href="%s" class="aui-action-btn">Open →</a>
			</td>
		</tr>`,
				escapedID,
				escapedName,
				html.EscapeString(state),
				escapedParent,
				ch.Depth,
				padLeft,
				urlPath,
				escapedName,
				html.EscapeString(state),
				html.EscapeString(state),
				html.EscapeString(sizeStr),
				html.EscapeString(timeStr),
				urlPath,
			))
		}
	}
}

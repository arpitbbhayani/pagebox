package server

import (
	"context"
	"crypto/sha256"
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
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="6" fill="#111113"/><text x="16" y="22" font-size="18" font-family="monospace" font-weight="bold" fill="#e5000f" text-anchor="middle">/p</text></svg>`))
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

	id := strings.TrimPrefix(r.URL.Path, "/")
	id = strings.TrimSuffix(id, ".html")

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

	var rows strings.Builder
	for _, e := range entries {
		state := string(e.State)
		if state == "" {
			state = "cached"
		}
		escapedID := html.EscapeString(e.ID)
		urlPath := "/" + url.PathEscape(e.ID)

		sizeStr := formatBytes(e.Size)
		timeStr := formatRelativeTime(e.LastSyncedAt)

		rows.WriteString(fmt.Sprintf(`
		<tr class="aui-tr" data-id="%s" data-state="%s">
			<td class="aui-td-doc">
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
			html.EscapeString(state),
			urlPath,
			escapedID,
			html.EscapeString(state),
			html.EscapeString(state),
			html.EscapeString(sizeStr),
			html.EscapeString(timeStr),
			urlPath,
		))
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>/pagebox</title>
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
			<span>poll interval %s</span>
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

	// Filter and Search logic
	var searchInput = document.getElementById('search-input');
	var filterBtns = document.querySelectorAll('.aui-filter-btn');
	var tbody = document.getElementById('doc-tbody');
	var rows = tbody.querySelectorAll('.aui-tr');
	var emptyState = document.getElementById('empty-state');
	var activeFilter = 'all';

	function applyFilters() {
		var term = (searchInput.value || '').trim().toLowerCase();
		var visibleCount = 0;

		rows.forEach(function(row) {
			var id = (row.getAttribute('data-id') || '').toLowerCase();
			var state = (row.getAttribute('data-state') || '').toLowerCase();

			var matchesFilter = (activeFilter === 'all') || (state === activeFilter);
			var matchesSearch = !term || id.indexOf(term) !== -1;

			if (matchesFilter && matchesSearch) {
				row.style.display = '';
				visibleCount++;
			} else {
				row.style.display = 'none';
			}
		});

		if (visibleCount === 0) {
			emptyState.style.display = 'block';
		} else {
			emptyState.style.display = 'none';
		}
	}

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
			for (var i = 0; i < rows.length; i++) {
				if (rows[i].style.display !== 'none') {
					var link = rows[i].querySelector('.aui-doc-link');
					if (link) {
						window.location.href = link.href;
						break;
					}
				}
			}
		}
	});
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
		html.EscapeString(s.cfg.PollInterval),
	)

	w.Write([]byte(htmlContent))
}

func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, id string) {
	entry, ok := s.idx.Get(id)
	var localPath string

	if !ok {
		// Could be a new file on disk not yet in the index
		localPath = filepath.Join(s.cfg.DocsDir, id+".html")
		if _, err := os.Stat(localPath); err != nil {
			s.serveNotFound(w, r, id)
			return
		}
		entry = index.Entry{
			ID:    id,
			Key:   id + ".html",
			State: index.StateNew,
		}
	} else {
		localPath = filepath.Join(s.cfg.DocsDir, entry.Key)
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

	// If raw HTML document is requested (e.g. inside the iframe or direct download)
	if r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("raw") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, localPath)
		return
	}

	// Serve the editorial viewer with the required "/pagebox" header, back arrow, and iframe
	s.serveDocumentViewer(w, r, id, entry)
}

func (s *Server) serveDocumentViewer(w http.ResponseWriter, r *http.Request, id string, entry index.Entry) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	state := string(entry.State)
	if state == "" {
		state = "cached"
	}

	escapedID := html.EscapeString(id)
	rawURL := "/" + url.PathEscape(id) + "?raw=1"

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s · /pagebox</title>
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
				<span class="aui-header-brand-name">/pagebox</span>
			</a>
			<span class="aui-header-sep">/</span>
			<span class="aui-header-doc-title" id="header-doc-label">%s</span>
		</div>
		<div class="aui-header-right">
			<span class="aui-pill aui-pill-%s">
				<span class="aui-pill-dot"></span>
				<span>%s</span>
			</span>
			<a href="%s" target="_blank" class="aui-raw-btn" title="Open raw HTML in new tab">raw ↗</a>
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
			var theme = getTheme();
			applyThemeToFrame(theme);

			try {
				if (frame.contentDocument && frame.contentDocument.title) {
					document.title = frame.contentDocument.title + ' · /pagebox';
				}
			} catch (e) {}
		});
	}
})();
</script>
</body>
</html>`,
		escapedID,
		sharedCSS(),
		escapedID,
		html.EscapeString(state),
		html.EscapeString(state),
		rawURL,
		rawURL,
		escapedID,
	)

	w.Write([]byte(htmlContent))
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
	gap: 2px;
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

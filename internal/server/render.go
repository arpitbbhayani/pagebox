package server

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pbconfig "github.com/arpitbhayani/pagebox/internal/config"
	"github.com/arpitbhayani/pagebox/internal/index"
)

// GenerateIndexHTML generates the static HTML for index.html using the Array UI design system.
func GenerateIndexHTML(cfg pbconfig.Config, idx *index.Store) string {
	entries := idx.List()

	// Filter out index.html, robots.txt, and any ignored/system files so they are not shown on UI
	filtered := make([]index.Entry, 0, len(entries))
	for _, e := range entries {
		if index.IsIgnoredDoc(e.ID, e.Key) {
			continue
		}
		filtered = append(filtered, e)
	}
	entries = filtered

	for i := range entries {
		if entries[i].State == index.StateCached && entries[i].Key != "" {
			p := filepath.Join(cfg.DocsDir, filepath.FromSlash(entries[i].Key))
			if fi, err := os.Stat(p); err != nil || fi.IsDir() {
				entries[i].State = index.StatePlaceholder
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		tI := entries[i].Timestamp(cfg.DocsDir)
		tJ := entries[j].Timestamp(cfg.DocsDir)
		if !tI.Equal(tJ) {
			return tI.After(tJ)
		}
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

	tree := buildFileTree(entries, cfg.DocsDir)
	var rows strings.Builder
	renderTreeRows(tree, &rows)

	portInfo := ""
	if cfg.Port > 0 {
		portInfo = fmt.Sprintf(`<span class="aui-pill aui-mono-pill">:port %d</span>`, cfg.Port)
	}

	docsDirInfo := ""
	if cfg.DocsDir != "" {
		docsDirInfo = fmt.Sprintf(`<span class="aui-pill aui-mono-pill">%s</span>`, html.EscapeString(cfg.DocsDir))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
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
	var saved = localStorage.getItem('aui-theme') || localStorage.getItem('theme');
	var theme = saved || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
	document.documentElement.setAttribute('data-theme', theme);
	if (theme === 'dark') {
		document.documentElement.classList.add('dark');
	} else {
		document.documentElement.classList.remove('dark');
	}
	try {
		localStorage.setItem('aui-theme', theme);
		localStorage.setItem('theme', theme);
	} catch(_) {}
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
				%s
				%s
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
				localStorage.getItem('aui-theme') ||
				localStorage.getItem('theme') ||
				(window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
			var next = current === 'dark' ? 'light' : 'dark';
			document.documentElement.setAttribute('data-theme', next);
			if (next === 'dark') {
				document.documentElement.classList.add('dark');
			} else {
				document.documentElement.classList.remove('dark');
			}
			try {
				localStorage.setItem('aui-theme', next);
				localStorage.setItem('theme', next);
			} catch (_) {}
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
		var term = (searchInput ? searchInput.value : '').toLowerCase().trim();
		var visibleCount = 0;
		var filterFolders = new Set();

		fileRows.forEach(function(row) {
			var text = (row.getAttribute('data-text') || '').toLowerCase();
			var state = (row.getAttribute('data-state') || '').toLowerCase();
			var folderPath = row.getAttribute('data-folder-path') || '';

			var matchesFilter = (activeFilter === 'all') || (state === activeFilter);
			var matchesSearch = (!term) || (text.indexOf(term) !== -1);

			if (matchesFilter && matchesSearch) {
				row.style.display = '';
				visibleCount++;
				if (folderPath) {
					var parts = folderPath.split('/');
					var accum = '';
					for (var i = 0; i < parts.length; i++) {
						accum = accum ? (accum + '/' + parts[i]) : parts[i];
						filterFolders.add(accum);
					}
				}
			} else {
				row.style.display = 'none';
			}
		});

		// Manage collapsed states and visibility of folder rows
		var collapsed = new Set();
		folderRows.forEach(function(fRow) {
			var fPath = fRow.getAttribute('data-folder-path');
			var isExp = fRow.getAttribute('data-expanded') !== 'false';
			if (!isExp) {
				collapsed.add(fPath);
			}
		});

		// Hide children of collapsed folders
		fileRows.forEach(function(row) {
			if (row.style.display === 'none') return;
			var fPath = row.getAttribute('data-folder-path');
			if (fPath) {
				var parts = fPath.split('/');
				var cur = '';
				for (var i = 0; i < parts.length; i++) {
					cur = cur ? (cur + '/' + parts[i]) : parts[i];
					if (collapsed.has(cur)) {
						row.style.display = 'none';
						break;
					}
				}
			}
		});

		// Folder row visibility
		if (folderRows.length > 0) {
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

	// Lazy load placeholder documents when clicked from index
	tbody.addEventListener('click', function(e) {
		var link = e.target.closest('.aui-doc-link, .aui-action-btn');
		if (!link) return;

		var row = link.closest('.aui-tr-file');
		if (!row) return;

		var state = (row.getAttribute('data-state') || '').toLowerCase();
		var id = row.getAttribute('data-id');

		if (state === 'placeholder') {
			e.preventDefault();
			var actionBtn = row.querySelector('.aui-action-btn');
			var docLink = row.querySelector('.aui-doc-link');
			if (actionBtn) {
				actionBtn.textContent = 'Loading...';
				actionBtn.style.opacity = '0.7';
				actionBtn.style.pointerEvents = 'none';
			}
			if (docLink) {
				docLink.style.opacity = '0.7';
			}

			fetch('/_pagebox/api/fetch?id=' + encodeURIComponent(id), {
				method: 'POST'
			})
			.then(function(res) {
				if (!res.ok) throw new Error('Fetch failed');
				return res.json();
			})
			.then(function() {
				window.location.href = '/' + encodeURIComponent(id);
			})
			.catch(function() {
				window.location.href = '/' + encodeURIComponent(id);
			});
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
		portInfo,
		docsDirInfo,
		len(entries),
		cachedCount,
		renderOptionalFilterTab(staleCount, "stale", "Stale"),
		renderOptionalFilterTab(dirtyCount, "dirty", "Local"),
		renderOptionalFilterTab(placeholderCount, "placeholder", "Remote"),
		rows.String(),
		emptyDisplay(len(entries)),
	)
}

// RobotsTxtContent is the robots.txt content disallowing all web crawlers, AI agents, and scrapers.
const RobotsTxtContent = "User-agent: *\nDisallow: /\n"

// WriteRobotsFile writes robots.txt to cfg.DocsDir if it does not exist or needs update.
func WriteRobotsFile(cfg pbconfig.Config) error {
	if cfg.DocsDir == "" {
		return fmt.Errorf("docs directory not configured")
	}

	if err := os.MkdirAll(cfg.DocsDir, 0755); err != nil {
		return err
	}

	robotsPath := filepath.Join(cfg.DocsDir, "robots.txt")
	tmpPath := filepath.Join(cfg.DocsDir, fmt.Sprintf(".robots.txt.tmp-%d", os.Getpid()))

	if err := os.WriteFile(tmpPath, []byte(RobotsTxtContent), 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, robotsPath)
}

// WriteIndexFile writes the rendered index.html atomically to cfg.DocsDir.
func WriteIndexFile(cfg pbconfig.Config, idx *index.Store) error {
	if cfg.DocsDir == "" {
		return fmt.Errorf("docs directory not configured")
	}

	if err := os.MkdirAll(cfg.DocsDir, 0755); err != nil {
		return err
	}

	htmlContent := GenerateIndexHTML(cfg, idx)
	indexPath := filepath.Join(cfg.DocsDir, "index.html")
	tmpPath := filepath.Join(cfg.DocsDir, fmt.Sprintf(".index.html.tmp-%d", os.Getpid()))

	if err := os.WriteFile(tmpPath, []byte(htmlContent), 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, indexPath)
}

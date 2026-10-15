package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	if r.URL.Path == "/" {
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

	// Must be an HTML file mapping
	if !strings.HasSuffix(r.URL.Path, ".html") && r.URL.Path != "/"+id {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	s.serveDocument(w, r, id)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	entries := s.idx.List()
	
	html := `<!DOCTYPE html>
<html>
<head>
<title>Pagebox</title>
<style>
	body { font-family: system-ui, sans-serif; max-width: 800px; margin: 40px auto; line-height: 1.6; color: #333; }
	a { color: #0366d6; text-decoration: none; }
	a:hover { text-decoration: underline; }
	table { width: 100%; border-collapse: collapse; margin-top: 20px; }
	th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid #eee; }
	.badge { padding: 2px 6px; border-radius: 4px; font-size: 0.85em; }
	.cached { background: #e6ffed; color: #22863a; }
	.stale { background: #fff5b1; color: #735c0f; }
	.dirty { background: #ffeef0; color: #cb2431; }
	.placeholder { background: #f1f8ff; color: #0366d6; }
	.diverged { background: #ffeef0; color: #cb2431; font-weight: bold; }
</style>
</head>
<body>
<h1>Pagebox Index</h1>
<input type="text" id="search" placeholder="Search documents..." style="width: 100%; padding: 8px; margin-bottom: 20px; box-sizing: border-box;">
<table>
	<thead>
		<tr><th>Document</th><th>State</th></tr>
	</thead>
	<tbody id="list">
`
	for _, e := range entries {
		badgeClass := string(e.State)
		if badgeClass == "" {
			badgeClass = "unknown"
		}
		html += fmt.Sprintf(`<tr><td><a href="/%s">%s</a></td><td><span class="badge %s">%s</span></td></tr>`+"\n", 
			e.ID, e.ID, badgeClass, e.State)
	}

	html += `
	</tbody>
</table>
<script>
document.getElementById('search').addEventListener('input', function(e) {
	const term = e.target.value.toLowerCase();
	const rows = document.getElementById('list').getElementsByTagName('tr');
	for (let row of rows) {
		const text = row.cells[0].textContent.toLowerCase();
		row.style.display = text.includes(term) ? '' : 'none';
	}
});
</script>
</body>
</html>`

	w.Write([]byte(html))
}

func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, id string) {
	entry, ok := s.idx.Get(id)
	if !ok {
		// Could be a new file we haven't tracked yet, try serving from disk anyway
		localPath := filepath.Join(s.cfg.DocsDir, id+".html")
		if _, err := os.Stat(localPath); err == nil {
			http.ServeFile(w, r, localPath)
			return
		}
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	localPath := filepath.Join(s.cfg.DocsDir, entry.Key)

	// Lazy fetch if placeholder or stale
	if entry.State == index.StatePlaceholder || entry.State == index.StateStale {
		err := s.fetch(id, entry, localPath)
		if err != nil {
			// If S3 is unreachable, PRD says: "cached files are served and uncached files return a clear error page."
			if _, statErr := os.Stat(localPath); statErr == nil {
				// We have a local version (e.g. it was stale, not a placeholder), serve it.
				log.Printf("fetch failed for %s, serving stale local copy: %v", id, err)
			} else {
				http.Error(w, fmt.Sprintf("Failed to fetch document from remote: %v", err), http.StatusServiceUnavailable)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, localPath)
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

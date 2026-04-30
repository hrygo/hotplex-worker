package webchat

import (
	"io/fs"
	"net/http"
	"strings"
)

var spaFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(spaFS)

// Handler returns an http.Handler that serves the webchat SPA.
//
// Routing strategy:
//   - /_next/*  → static assets with aggressive cache headers (hashed filenames)
//   - exact file match (favicon, robots.txt) → serve directly
//   - everything else → fallback to index.html (client-side routing)
//
// Must be registered last on the ServeMux so explicit API/WS routes take priority.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Static assets with content-hashed filenames — cache for 1 year.
		if strings.HasPrefix(path, "/_next/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try exact file match (favicon.ico, robots.txt, etc.).
		relPath := strings.TrimPrefix(path, "/")
		if relPath != "" {
			if f, err := spaFS.Open(relPath); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for all non-file paths.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

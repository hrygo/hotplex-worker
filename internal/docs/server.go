package docs

import (
	"io/fs"
	"net/http"
)

var docsFS, _ = fs.Sub(StaticFS, "out")

var fileServer = http.FileServerFS(docsFS)

// securityHeaders injects security response headers for all docs responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Slightly more relaxed CSP than webchat because docs might have embedded diagrams/scripts if needed
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

// Handler returns an http.Handler that serves the static documentation.
func Handler() http.Handler {
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fileServer automatically handles index.html for directories.
		fileServer.ServeHTTP(w, r)
	}))
}

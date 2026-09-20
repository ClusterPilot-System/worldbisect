// Package hubweb serves the experimental team report hub dashboard.
package hubweb

import (
	"embed"
	"net/http"
)

//go:embed index.html app.js style.css
var assets embed.FS

// Handler serves only the dashboard's embedded, public static assets. API
// authentication is handled separately by the hub server.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var name, contentType string
		switch r.URL.Path {
		case "/":
			name, contentType = "index.html", "text/html; charset=utf-8"
		case "/app.js":
			name, contentType = "app.js", "text/javascript; charset=utf-8"
		case "/style.css":
			name, contentType = "style.css", "text/css; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		data, err := assets.ReadFile(name)
		if err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	})
}

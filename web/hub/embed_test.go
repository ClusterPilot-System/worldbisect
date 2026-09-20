package hubweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRestrictsStaticSurface(t *testing.T) {
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("%s: status %d, bytes %d", path, response.Code, response.Body.Len())
		}
		policy := response.Header().Get("Content-Security-Policy")
		for _, directive := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(policy, directive) {
				t.Errorf("%s missing CSP directive %s", path, directive)
			}
		}
		if strings.Contains(policy, "unsafe-inline") || response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s has unsafe inline or cache policy", path)
		}
	}
	for _, path := range []string{"/index.html", "/app.test.js", "/embed.go", "/api/v1/reports", "/../index.html"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("unexpectedly served %s: %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / status %d", response.Code)
	}
	response = httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/", nil))
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Errorf("HEAD / status %d, bytes %d", response.Code, response.Body.Len())
	}
}

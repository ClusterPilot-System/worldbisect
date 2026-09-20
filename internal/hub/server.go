package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// NewHandler exposes only summary storage, never the diagnostic daemon or its
// execution endpoints. It intentionally does not accept cookie or query auth.
func NewHandler(store *Store, assets http.Handler) http.Handler {
	slots := make(chan struct{}, 16)
	limiter := &rateLimiter{buckets: make(map[string]rateBucket)}
	workspaceSlots := make(map[string]chan struct{})
	for _, key := range store.config.Keys {
		workspaceSlots[key.Workspace] = make(chan struct{}, 2)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/healthz" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				methodNotAllowed(w, "GET, HEAD")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				methodNotAllowed(w, "GET, HEAD")
				return
			}
			if assets == nil {
				http.NotFound(w, r)
				return
			}
			assets.ServeHTTP(w, r)
			return
		}
		key, ok := authenticate(r, store.config)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="worldbisect-hub"`)
			writeError(w, http.StatusUnauthorized, "a valid workspace bearer token is required")
			return
		}
		if !limiter.allow(key.TokenSHA256) {
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusTooManyRequests, "request rate exceeded; retry after two seconds")
			return
		}
		select {
		case workspaceSlots[key.Workspace] <- struct{}{}:
			defer func() { <-workspaceSlots[key.Workspace] }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "workspace already has two requests in progress; retry after one second")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "hub is busy; retry after one second")
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "API query parameters are not supported")
			return
		}
		if r.URL.Path == "/api/v1/session" {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, "GET")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"workspace": key.Workspace, "permission": key.Permission})
			return
		}
		if r.URL.Path == "/api/v1/reports" {
			switch r.Method {
			case http.MethodGet:
				reports, err := store.List(key.Workspace)
				if err != nil {
					storeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, struct {
					Reports []Report `json:"reports"`
				}{Reports: reports})
			case http.MethodPost:
				if key.Permission != "write" {
					writeError(w, http.StatusForbidden, "this token is read-only")
					return
				}
				mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mediaType != "application/json" {
					writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
				b, err := io.ReadAll(r.Body)
				if err != nil {
					var maxBytes *http.MaxBytesError
					if errors.As(err, &maxBytes) {
						writeError(w, http.StatusRequestEntityTooLarge, "summary exceeds the 16 KiB request limit")
						return
					}
					writeError(w, http.StatusBadRequest, "could not read JSON request")
					return
				}
				var submission Submission
				if err := decodeStrict(b, &submission); err != nil {
					writeError(w, http.StatusBadRequest, "invalid summary JSON: use only documented fields and one object")
					return
				}
				if err := submission.Validate(); err != nil {
					writeError(w, http.StatusUnprocessableEntity, err.Error())
					return
				}
				report, err := store.Create(key.Workspace, submission)
				if err != nil {
					storeError(w, err)
					return
				}
				w.Header().Set("Location", "/api/v1/reports/"+report.ID)
				writeJSON(w, http.StatusCreated, report)
			default:
				methodNotAllowed(w, "GET, POST")
			}
			return
		}
		prefix := "/api/v1/reports/"
		if strings.HasPrefix(r.URL.Path, prefix) && idPattern.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) {
			id := strings.TrimPrefix(r.URL.Path, prefix)
			switch r.Method {
			case http.MethodGet:
				report, err := store.Get(key.Workspace, id)
				if err != nil {
					storeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, report)
			case http.MethodDelete:
				if key.Permission != "write" {
					writeError(w, http.StatusForbidden, "this token is read-only")
					return
				}
				if err := store.Delete(key.Workspace, id); err != nil {
					storeError(w, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			default:
				methodNotAllowed(w, "GET, DELETE")
			}
			return
		}
		writeError(w, http.StatusNotFound, "API route not found")
	})
}

type rateBucket struct {
	tokens  float64
	updated time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

// Only authenticated, configured keys enter this map, bounding it to 100
// entries. Each key may burst 30 requests and refill one every two seconds.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, exists := l.buckets[key]
	if !exists {
		b = rateBucket{tokens: 30, updated: now}
	}
	b.tokens += now.Sub(b.updated).Seconds() / 2
	if b.tokens > 30 {
		b.tokens = 30
	}
	b.updated = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b
	return allowed
}

func authenticate(r *http.Request, config Config) (Key, bool) {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		return Key{}, false
	}
	token := strings.TrimPrefix(headers[0], "Bearer ")
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, " \t\r\n,") {
		return Key{}, false
	}
	digest := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(digest[:])
	var match Key
	found := false
	for _, key := range config.Keys {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(key.TokenSHA256)) == 1 {
			match = key
			found = true
		}
	}
	return match, found
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "report not found")
	case errors.Is(err, ErrQuota):
		writeError(w, http.StatusConflict, ErrQuota.Error())
	default:
		writeError(w, http.StatusInternalServerError, "report storage is unavailable; ask the hub operator to check the data directory")
	}
}

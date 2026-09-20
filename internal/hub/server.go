package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server controls access independently of the storage layer. Reload takes the
// write lock; store operations reauthorize under a read lock after body reads.
// Consequently a body started before revocation cannot commit after reload.
type Server struct {
	mu             sync.RWMutex
	config         Config
	store          *Store
	audit          *AuditLog
	ci             *ciVerifier
	assets         http.Handler
	slots          chan struct{}
	workspaceSlots map[string]chan struct{}
	limiter        *rateLimiter
}

func NewHandler(store *Store, assets http.Handler) http.Handler { return NewServer(store, assets, nil) }

func NewServer(store *Store, assets http.Handler, audit *AuditLog) *Server {
	store.mu.Lock()
	store.config = cloneConfig(store.config)
	store.mu.Unlock()
	s := &Server{config: store.config, store: store, audit: audit, assets: assets, slots: make(chan struct{}, 16), limiter: &rateLimiter{buckets: make(map[string]rateBucket)}, ci: newCIVerifier(store.root)}
	s.resetWorkspaceSlots(store.config)
	return s
}

func (s *Server) resetWorkspaceSlots(c Config) {
	next := make(map[string]chan struct{})
	for _, key := range c.Keys {
		next[key.Workspace] = s.workspaceSlots[key.Workspace]
		if next[key.Workspace] == nil {
			next[key.Workspace] = make(chan struct{}, 2)
		}
	}
	for _, member := range c.Memberships {
		next[member.Workspace] = s.workspaceSlots[member.Workspace]
		if next[member.Workspace] == nil {
			next[member.Workspace] = make(chan struct{}, 2)
		}
	}
	s.workspaceSlots = next
}

// Reload atomically replaces access configuration after validation. Retention
// and quota changes are applied to the store in the same critical section.
func (s *Server) Reload(config Config) error {
	config = cloneConfig(config)
	if err := config.Validate(); err != nil {
		_ = s.audit.Append(AuditEvent{Actor: "operator", Kind: "operator", Action: "config.reload", Outcome: "rejected"})
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.audit.Append(AuditEvent{Actor: "operator", Kind: "operator", Action: "config.reload", Outcome: "intent"}); err != nil {
		return err
	}
	s.store.mu.Lock()
	s.store.config = config
	s.store.mu.Unlock()
	s.config = config
	s.resetWorkspaceSlots(config)
	// Preserve active buckets so reload cannot reset a caller's burst allowance.
	s.limiter.retain(config)
	s.audit.SetRetention(config.AuditRetentionDays)
	return s.audit.Append(AuditEvent{Actor: "operator", Kind: "operator", Action: "config.reload", Outcome: "applied"})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" {
		if r.Method != "GET" && r.Method != "HEAD" {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		if s.assets == nil {
			http.NotFound(w, r)
			return
		}
		s.assets.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/ci/reports" {
		s.handleCI(w, r)
		return
	}
	s.mu.RLock()
	key, ok := authenticate(r, s.config)
	principal, valid := principalForKey(s.config, key, time.Now())
	slot := s.workspaceSlots[key.Workspace]
	s.mu.RUnlock()
	if !ok || !valid {
		s.authFailure(w)
		return
	}
	if !s.limiter.allow(key.TokenSHA256) {
		w.Header().Set("Retry-After", "2")
		writeError(w, 429, "request rate exceeded; retry after two seconds")
		return
	}
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, 429, "workspace already has two requests in progress; retry after one second")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, 503, "hub is busy; retry after one second")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "API query parameters are not supported")
		return
	}
	if r.URL.Path == "/api/v1/session" {
		if r.Method != "GET" {
			methodNotAllowed(w, "GET")
			return
		}
		s.authorized(w, r, "", "auth.success", func(p Principal) operationResult {
			permission := "read"
			if slices.Contains(p.Scopes, "reports:write") {
				permission = "write"
			}
			return operationResult{status: 200, value: map[string]any{"workspace": p.Workspace, "permission": permission, "subject_id": p.SubjectID, "kind": p.Kind, "credential_id": p.CredentialID, "scopes": p.Scopes, "role": principalRole(s.config, p)}}
		})
		return
	}
	if r.URL.Path == "/api/v1/reports" {
		switch r.Method {
		case "GET":
			s.authorized(w, r, "reports:read", "reports.list", func(p Principal) operationResult {
				reports, err := s.store.List(p.Workspace)
				if err != nil {
					return operationError(err)
				}
				return operationResult{status: 200, value: struct {
					Reports []Report `json:"reports"`
				}{reports}}
			})
		case "POST":
			if !slices.Contains(principal.Scopes, "reports:write") {
				s.authorized(w, r, "reports:write", "reports.create", nil)
				return
			}
			submission, errStatus, err := readSubmission(w, r)
			if err != nil {
				writeError(w, errStatus, err.Error())
				return
			}
			s.authorized(w, r, "reports:write", "reports.create", func(p Principal) operationResult {
				report, err := s.store.Create(p.Workspace, submission)
				if err != nil {
					return operationError(err)
				}
				return operationResult{status: 201, value: report, reportID: report.ID}
			})
		default:
			methodNotAllowed(w, "GET, POST")
		}
		return
	}
	prefix := "/api/v1/reports/"
	if strings.HasPrefix(r.URL.Path, prefix) && idPattern.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) {
		id := strings.TrimPrefix(r.URL.Path, prefix)
		switch r.Method {
		case "GET":
			s.authorized(w, r, "reports:read", "reports.get", func(p Principal) operationResult {
				report, err := s.store.Get(p.Workspace, id)
				if err != nil {
					return operationError(err)
				}
				return operationResult{status: 200, value: report, reportID: id}
			})
		case "DELETE":
			s.authorized(w, r, "reports:delete", "reports.delete", func(p Principal) operationResult {
				if err := s.store.Delete(p.Workspace, id); err != nil {
					return operationError(err)
				}
				return operationResult{status: 204, reportID: id}
			})
		default:
			methodNotAllowed(w, "GET, DELETE")
		}
		return
	}
	writeError(w, 404, "API route not found")
}

type operationResult struct {
	status   int
	value    any
	reportID string
}

func operationError(err error) operationResult {
	status := 500
	message := "report storage is unavailable; ask the hub operator to check the data directory"
	switch {
	case errors.Is(err, ErrNotFound):
		status = 404
		message = "report not found"
	case errors.Is(err, ErrQuota):
		status = 409
		message = ErrQuota.Error()
	}
	return operationResult{status: status, value: map[string]string{"error": message}}
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, scope, action string, operation func(Principal) operationResult) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := authenticate(r, s.config)
	p, valid := principalForKey(s.config, key, time.Now())
	if !ok || !valid {
		s.authFailure(w)
		return
	}
	event := AuditEvent{Actor: p.SubjectID, Kind: p.Kind, Workspace: p.Workspace, CredentialID: p.CredentialID, Action: action, Outcome: "intent"}
	if id := strings.TrimPrefix(r.URL.Path, "/api/v1/reports/"); idPattern.MatchString(id) {
		event.ReportID = id
	}
	if scope != "" && !slices.Contains(p.Scopes, scope) {
		event.Outcome = "denied"
		if err := s.audit.Append(event); err != nil {
			writeError(w, 503, "audit storage unavailable")
			return
		}
		writeError(w, 403, "this credential does not permit the requested action")
		return
	}
	if operation == nil {
		writeError(w, 403, "action is not allowed")
		return
	}
	if err := s.audit.Append(event); err != nil {
		writeError(w, 503, "audit storage unavailable")
		return
	}
	result := operation(p)
	event.Outcome = strconv.Itoa(result.status)
	event.ReportID = result.reportID
	if err := s.audit.Append(event); err != nil {
		writeError(w, 503, "audit completion unavailable; check the recorded intent before retrying a mutation")
		return
	}
	if result.status == 201 {
		w.Header().Set("Location", "/api/v1/reports/"+result.reportID)
	}
	if result.status == 204 {
		w.WriteHeader(204)
		return
	}
	writeJSON(w, result.status, result.value)
}

func (s *Server) authFailure(w http.ResponseWriter) {
	// Anonymous flood recording is bounded to the same burst/refill policy.
	if s.limiter.allow("anonymous-auth") {
		if err := s.audit.Append(AuditEvent{Actor: "anonymous", Kind: "anonymous", Action: "auth.failure", Outcome: "denied"}); err != nil {
			writeError(w, 503, "audit storage unavailable")
			return
		}
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="worldbisect-hub"`)
	writeError(w, 401, "a valid, unexpired workspace bearer token is required")
}

func readSubmission(w http.ResponseWriter, r *http.Request) (Submission, int, error) {
	var submission Submission
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return submission, 415, errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return submission, 413, errors.New("summary exceeds the 16 KiB request limit")
		}
		return submission, 400, errors.New("could not read JSON request")
	}
	if err := decodeStrict(b, &submission); err != nil {
		return submission, 400, errors.New("invalid summary JSON: use only documented fields and one object")
	}
	if err := submission.Validate(); err != nil {
		return submission, 422, err
	}
	return submission, 0, nil
}

type rateBucket struct {
	tokens  float64
	updated time.Time
}
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= 128 {
			var oldest string
			var when time.Time
			for candidate, bucket := range l.buckets {
				if oldest == "" || bucket.updated.Before(when) {
					oldest = candidate
					when = bucket.updated
				}
			}
			delete(l.buckets, oldest)
		}
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
func (l *rateLimiter) retain(c Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := map[string]bool{"anonymous-auth": true}
	for _, key := range c.Keys {
		keep[key.TokenSHA256] = true
	}
	for key := range l.buckets {
		if !keep[key] {
			delete(l.buckets, key)
		}
	}
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
	writeError(w, 405, "method not allowed")
}
func storeError(w http.ResponseWriter, err error) {
	result := operationError(err)
	writeJSON(w, result.status, result.value)
}

// AccessSummary contains counts only, for operator reload logs without identity
// or credential material.
func (c Config) AccessSummary() string {
	return fmt.Sprintf("%d subjects, %d memberships, %d opaque credentials", len(c.Subjects), len(c.Memberships), len(c.Keys))
}

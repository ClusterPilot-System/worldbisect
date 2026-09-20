package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const tokenA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const tokenReadA = "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr"
const tokenB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func testConfig() Config {
	c := Config{Version: 1, RetentionDays: 7, MaxReportsPerWorkspace: 500}
	for _, item := range []struct{ token, workspace, permission string }{{tokenA, "alpha", "write"}, {tokenReadA, "alpha", "read"}, {tokenB, "beta", "write"}} {
		digest := sha256.Sum256([]byte(item.token))
		c.Keys = append(c.Keys, Key{TokenSHA256: hex.EncodeToString(digest[:]), Workspace: item.workspace, Permission: item.permission})
	}
	return c
}

func fixture() Submission {
	return Submission{Repository: "example/project", CheckName: "unit tests", Status: "SUPPORTED", Finding: "Configuration changed", Tested: "Old and new settings replayed", NextStep: "Review configuration diff", Experiments: 9, RunURL: "https://github.com/example/project/actions/runs/123"}
}

func testStore(t *testing.T, c Config) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "data"), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func request(h http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTenantIsolationAndPermissions(t *testing.T) {
	s := testStore(t, testConfig())
	h := NewHandler(s, nil)
	created := request(h, "POST", "/api/v1/reports", tokenA, jsonBytes(t, fixture()))
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var report Report
	if err := json.Unmarshal(created.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ID == "" || report.CreatedAt.IsZero() {
		t.Fatal("missing server identity")
	}
	path := "/api/v1/reports/" + report.ID
	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", path, tokenReadA, 200}, {"DELETE", path, tokenReadA, 403},
		{"GET", path, tokenB, 404}, {"DELETE", path, tokenB, 404},
		{"GET", path, "", 401}, {"GET", "/api/v1/reports?token=" + tokenA, "", 401},
		{"GET", "/api/v1/reports?workspace=alpha", tokenB, 400},
		{"GET", "/healthz", "", 200},
	} {
		w := request(h, tc.method, tc.path, tc.token, nil)
		if w.Code != tc.want {
			t.Errorf("%s %s: got%d want%d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if got := request(h, "POST", "/api/v1/reports", tokenReadA, jsonBytes(t, fixture())).Code; got != 403 {
		t.Fatalf("read token created report: %d", got)
	}
	other := request(h, "GET", "/api/v1/reports", tokenB, nil)
	if other.Code != 200 || strings.Contains(other.Body.String(), report.ID) {
		t.Fatalf("tenant leak: %s", other.Body.String())
	}
	if got := request(h, "DELETE", path, tokenA, nil).Code; got != 204 {
		t.Fatal(got)
	}
	if got := request(h, "GET", path, tokenA, nil).Code; got != 404 {
		t.Fatal(got)
	}
	if created.Header().Get("Cache-Control") != "no-store" || created.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing security headers")
	}
}

func TestInputBoundary(t *testing.T) {
	h := NewHandler(testStore(t, testConfig()), nil)
	good := jsonBytes(t, fixture())
	for _, field := range []string{"id", "created_at", "workspace", "tenant", "command", "files"} {
		b := append([]byte(`{"`+field+`":"injected",`), good[1:]...)
		if w := request(h, "POST", "/api/v1/reports", tokenA, b); w.Code != 400 {
			t.Errorf("field%s: %d", field, w.Code)
		}
	}
	if w := request(h, "POST", "/api/v1/reports", tokenA, []byte(strings.Repeat(" ", MaxBodyBytes+1))); w.Code != 413 {
		t.Fatalf("body limit: %d", w.Code)
	}
	if w := request(h, "POST", "/api/v1/reports", tokenA, append(good, good...)); w.Code != 400 {
		t.Fatal(w.Code)
	}
	for _, modify := range []func(*Submission){
		func(s *Submission) { s.RunURL = "https://github.com/evil/other/actions/runs/123" },
		func(s *Submission) { s.RunURL = "https://github.com@example.com/example/project/actions/runs/123" },
		func(s *Submission) { s.RunURL = "https://github.com/example/project/actions/runs/123?token=x" },
		func(s *Submission) { s.Repository = "../../etc" }, func(s *Submission) { s.Status = "VERIFIED" },
		func(s *Submission) { s.Experiments = -1 }, func(s *Submission) { s.Finding = strings.Repeat("a", 2001) },
		func(s *Submission) { s.CommitSHA = "main" }, func(s *Submission) { s.NextStep = "\x00" },
	} {
		v := fixture()
		modify(&v)
		if w := request(h, "POST", "/api/v1/reports", tokenA, jsonBytes(t, v)); w.Code != 422 {
			t.Errorf("invalid input accepted: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestPersistenceQuotaRetentionAndLock(t *testing.T) {
	c := testConfig()
	c.MaxReportsPerWorkspace = 1
	dir := filepath.Join(t.TempDir(), "data")
	s, err := OpenStore(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if second, err := OpenStore(dir, c); err == nil {
		second.Close()
		t.Fatal("second writer accepted")
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	r, err := s.Create("alpha", fixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("alpha", fixture()); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota: %v", err)
	}
	if _, err := s.Create("beta", fixture()); err != nil {
		t.Fatalf("cross-tenant quota: %v", err)
	}
	s.Close()
	s, err = OpenStore(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Get("alpha", r.ID); err != nil || got.Finding != r.Finding {
		t.Fatalf("restart: %v", err)
	}
	digest := sha256.Sum256([]byte("alpha"))
	path := filepath.Join(dir, hex.EncodeToString(digest[:]), r.ID+".json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode: %v", err)
	}
	s.now = func() time.Time { return now.Add(7 * 24 * time.Hour) }
	if _, err := s.Get("alpha", r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired record: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file still exists: %v", err)
	}
	if _, err := s.Create("alpha", fixture()); err != nil {
		t.Fatalf("quota not freed: %v", err)
	}
}

func TestRetainsNoExpiredOrphanWorkspace(t *testing.T) {
	c := testConfig()
	dir := filepath.Join(t.TempDir(), "data")
	s, err := OpenStore(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) }
	if _, err := s.Create("beta", fixture()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	c.Keys = c.Keys[:2]
	s, err = OpenStore(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	digest := sha256.Sum256([]byte("beta"))
	if _, err := os.Stat(filepath.Join(dir, hex.EncodeToString(digest[:]))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan retention: %v", err)
	}
}

func TestStoreRejectsSymlinkAndOversizedPersistedData(t *testing.T) {
	for _, kind := range []string{"symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t, testConfig())
			r, err := s.Create("alpha", fixture())
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte("alpha"))
			path := filepath.Join(s.root, hex.EncodeToString(digest[:]), r.ID+".json")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink("/etc/passwd", path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 65537), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.List("alpha"); err == nil {
				t.Fatal("unsafe persisted data accepted")
			}
		})
	}
}

func TestInitializationStoresHashesOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.json")
	tokens, err := Initialize(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if len(token) < 64 || bytes.Contains(b, []byte(token)) {
			t.Fatal("raw token persisted")
		}
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(path); err == nil {
		t.Fatal("existing config overwritten")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("public config accepted")
	}
}

func TestListIsBounded(t *testing.T) {
	s := testStore(t, testConfig())
	for i := 0; i < 101; i++ {
		if _, err := s.Create("alpha", fixture()); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := s.List("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 100 {
		t.Fatal(len(reports))
	}
}

type blockingBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}
func (*blockingBody) Close() error { return nil }

func TestSlowTenantDoesNotOccupyAllRequestSlots(t *testing.T) {
	h := NewHandler(testStore(t, testConfig()), nil)
	release := make(chan struct{})
	defer close(release)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		body := &blockingBody{started: make(chan struct{}), release: release}
		r := httptest.NewRequest("POST", "/api/v1/reports", nil)
		r.Body = body
		r.Header.Set("Authorization", "Bearer "+tokenA)
		r.Header.Set("Content-Type", "application/json")
		wg.Add(1)
		go func() { defer wg.Done(); h.ServeHTTP(httptest.NewRecorder(), r) }()
		<-body.started
	}
	if w := request(h, "GET", "/api/v1/session", tokenA, nil); w.Code != 429 {
		t.Errorf("excess tenant request: %d", w.Code)
	}
	if w := request(h, "GET", "/api/v1/session", tokenB, nil); w.Code != 200 {
		t.Errorf("other tenant blocked: %d", w.Code)
	}
	// The deferred close releases requests before cleanup closes the store.
	t.Cleanup(wg.Wait)
}

func TestAuthenticatedRateLimit(t *testing.T) {
	h := NewHandler(testStore(t, testConfig()), nil)
	for i := 0; i < 30; i++ {
		if w := request(h, "GET", "/api/v1/session", tokenA, nil); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if w := request(h, "GET", "/api/v1/session", tokenA, nil); w.Code != 429 {
		t.Fatalf("rate limit: %d", w.Code)
	}
	if w := request(h, "GET", "/api/v1/session", tokenB, nil); w.Code != 200 {
		t.Fatalf("other tenant rate: %d", w.Code)
	}
}

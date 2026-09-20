package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func identityConfig() Config {
	c := testConfig()
	c.Version = 2
	c.Subjects = []Subject{{ID: "alice", Kind: "user"}, {ID: "beta-ci", Kind: "service"}}
	c.Memberships = []Membership{{SubjectID: "alice", Workspace: "alpha", Role: "editor"}, {SubjectID: "beta-ci", Workspace: "beta", Role: "editor"}}
	for i := range c.Keys {
		c.Keys[i].ID = fmt.Sprintf("key-%d", i)
		c.Keys[i].SubjectID = "alice"
		if i == 2 {
			c.Keys[i].SubjectID = "beta-ci"
		}
	}
	return c
}

func TestNamedIdentitiesScopesExpiryAndRevocation(t *testing.T) {
	c := identityConfig()
	s := NewServer(testStore(t, c), nil, nil)
	w := request(s, "GET", "/api/v1/session", tokenA, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var session map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session["subject_id"] != "alice" || session["kind"] != "user" || session["role"] != "editor" {
		t.Fatal(session)
	}
	// A service publisher may create, but cannot read or delete reports.
	c.Memberships[1].Role = "publisher"
	c.Keys[2].Scopes = []string{"reports:write"}
	if err := s.Reload(c); err != nil {
		t.Fatal(err)
	}
	created := request(s, "POST", "/api/v1/reports", tokenB, jsonBytes(t, fixture()))
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var report Report
	json.Unmarshal(created.Body.Bytes(), &report)
	for _, pair := range []struct{ method, path string }{{"GET", "/api/v1/reports"}, {"DELETE", "/api/v1/reports/" + report.ID}} {
		if got := request(s, pair.method, pair.path, tokenB, nil).Code; got != 403 {
			t.Fatal(got)
		}
	}
	past := time.Now().Add(-time.Minute)
	c.Keys[0].ExpiresAt = &past
	if err := s.Reload(c); err != nil {
		t.Fatal(err)
	}
	if got := request(s, "GET", "/api/v1/session", tokenA, nil).Code; got != 401 {
		t.Fatalf("expired token: %d", got)
	}
	c.Subjects[0].Disabled = true
	if err := s.Reload(c); err != nil {
		t.Fatal(err)
	}
	if got := request(s, "GET", "/api/v1/session", tokenReadA, nil).Code; got != 401 {
		t.Fatalf("disabled subject: %d", got)
	}
	if got := request(s, "GET", "/api/v1/session", tokenB, nil).Code; got != 200 {
		t.Fatalf("unrelated subject: %d", got)
	}
}

type heldJSON struct {
	started, release chan struct{}
	once             sync.Once
	reader           io.Reader
}

func (b *heldJSON) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.reader.Read(p)
}
func (*heldJSON) Close() error { return nil }

func TestReloadRevokesInFlightBodyBeforeCommit(t *testing.T) {
	c := identityConfig()
	store := testStore(t, c)
	s := NewServer(store, nil, nil)
	body := &heldJSON{started: make(chan struct{}), release: make(chan struct{}), reader: bytes.NewReader(jsonBytes(t, fixture()))}
	r := httptest.NewRequest("POST", "/api/v1/reports", nil)
	r.Body = body
	r.Header.Set("Authorization", "Bearer "+tokenA)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.ServeHTTP(w, r); close(done) }()
	<-body.started
	c.Keys = append([]Key{}, c.Keys[1:]...)
	reloaded := make(chan error, 1)
	go func() { reloaded <- s.Reload(c) }()
	select {
	case err := <-reloaded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(body.release)
		t.Fatal("reload waited for untrusted request body")
	}
	close(body.release)
	<-done
	if w.Code != 401 {
		t.Fatalf("revoked request committed: %d %s", w.Code, w.Body.String())
	}
	reports, err := store.List("alpha")
	if err != nil || len(reports) != 0 {
		t.Fatalf("revoked upload persisted: %v %#v", err, reports)
	}
}

func TestInvalidReloadPreservesAccessAndConcurrentReads(t *testing.T) {
	c := identityConfig()
	s := NewServer(testStore(t, c), nil, nil)
	invalid := c
	invalid.Version = 99
	if err := s.Reload(invalid); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if request(s, "GET", "/api/v1/session", tokenA, nil).Code != 200 {
		t.Fatal("good access lost")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				request(s, "GET", "/api/v1/session", tokenA, nil)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		if err := s.Reload(c); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestCredentialProvisioningIsPrivateAndExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.json")
	if _, err := Initialize(path); err != nil {
		t.Fatal(err)
	}
	key, token, err := ProvisionCredential(path, CredentialOptions{SubjectID: "jo", Kind: "user", Workspace: "alpha", Role: "viewer", ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if key.ExpiresAt == nil || key.SubjectID != "jo" || key.ID == "" || len(token) < 64 {
		t.Fatal("invalid provisioned key")
	}
	content, _ := os.ReadFile(path)
	if bytes.Contains(content, []byte(token)) {
		t.Fatal("raw token persisted")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	store := testStore(t, c)
	s := NewServer(store, nil, nil)
	if request(s, "GET", "/api/v1/session", token, nil).Code != 200 {
		t.Fatal("provisioned token unusable")
	}
	if request(s, "POST", "/api/v1/reports", token, jsonBytes(t, fixture())).Code != 403 {
		t.Fatal("viewer escalation")
	}
	if _, _, err := ProvisionCredential(path, CredentialOptions{SubjectID: "jo", Kind: "user", Workspace: "alpha", Role: "editor"}); err == nil {
		t.Fatal("implicit membership escalation")
	}
}

func TestAccessConfigurationRejectsEscalationAndMissingMembership(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Keys[0].SubjectID = "unknown" },
		func(c *Config) { c.Memberships[0].Role = "viewer" },
		func(c *Config) { c.Keys[0].Scopes = []string{"reports:admin"} },
		func(c *Config) { c.Keys[1].ID = c.Keys[0].ID },
		func(c *Config) { c.Subjects[0].Kind = "root" },
	} {
		c := identityConfig()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Fatal("invalid identity configuration accepted")
		}
	}
}

func TestAuditContainsAttributionWithoutContentAndFailsClosed(t *testing.T) {
	c := identityConfig()
	store := testStore(t, c)
	dir := filepath.Join(t.TempDir(), "audit")
	a, err := OpenAudit(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	s := NewServer(store, nil, a)
	submission := fixture()
	submission.Finding = "private-sensitive-finding-never-audit"
	if w := request(s, "POST", "/api/v1/reports", tokenA, jsonBytes(t, submission)); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(s, "GET", "/api/v1/reports", tokenReadA, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/api/v1/reports", "bad-token", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var combined []byte
	for _, file := range files {
		b, _ := os.ReadFile(file)
		combined = append(combined, b...)
	}
	for _, secret := range []string{tokenA, tokenReadA, submission.Finding, submission.Repository, submission.RunURL} {
		if bytes.Contains(combined, []byte(secret)) {
			t.Fatal("sensitive data entered audit")
		}
	}
	for _, want := range []string{`"actor":"alice"`, `"credential_id":"key-0"`, `"action":"reports.create"`, `"action":"reports.list"`, `"action":"auth.failure"`} {
		if !bytes.Contains(combined, []byte(want)) {
			t.Fatalf("missing audit attribution %s", want)
		}
	}
	a.mu.Lock()
	a.fault = errors.New("disk unavailable")
	a.mu.Unlock()
	if w := request(s, "POST", "/api/v1/reports", tokenA, jsonBytes(t, fixture())); w.Code != 503 {
		t.Fatal(w.Code)
	}
	reports, err := store.List("alpha")
	if err != nil || len(reports) != 1 {
		t.Fatalf("write continued after audit failure: %v %d", err, len(reports))
	}
	if !strings.Contains(string(combined), `"outcome":"201"`) {
		t.Fatal("missing completion")
	}
}

var _ http.Handler = (*Server)(nil)

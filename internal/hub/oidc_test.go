package hub

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

type ciRoundTrip func(*http.Request) (*http.Response, error)

func (f ciRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func ciFixture(t *testing.T) (*rsa.PrivateKey, ciClaims, CIPublisher) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	p := CIPublisher{SubjectID: "ci-service", Workspace: "alpha", Audience: "https://hub.example/alpha", Repository: "example/project", RepositoryID: "123", RepositoryOwnerID: "456", WorkflowRef: "example/project/.github/workflows/ci.yml@refs/heads/main", Ref: "refs/heads/main", Subject: "repo:example@456/project@123:ref:refs/heads/main"}
	c := ciClaims{Issuer: githubIssuer, Subject: p.Subject, Audience: p.Audience, ID: "fixture-token-id", IssuedAt: now - 2, NotBefore: now - 2, Expires: now + 298, Repository: p.Repository, RepositoryID: p.RepositoryID, RepositoryOwnerID: p.RepositoryOwnerID, WorkflowRef: p.WorkflowRef, Ref: p.Ref, RefType: "branch", EventName: "push", RunnerEnvironment: "github-hosted", SHA: strings.Repeat("a", 40), RunID: "123", RunAttempt: "1"}
	return key, c, p
}

func ciConfig(p CIPublisher) Config {
	return Config{Version: 2, RetentionDays: 7, MaxReportsPerWorkspace: 50, AuditRetentionDays: 7, Subjects: []Subject{{ID: p.SubjectID, Kind: "service"}}, Memberships: []Membership{{SubjectID: p.SubjectID, Workspace: p.Workspace, Role: "publisher"}}, CIPublishers: []CIPublisher{p}}
}

func signedCI(t *testing.T, key *rsa.PrivateKey, header, claims []byte) string {
	t.Helper()
	message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(sig)
}

var ciHeader = []byte(`{"typ":"JWT","alg":"RS256","kid":"fixture"}`)

func cachedVerifier(root string, key *rsa.PrivateKey) *ciVerifier {
	v := newCIVerifier(root)
	v.keys = map[string]*rsa.PublicKey{"fixture": &key.PublicKey}
	v.fetched = time.Now()
	return v
}

func TestCIVerifiesSignatureAndStrictClaims(t *testing.T) {
	key, claims, p := ciFixture(t)
	v := cachedVerifier(t.TempDir(), key)
	valid := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	got, err := v.verify(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := matchingPublisher(ciConfig(p), got); !ok {
		t.Fatal("valid binding did not match")
	}
	for name, mutate := range map[string]func(*ciClaims){
		"issuer":         func(c *ciClaims) { c.Issuer = "https://attacker.invalid" },
		"expired":        func(c *ciClaims) { c.Expires = time.Now().Unix() - 1 },
		"future":         func(c *ciClaims) { c.NotBefore = time.Now().Unix() + 100 },
		"long-life":      func(c *ciClaims) { c.Expires = c.IssuedAt + 601 },
		"pr":             func(c *ciClaims) { c.EventName = "pull_request" },
		"pr-target":      func(c *ciClaims) { c.EventName = "pull_request_target" },
		"untrusted-head": func(c *ciClaims) { c.HeadRef = "attacker" },
		"self-hosted":    func(c *ciClaims) { c.RunnerEnvironment = "self-hosted" },
		"reusable":       func(c *ciClaims) { c.JobWorkflowRef = "other/repo/.github/workflows/test.yml@main" },
		"run-id":         func(c *ciClaims) { c.RunID = "../123" },
		"missing-jti":    func(c *ciClaims) { c.ID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := claims
			mutate(&changed)
			if _, err := v.verify(context.Background(), signedCI(t, key, ciHeader, jsonBytes(t, changed))); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*ciClaims){
		"audience":  func(c *ciClaims) { c.Audience = "https://other.example" },
		"subject":   func(c *ciClaims) { c.Subject += "-changed" },
		"repo-name": func(c *ciClaims) { c.Repository = "other/project" },
		"repo-id":   func(c *ciClaims) { c.RepositoryID = "999" },
		"owner-id":  func(c *ciClaims) { c.RepositoryOwnerID = "999" },
		"workflow":  func(c *ciClaims) { c.WorkflowRef = "example/project/.github/workflows/other.yml@refs/heads/main" },
		"branch":    func(c *ciClaims) { c.Ref = "refs/heads/attacker" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := claims
			mutate(&changed)
			if _, ok := matchingPublisher(ciConfig(p), changed); ok {
				t.Fatal("wrong identity matched")
			}
		})
	}
	for _, header := range []string{
		`{"typ":"JWT","alg":"none","kid":"fixture"}`,
		`{"typ":"JWT","alg":"HS256","kid":"fixture"}`,
		`{"typ":"JWT","alg":"RS256","kid":"fixture","jku":"https://attacker.invalid"}`,
		`{"typ":"JWT","alg":"RS256","kid":"fixture","crit":["b64"]}`,
		`{"typ":"JWT","alg":"none","alg":"RS256","kid":"fixture"}`,
	} {
		if _, err := v.verify(context.Background(), signedCI(t, key, []byte(header), jsonBytes(t, claims))); err == nil {
			t.Fatal("unsafe header accepted")
		}
	}
	duplicate := append([]byte(`{"aud":"https://attacker.invalid",`), jsonBytes(t, claims)[1:]...)
	if _, err := v.verify(context.Background(), signedCI(t, key, ciHeader, duplicate)); err == nil {
		t.Fatal("duplicate audience accepted")
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.verify(context.Background(), signedCI(t, other, ciHeader, jsonBytes(t, claims))); err == nil {
		t.Fatal("untrusted signature accepted")
	}
}

func TestCIKeyFetchIsFixedBoundedAndCached(t *testing.T) {
	key, claims, _ := ciFixture(t)
	v := newCIVerifier(t.TempDir())
	requests := 0
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"fixture","alg":"RS256","use":"sig","n":%q,"e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))
	v.client.Transport = ciRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != githubJWKS || r.Header.Get("Authorization") != "" {
			t.Fatal("unsafe key fetch")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(jwks)), Header: make(http.Header)}, nil
	})
	for i := 0; i < 3; i++ {
		if _, err := v.verify(context.Background(), signedCI(t, key, ciHeader, jsonBytes(t, claims))); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 {
		t.Fatalf("got %d JWKS fetches", requests)
	}
	if _, err := v.signingKey(context.Background(), "attacker"); err == nil || requests != 1 {
		t.Fatal("unknown kid escaped refresh limit")
	}
	v.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	v.client.Transport = ciRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", 65537))), Header: make(http.Header)}, nil
	})
	if _, err := v.signingKey(context.Background(), "fixture"); err == nil {
		t.Fatal("oversized key document or stale key accepted")
	}
	if err := v.client.CheckRedirect(httptest.NewRequest("GET", "https://attacker.invalid", nil), nil); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestCIReplaySurvivesRestartAndConcurrentUse(t *testing.T) {
	key, claims, p := ciFixture(t)
	root := t.TempDir()
	v := cachedVerifier(root, key)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v.consume(claims, p) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("successful replay reservations: %d", success)
	}
	v = newCIVerifier(root)
	if err := v.consume(claims, p); err == nil {
		t.Fatal("replay survived restart")
	}
	info, err := os.Stat(filepath.Join(root, replayFilename))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("ledger not private")
	}
	for i := 1; i < 32; i++ {
		claims.ID = fmt.Sprintf("next-token-%03d", i)
		if err := v.consume(claims, p); err != nil {
			t.Fatal(err)
		}
	}
	claims.ID = "token-over-quota"
	if err := v.consume(claims, p); err == nil {
		t.Fatal("publisher replay quota exceeded")
	}
	v.now = func() time.Time { return time.Unix(claims.Expires+1, 0) }
	claims.Expires += 300
	if err := v.consume(claims, p); err != nil {
		t.Fatalf("expired ledger entries were not pruned: %v", err)
	}
}

func TestCIEndpointPersistsOriginAndRejectsSpoofing(t *testing.T) {
	key, claims, p := ciFixture(t)
	config := ciConfig(p)
	store := testStore(t, config)
	server := NewServer(store, nil, nil)
	server.ci = cachedVerifier(store.root, key)
	token := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	submission := fixture()
	submission.CommitSHA = claims.SHA
	for _, mutate := range []func(*Submission){func(s *Submission) { s.CommitSHA = strings.Repeat("b", 40) }, func(s *Submission) { s.RunURL = "https://github.com/example/project/actions/runs/124" }, func(s *Submission) {
		s.Repository = "other/project"
		s.RunURL = "https://github.com/other/project/actions/runs/123"
	}} {
		bad := submission
		mutate(&bad)
		response := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, bad))
		if response.Code != 403 {
			t.Fatalf("unbound summary status %d", response.Code)
		}
	}
	spoof := append([]byte(`{"publisher":{"provider":"github-actions-oidc"},`), jsonBytes(t, submission)[1:]...)
	if got := request(server, "POST", "/api/v1/ci/reports", token, spoof).Code; got != 400 {
		t.Fatalf("spoofed metadata status %d", got)
	}
	created := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, submission))
	if created.Code != 201 {
		t.Fatalf("create %d: %s", created.Code, created.Body.String())
	}
	var report Report
	if err := json.Unmarshal(created.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(p.Workspace, report.ID)
	if err != nil || stored.Publisher == nil || stored.Publisher.RepositoryID != p.RepositoryID || stored.Status != "SUPPORTED" {
		t.Fatalf("origin missing or confidence modified: %+v %v", stored, err)
	}
	if got := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, submission)).Code; got != 409 {
		t.Fatalf("replay status %d", got)
	}
	if err := store.PurgeExpired(); err != nil {
		t.Fatalf("replay ledger broke storage: %v", err)
	}
	if _, err := store.Get("beta", report.ID); err != ErrNotFound {
		t.Fatal("cross-workspace report visible")
	}
}

func TestCIDisabledMembershipAndSlowBodyRevocation(t *testing.T) {
	key, claims, p := ciFixture(t)
	config := ciConfig(p)
	store := testStore(t, config)
	server := NewServer(store, nil, nil)
	server.ci = cachedVerifier(store.root, key)
	token := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	submission := fixture()
	submission.CommitSHA = claims.SHA
	// A valid token cannot win a race with a config reload during its body read.
	started := make(chan struct{})
	proceed := make(chan struct{})
	done := make(chan int)
	body := &ciBlockingReader{Reader: bytes.NewReader(jsonBytes(t, submission)), started: started, proceed: proceed}
	r := httptest.NewRequest("POST", "/api/v1/ci/reports", body)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	go func() { w := httptest.NewRecorder(); server.ServeHTTP(w, r); done <- w.Code }()
	<-started
	// Revocation keeps valid config by retaining a disabled service and removing
	// its trust rule. A second unrelated CI rule keeps the endpoint configured.
	other := p
	other.SubjectID = "other-service"
	other.Workspace = "beta"
	other.Audience = "https://hub.example/other"
	config.Subjects = append(config.Subjects, Subject{ID: other.SubjectID, Kind: "service"})
	config.Memberships = append(config.Memberships, Membership{SubjectID: other.SubjectID, Workspace: "beta", Role: "publisher"})
	config.CIPublishers = []CIPublisher{other}
	if err := server.Reload(config); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if got := <-done; got != 403 {
		t.Fatalf("revoked publisher committed: %d", got)
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Memberships[0].Role = "viewer" }, func(c *Config) { c.CIPublishers[0].RepositoryID = "*" }, func(c *Config) { c.CIPublishers = append(c.CIPublishers, c.CIPublishers[0]) }} {
		c := ciConfig(p)
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("unsafe publisher config accepted")
		}
	}
	disabled := ciConfig(p)
	disabled.Subjects[0].Disabled = true
	if err := disabled.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := matchingPublisher(disabled, claims); ok {
		t.Fatal("disabled subject matched")
	}
}

type ciBlockingReader struct {
	*bytes.Reader
	started, proceed chan struct{}
	once             sync.Once
}

func (r *ciBlockingReader) Read(b []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.proceed })
	return r.Reader.Read(b)
}

func TestCIAnonymousTrafficCannotExhaustPublisherBudget(t *testing.T) {
	key, claims, publisher := ciFixture(t)
	store := testStore(t, ciConfig(publisher))
	server := NewServer(store, nil, nil)
	server.ci = cachedVerifier(store.root, key)
	for i := 0; i < 64; i++ {
		if got := request(server, "POST", "/api/v1/ci/reports", "", nil).Code; got != 401 {
			t.Fatalf("anonymous request consumed publisher budget: %d", got)
		}
	}
	submission := fixture()
	submission.CommitSHA = claims.SHA
	token := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	if got := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, submission)).Code; got != 201 {
		t.Fatalf("valid publisher starved by anonymous requests: %d", got)
	}
}

func TestCISlowWorkspaceDoesNotBlockAnotherWorkspace(t *testing.T) {
	key, claims, p := ciFixture(t)
	config := ciConfig(p)
	other := p
	other.SubjectID, other.Workspace, other.Audience = "other-service", "beta", "https://hub.example/beta"
	config.Subjects = append(config.Subjects, Subject{ID: other.SubjectID, Kind: "service"})
	config.Memberships = append(config.Memberships, Membership{SubjectID: other.SubjectID, Workspace: other.Workspace, Role: "publisher"})
	config.CIPublishers = append(config.CIPublishers, other)
	store := testStore(t, config)
	server := NewServer(store, nil, nil)
	server.ci = cachedVerifier(store.root, key)
	submission := fixture()
	submission.CommitSHA = claims.SHA
	token := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	proceed := make(chan struct{})
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		started := make(chan struct{})
		r := httptest.NewRequest("POST", "/api/v1/ci/reports", &ciBlockingReader{Reader: bytes.NewReader(jsonBytes(t, submission)), started: started, proceed: proceed})
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		go func() { w := httptest.NewRecorder(); server.ServeHTTP(w, r); done <- w.Code }()
		<-started
	}
	defer func() { close(proceed); <-done; <-done }()
	if got := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, submission)).Code; got != 429 {
		t.Fatalf("workspace concurrency limit not enforced: %d", got)
	}
	claims.Audience, claims.ID = other.Audience, "other-token-identity"
	otherToken := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	if got := request(server, "POST", "/api/v1/ci/reports", otherToken, jsonBytes(t, submission)).Code; got != 201 {
		t.Fatalf("slow publisher blocked another workspace: %d", got)
	}
}

func TestCIRestoreQuarantineIncludingPendingBody(t *testing.T) {
	key, claims, p := ciFixture(t)
	config := ciConfig(p)
	store := testStore(t, config)
	server := NewServer(store, nil, nil)
	server.ci = cachedVerifier(store.root, key)
	submission := fixture()
	submission.CommitSHA = claims.SHA
	token := signedCI(t, key, ciHeader, jsonBytes(t, claims))
	started, proceed := make(chan struct{}), make(chan struct{})
	done := make(chan int, 1)
	r := httptest.NewRequest("POST", "/api/v1/ci/reports", &ciBlockingReader{Reader: bytes.NewReader(jsonBytes(t, submission)), started: started, proceed: proceed})
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	go func() { w := httptest.NewRecorder(); server.ServeHTTP(w, r); done <- w.Code }()
	<-started
	until := time.Now().Add(11 * time.Minute)
	config.CIQuarantineUntil = &until
	if err := server.Reload(config); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if got := <-done; got != 503 {
		t.Fatalf("pending body bypassed quarantine: %d", got)
	}
	response := request(server, "POST", "/api/v1/ci/reports", token, jsonBytes(t, submission))
	if response.Code != 503 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("quarantine response: %d", response.Code)
	}
	if reports, err := store.List(p.Workspace); err != nil || len(reports) != 0 {
		t.Fatal("report persisted during quarantine")
	}
	response = httptest.NewRecorder()
	if ciQuarantined(response, &until, until.Add(time.Second)) {
		t.Fatal("quarantine did not expire")
	}
}

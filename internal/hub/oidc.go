package hub

// This deliberately supports one issuer, algorithm and narrowly scoped workflow
// type. It is not a general OIDC login implementation or an evidence attestation.
import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const githubIssuer = "https://token.actions.githubusercontent.com"
const githubJWKS = githubIssuer + "/.well-known/jwks"
const replayFilename = ".ci-replays.json"

var decimalID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var branchRef = regexp.MustCompile(`^refs/heads/[A-Za-z0-9_./-]{1,200}$`)
var workflowFile = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.ya?ml$`)

// CIPublisher is an exact, operator-provisioned trust rule. No wildcard, issuer
// URL or remote key URL is accepted from configuration or a JWT.
type CIPublisher struct {
	SubjectID         string `json:"subject_id"`
	Workspace         string `json:"workspace"`
	Audience          string `json:"audience"`
	Repository        string `json:"repository"`
	RepositoryID      string `json:"repository_id"`
	RepositoryOwnerID string `json:"repository_owner_id"`
	WorkflowRef       string `json:"workflow_ref"`
	Ref               string `json:"ref"`
	Subject           string `json:"subject"`
}

func ValidateCIPublishers(c Config) error {
	if len(c.CIPublishers) > 50 || (len(c.CIPublishers) > 0 && c.Version != 2) {
		return errors.New("CI publishing requires configuration v2 and at most 50 trust rules")
	}
	seen := map[string]bool{}
	for _, p := range c.CIPublishers {
		if !workspacePattern.MatchString(p.SubjectID) || !workspacePattern.MatchString(p.Workspace) || !repositoryPattern.MatchString(p.Repository) || !decimalID.MatchString(p.RepositoryID) || !decimalID.MatchString(p.RepositoryOwnerID) || !branchRef.MatchString(p.Ref) || strings.Contains(p.Ref, "..") || strings.HasSuffix(p.Ref, "/") {
			return errors.New("CI publisher requires exact workspace, service subject, repository IDs and branch ref")
		}
		u, err := url.Parse(p.Audience)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(p.Audience) > 256 {
			return errors.New("CI audience must be a dedicated HTTPS identifier, at most 256 bytes")
		}
		file := strings.TrimSuffix(strings.TrimPrefix(p.WorkflowRef, p.Repository+"/.github/workflows/"), "@"+p.Ref)
		if p.WorkflowRef != p.Repository+"/.github/workflows/"+file+"@"+p.Ref || !workflowFile.MatchString(file) {
			return errors.New("CI workflow_ref must name one workflow file in the configured repository and branch")
		}
		if len(p.Subject) < 8 || len(p.Subject) > 512 || strings.ContainsAny(p.Subject, " \t\r\n*") {
			return errors.New("CI subject must be an exact OIDC subject without whitespace or wildcards")
		}
		if !ciMembership(c, p, false) {
			return errors.New("CI publisher requires a service subject and publisher or editor membership")
		}
		// One token must map to exactly one workspace and principal.
		binding := p.Audience + "\n" + p.Subject + "\n" + p.RepositoryID + "\n" + p.WorkflowRef
		if seen[binding] {
			return errors.New("duplicate CI publisher trust binding")
		}
		seen[binding] = true
	}
	return nil
}

func ciMembership(c Config, p CIPublisher, requireEnabled bool) bool {
	active := false
	for _, subject := range c.Subjects {
		if subject.ID == p.SubjectID && subject.Kind == "service" && (!requireEnabled || !subject.Disabled) {
			active = true
		}
	}
	if !active {
		return false
	}
	for _, member := range c.Memberships {
		if member.SubjectID == p.SubjectID && member.Workspace == p.Workspace && (member.Role == "publisher" || member.Role == "editor") {
			return true
		}
	}
	return false
}

type ciClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Audience          string `json:"aud"`
	ID                string `json:"jti"`
	IssuedAt          int64  `json:"iat"`
	NotBefore         int64  `json:"nbf"`
	Expires           int64  `json:"exp"`
	Repository        string `json:"repository"`
	RepositoryID      string `json:"repository_id"`
	RepositoryOwnerID string `json:"repository_owner_id"`
	WorkflowRef       string `json:"workflow_ref"`
	Ref               string `json:"ref"`
	RefType           string `json:"ref_type"`
	EventName         string `json:"event_name"`
	RunnerEnvironment string `json:"runner_environment"`
	SHA               string `json:"sha"`
	RunID             string `json:"run_id"`
	RunAttempt        string `json:"run_attempt"`
	HeadRef           string `json:"head_ref"`
	BaseRef           string `json:"base_ref"`
	JobWorkflowRef    string `json:"job_workflow_ref"`
}

type ciVerifier struct {
	mu          sync.Mutex
	client      *http.Client
	now         func() time.Time
	keys        map[string]*rsa.PublicKey
	fetched     time.Time
	lastAttempt time.Time
	root        string
	slots       chan struct{}
	limiter     *rateLimiter
}

func newCIVerifier(root string) *ciVerifier {
	return &ciVerifier{
		root: root, now: time.Now, slots: make(chan struct{}, 2),
		limiter: &rateLimiter{buckets: make(map[string]rateBucket)},
		client: &http.Client{Timeout: 5 * time.Second,
			Transport:     &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OIDC key redirects are forbidden") }},
	}
}

// objectJSON rejects duplicate top-level names. JWT claim extensions are allowed
// but cannot shadow the fields used for authorization. Nested data is never used.
func objectJSON(b []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return errors.New("duplicate or invalid JSON field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return json.Unmarshal(b, target)
}

func (v *ciVerifier) verify(ctx context.Context, token string) (ciClaims, error) {
	var claims ciClaims
	if len(token) > 12*1024 || strings.ContainsAny(token, " \t\r\n,") {
		return claims, errors.New("invalid token size or encoding")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("expected signed JWT")
	}
	headerBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(headerBytes) > 2048 {
		return claims, errors.New("invalid JWT header")
	}
	var header map[string]json.RawMessage
	if objectJSON(headerBytes, &header) != nil {
		return claims, errors.New("invalid JWT header object")
	}
	for name := range header {
		if name != "alg" && name != "typ" && name != "kid" && name != "x5t" && name != "x5t#S256" {
			return claims, errors.New("unsupported JWT header")
		}
	}
	var alg, typ, kid string
	if json.Unmarshal(header["alg"], &alg) != nil || json.Unmarshal(header["typ"], &typ) != nil || json.Unmarshal(header["kid"], &kid) != nil || alg != "RS256" || typ != "JWT" || len(kid) < 1 || len(kid) > 128 {
		return claims, errors.New("unsupported token algorithm, type or key")
	}
	claimBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || objectJSON(claimBytes, &claims) != nil {
		return claims, errors.New("invalid JWT claims")
	}
	now := v.now().Unix()
	if claims.Issuer != githubIssuer || claims.IssuedAt <= 0 || claims.NotBefore <= 0 || claims.Expires <= now || claims.IssuedAt > now+30 || claims.NotBefore > now+30 || claims.NotBefore > claims.IssuedAt+30 || claims.Expires <= claims.IssuedAt || claims.Expires-claims.IssuedAt > 600 || claims.IssuedAt < now-600 {
		return claims, errors.New("invalid issuer or token lifetime")
	}
	if len(claims.ID) < 8 || len(claims.ID) > 128 || strings.ContainsAny(claims.ID, " \t\r\n") || claims.EventName != "push" || claims.RefType != "branch" || claims.RunnerEnvironment != "github-hosted" || claims.HeadRef != "" || claims.BaseRef != "" || claims.JobWorkflowRef != "" || !commitPattern.MatchString(claims.SHA) || !decimalID.MatchString(claims.RunID) || !decimalID.MatchString(claims.RunAttempt) {
		return claims, errors.New("unsupported workflow identity")
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return claims, errors.New("invalid JWT signature encoding")
	}
	key, err := v.signingKey(ctx, kid)
	if err != nil {
		return claims, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return claims, errors.New("invalid JWT signature")
	}
	return claims, nil
}

func (v *ciVerifier) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	if key := v.keys[kid]; key != nil && now.Sub(v.fetched) < 10*time.Minute {
		return key, nil
	}
	if !v.lastAttempt.IsZero() && now.Sub(v.lastAttempt) < time.Minute {
		return nil, errors.New("OIDC signing key unavailable; retry later")
	}
	v.lastAttempt = now
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, githubJWKS, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.client.Do(r)
	if err != nil {
		return nil, errors.New("OIDC key service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("OIDC key service rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("OIDC key response exceeds limits")
	}
	keys, err := parseSigningKeys(raw)
	if err != nil {
		return nil, err
	}
	v.keys, v.fetched = keys, now
	if key := keys[kid]; key != nil {
		return key, nil
	}
	return nil, errors.New("unknown OIDC signing key")
}

func parseSigningKeys(raw []byte) (map[string]*rsa.PublicKey, error) {
	var document struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if objectJSON(raw, &document) != nil || len(document.Keys) == 0 || len(document.Keys) > 10 {
		return nil, errors.New("invalid OIDC key set")
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, encoded := range document.Keys {
		var key struct{ Kty, Kid, Alg, Use, N, E string }
		if objectJSON(encoded, &key) != nil || key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" || len(key.Kid) == 0 || len(key.Kid) > 128 || keys[key.Kid] != nil {
			return nil, errors.New("invalid or duplicate OIDC signing key")
		}
		n, err := base64.RawURLEncoding.Strict().DecodeString(key.N)
		if err != nil || len(n) < 256 || len(n) > 512 {
			return nil, errors.New("unsupported RSA modulus")
		}
		e, err := base64.RawURLEncoding.Strict().DecodeString(key.E)
		if err != nil || !bytes.Equal(e, []byte{1, 0, 1}) {
			return nil, errors.New("unsupported RSA exponent")
		}
		modulus := new(big.Int).SetBytes(n)
		if modulus.BitLen() < 2048 || modulus.BitLen() > 4096 || modulus.Bit(0) != 1 {
			return nil, errors.New("unsupported RSA modulus")
		}
		keys[key.Kid] = &rsa.PublicKey{N: modulus, E: 65537}
	}
	return keys, nil
}

func matchingPublisher(config Config, c ciClaims) (CIPublisher, bool) {
	for _, p := range config.CIPublishers {
		if p.Audience == c.Audience && p.Subject == c.Subject && p.Repository == c.Repository && p.RepositoryID == c.RepositoryID && p.RepositoryOwnerID == c.RepositoryOwnerID && p.WorkflowRef == c.WorkflowRef && p.Ref == c.Ref && ciMembership(config, p, true) {
			return p, true
		}
	}
	return CIPublisher{}, false
}

type replayRecord struct {
	Digest    string `json:"digest"`
	Publisher string `json:"publisher"`
	Expires   int64  `json:"expires"`
}

// consume reserves the token before report storage. A failed upload burns the
// token, rather than risking a duplicate after a crash. Obtain a new token only
// after checking whether the previous report was stored. The hub OS lock limits
// this ledger to a single process. Never restore a snapshot while tokens live.
func (v *ciVerifier) consume(c ciClaims, publisher CIPublisher) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	path := filepath.Join(v.root, replayFilename)
	var entries []replayRecord
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256*1024 {
			return errors.New("invalid replay ledger")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(f, 256*1024+1))
		_ = f.Close()
		if err != nil || len(raw) > 256*1024 || decodeStrict(raw, &entries) != nil || len(entries) > 1000 {
			return errors.New("invalid replay ledger content")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := v.now().Unix()
	digest := sha256.Sum256([]byte(githubIssuer + "\n" + c.ID))
	hash := hex.EncodeToString(digest[:])
	active := make([]replayRecord, 0, len(entries)+1)
	count := 0
	for _, entry := range entries {
		if len(entry.Digest) != 64 || !workspacePattern.MatchString(entry.Publisher) || entry.Expires <= 0 || entry.Expires > now+630 {
			return errors.New("invalid replay ledger record")
		}
		if entry.Expires <= now {
			continue
		}
		if entry.Digest == hash {
			return errors.New("CI token was already used")
		}
		if entry.Publisher == publisher.SubjectID {
			count++
		}
		active = append(active, entry)
	}
	if len(active) >= 1000 || count >= 32 {
		return errors.New("CI replay capacity reached; wait for token expiry")
	}
	active = append(active, replayRecord{Digest: hash, Publisher: publisher.SubjectID, Expires: c.Expires})
	raw, err := json.Marshal(active)
	if err != nil {
		return err
	}
	return atomicWrite(v.root, replayFilename, raw)
}

func (s *Server) handleCI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "CI query parameters are not supported")
		return
	}
	s.mu.RLock()
	enabled := len(s.config.CIPublishers) > 0
	quarantine := s.config.CIQuarantineUntil
	s.mu.RUnlock()
	if !enabled {
		writeError(w, http.StatusNotFound, "CI publishing is not enabled")
		return
	}
	if ciQuarantined(w, quarantine, s.ci.now()) {
		return
	}
	select {
	case s.ci.slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "CI publisher is busy")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		<-s.ci.slots
		writeError(w, http.StatusUnauthorized, "a GitHub Actions identity token is required")
		return
	}
	claims, err := s.ci.verify(r.Context(), strings.TrimPrefix(headers[0], "Bearer "))
	<-s.ci.slots
	if err != nil {
		writeError(w, http.StatusUnauthorized, "GitHub Actions identity could not be verified")
		return
	}
	// Unauthenticated traffic cannot consume any configured workspace's budget.
	// Verification slots are released before reading a potentially slow body.
	s.mu.RLock()
	initialPublisher, authorized := matchingPublisher(s.config, claims)
	workspaceSlot := s.workspaceSlots[initialPublisher.Workspace]
	allowed := authorized && s.ci.allowPublisher(s.config, initialPublisher.Workspace)
	s.mu.RUnlock()
	if !authorized {
		writeError(w, http.StatusForbidden, "CI identity is not authorized for a workspace")
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "CI workspace request rate exceeded")
		return
	}
	select {
	case workspaceSlot <- struct{}{}:
		defer func() { <-workspaceSlot }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "workspace already has two requests in progress")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "hub is busy")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "summary exceeds 16 KiB")
		} else {
			writeError(w, http.StatusBadRequest, "could not read CI summary")
		}
		return
	}
	var submission Submission
	if decodeStrict(raw, &submission) != nil {
		writeError(w, http.StatusBadRequest, "invalid CI summary JSON")
		return
	}
	if err := submission.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if submission.Repository != claims.Repository || submission.CommitSHA != claims.SHA || submission.RunURL != "https://github.com/"+claims.Repository+"/actions/runs/"+claims.RunID {
		writeError(w, http.StatusForbidden, "summary repository, commit and run must match the verified CI identity")
		return
	}
	// Recheck current trust under the reload lock immediately before mutation.
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ciQuarantined(w, s.config.CIQuarantineUntil, s.ci.now()) {
		return
	}
	publisher, ok := matchingPublisher(s.config, claims)
	if !ok || claims.Expires <= s.ci.now().Unix() {
		writeError(w, http.StatusForbidden, "CI identity is not authorized for a workspace")
		return
	}
	event := AuditEvent{Actor: publisher.SubjectID, Kind: "service", Workspace: publisher.Workspace, CredentialID: "github-actions-oidc", Action: "report.create.ci", Outcome: "intent"}
	if err := s.audit.Append(event); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit storage is unavailable")
		return
	}
	s.store.mu.Lock()
	err = s.ci.consume(claims, publisher)
	s.store.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusConflict, "CI token cannot be reserved; it may be used already or replay storage is unavailable")
		return
	}
	identity := &PublisherIdentity{Provider: "github-actions-oidc", SubjectID: publisher.SubjectID, RepositoryID: claims.RepositoryID, RepositoryOwnerID: claims.RepositoryOwnerID, WorkflowRef: claims.WorkflowRef, Ref: claims.Ref, RunID: claims.RunID, RunAttempt: claims.RunAttempt}
	report, err := s.store.CreateVerified(publisher.Workspace, submission, identity)
	if err != nil {
		storeError(w, err)
		return
	}
	event.Outcome, event.ReportID = "success", report.ID
	if err := s.audit.Append(event); err != nil {
		writeError(w, http.StatusServiceUnavailable, "report was stored but audit completion failed; inspect the workspace before retrying")
		return
	}
	w.Header().Set("Location", "/api/v1/reports/"+report.ID)
	writeJSON(w, http.StatusCreated, report)
}

func (v *ciVerifier) allowPublisher(config Config, workspace string) bool {
	// Drop removed workspaces so repeated configuration reloads cannot grow this
	// limiter indefinitely. Current callers hold the server configuration lock.
	v.limiter.mu.Lock()
	for existing := range v.limiter.buckets {
		found := false
		for _, publisher := range config.CIPublishers {
			if publisher.Workspace == existing {
				found = true
				break
			}
		}
		if !found {
			delete(v.limiter.buckets, existing)
		}
	}
	v.limiter.mu.Unlock()
	return v.limiter.allow(workspace)
}

func ciQuarantined(w http.ResponseWriter, until *time.Time, now time.Time) bool {
	if until == nil || !now.Before(*until) {
		return false
	}
	w.Header().Set("Retry-After", strconv.FormatInt(int64(until.Sub(now).Seconds())+1, 10))
	writeError(w, http.StatusServiceUnavailable, "CI publishing is temporarily paused after a restore; obtain a new identity token after the retry interval")
	return true
}

// PublisherIdentity describes verified origin only. Diagnosis confidence and
// experimental outcomes remain client-reported regardless of this metadata.
type PublisherIdentity struct {
	Provider          string `json:"provider"`
	SubjectID         string `json:"subject_id"`
	RepositoryID      string `json:"repository_id"`
	RepositoryOwnerID string `json:"repository_owner_id"`
	WorkflowRef       string `json:"workflow_ref"`
	Ref               string `json:"ref"`
	RunID             string `json:"run_id"`
	RunAttempt        string `json:"run_attempt"`
}

func (p *PublisherIdentity) validate(s Submission) error {
	if p == nil {
		return nil
	}
	file := strings.TrimSuffix(strings.TrimPrefix(p.WorkflowRef, s.Repository+"/.github/workflows/"), "@"+p.Ref)
	if p.Provider != "github-actions-oidc" || !workspacePattern.MatchString(p.SubjectID) || !decimalID.MatchString(p.RepositoryID) || !decimalID.MatchString(p.RepositoryOwnerID) || !decimalID.MatchString(p.RunID) || !decimalID.MatchString(p.RunAttempt) || !branchRef.MatchString(p.Ref) || !workflowFile.MatchString(file) || p.WorkflowRef != s.Repository+"/.github/workflows/"+file+"@"+p.Ref || s.CommitSHA == "" || s.RunURL != fmt.Sprintf("https://github.com/%s/actions/runs/%s", s.Repository, p.RunID) {
		return errors.New("invalid stored publisher identity")
	}
	return nil
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/auth"
	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/service"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func testServer(t *testing.T, remote bool) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	token := "correct-horse-battery-staple"
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.RemoteExecutionEnabled = remote
	cfg.AllowedCommands = []string{"/usr/bin/true"}
	cfg.AllowedWorkingDirectories = []string{workspace}
	cfg.Tokens = []config.Token{{Name: "test", Hash: auth.HashToken(token), Scopes: []string{"read", "capture:write", "analysis:write"}}}
	dataStore, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, dataStore, service.New(cfg, dataStore, runner.New())), token
}

func TestHealthUnauthenticated(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestVersionRequiresToken(t *testing.T) {
	server, token := testServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestRemoteExecutionDisabled(t *testing.T) {
	server, token := testServer(t, false)
	body, _ := json.Marshal(model.CaptureJobRequest{
		Command: model.CommandSpec{Arguments: []string{"/usr/bin/true"}, Directory: "/tmp"},
		Oracle:  model.Oracle{Kind: "exit", ExpectedExitCode: intPointer(0)},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/captures", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", "capture-disabled-001")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestCaptureIdempotency(t *testing.T) {
	server, token := testServer(t, true)
	workspace := server.cfg.AllowedWorkingDirectories[0]
	body, _ := json.Marshal(model.CaptureJobRequest{
		Command: model.CommandSpec{Arguments: []string{"/usr/bin/true"}, Directory: workspace, TimeoutMS: 1000},
		Oracle:  model.Oracle{Kind: "exit", ExpectedExitCode: intPointer(0)},
	})
	call := func(payload []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/captures", bytes.NewReader(payload))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", "capture-test-key")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}
	first := call(body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	second := call(body)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s", second.Code, second.Body.String())
	}
	body = bytes.Replace(body, []byte("1000"), []byte("1001"), 1)
	conflict := call(body)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestRequestBodyBound(t *testing.T) {
	server, token := testServer(t, true)
	server.cfg.Quotas.MaxRequestBytes = 16
	request := httptest.NewRequest(http.MethodPost, "/api/v1/captures", bytes.NewReader(bytes.Repeat([]byte("x"), 1024)))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", "request-body-bound")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing content security policy")
	}
	if response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing frame protection")
	}
}

func TestTraceWritten(t *testing.T) {
	server, _ := testServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("traceparent", "00-00000000000000000000000000000001-0000000000000001-01")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(server.cfg.DataDir, "traces", "spans.jsonl")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("trace not written")
}

func intPointer(value int) *int {
	return &value
}

var _ = context.Background

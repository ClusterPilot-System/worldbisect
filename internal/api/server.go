package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/auth"
	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/service"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
	"github.com/ClusterPilot-System/worldbisect/internal/version"
	"github.com/ClusterPilot-System/worldbisect/web"
)

type Server struct {
	cfg     config.Config
	store   *store.Store
	service *service.Service
	metrics metrics
	mux     *http.ServeMux
}

type metrics struct {
	requests  atomic.Uint64
	errors    atomic.Uint64
	captures  atomic.Uint64
	analyses  atomic.Uint64
	authFails atomic.Uint64
}

type principal struct {
	Name   string
	Scopes map[string]bool
	Hash   string
}

type contextKey string

const principalKey contextKey = "principal"

func New(cfg config.Config, dataStore *store.Store, runtimeService *service.Service) *Server {
	server := &Server{cfg: cfg, store: dataStore, service: runtimeService, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (server *Server) Handler() http.Handler {
	return server.securityHeaders(server.requestTracing(server.recoverer(server.mux)))
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /api/v1/health", server.health)
	server.mux.Handle("GET /api/v1/version", server.authenticate("read", http.HandlerFunc(server.version)))
	server.mux.Handle("GET /api/v1/captures", server.authenticate("read", http.HandlerFunc(server.listCaptures)))
	server.mux.Handle("POST /api/v1/captures", server.authenticate("capture:write", http.HandlerFunc(server.createCapture)))
	server.mux.Handle("GET /api/v1/captures/{id}", server.authenticate("read", http.HandlerFunc(server.getCapture)))
	server.mux.Handle("GET /api/v1/analyses", server.authenticate("read", http.HandlerFunc(server.listAnalyses)))
	server.mux.Handle("POST /api/v1/analyses", server.authenticate("analysis:write", http.HandlerFunc(server.createAnalysis)))
	server.mux.Handle("GET /api/v1/analyses/{id}", server.authenticate("read", http.HandlerFunc(server.getAnalysis)))
	server.mux.Handle("GET /api/v1/jobs", server.authenticate("read", http.HandlerFunc(server.listJobs)))
	server.mux.Handle("GET /api/v1/jobs/{id}", server.authenticate("read", http.HandlerFunc(server.getJob)))
	server.mux.Handle("POST /api/v1/audit/verify", server.authenticate("read", http.HandlerFunc(server.verifyAudit)))
	server.mux.Handle("GET /api/v1/metrics", server.authenticate("read", http.HandlerFunc(server.prometheus)))
	server.mux.Handle("GET /", server.authenticate("read", http.HandlerFunc(server.dashboard)))
}

func (server *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		writer.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) requestTracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.metrics.requests.Add(1)
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = randomID("req")
		}
		traceParent := request.Header.Get("traceparent")
		if traceParent == "" {
			traceParent = newTraceParent()
		}
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("traceparent", traceParent)
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		span := model.TraceSpan{
			TraceParent: traceParent,
			RequestID:   requestID,
			Name:        request.Method + " " + request.URL.Path,
			StartedAt:   start.UTC(),
			EndedAt:     time.Now().UTC(),
			Status:      recorder.status,
		}
		_ = server.store.AppendTrace(span)
		log.Printf(`{"level":"info","request_id":%q,"method":%q,"path":%q,"status":%d,"duration_ms":%d}`,
			requestID, request.Method, request.URL.Path, recorder.status, time.Since(start).Milliseconds())
	})
}

func (server *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				server.metrics.errors.Add(1)
				server.writeError(writer, request, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) authenticate(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization := request.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			server.metrics.authFails.Add(1)
			server.writeError(writer, request, http.StatusUnauthorized, "unauthorized", "bearer token required", nil)
			return
		}
		raw := strings.TrimPrefix(authorization, "Bearer ")
		for _, token := range server.cfg.Tokens {
			if auth.VerifyToken(raw, token.Hash) {
				scopes := make(map[string]bool, len(token.Scopes))
				for _, value := range token.Scopes {
					scopes[value] = true
				}
				if scope != "" && !scopes[scope] && !scopes["admin"] {
					server.writeError(writer, request, http.StatusForbidden, "forbidden", "token lacks required scope", map[string]any{"scope": scope})
					return
				}
				principalValue := principal{Name: token.Name, Scopes: scopes, Hash: auth.Fingerprint(raw)}
				request = request.WithContext(contextWithPrincipal(request.Context(), principalValue))
				next.ServeHTTP(writer, request)
				return
			}
		}
		server.metrics.authFails.Add(1)
		server.writeError(writer, request, http.StatusUnauthorized, "unauthorized", "invalid bearer token", nil)
	})
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	server.writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) version(writer http.ResponseWriter, _ *http.Request) {
	server.writeJSON(writer, http.StatusOK, version.Info())
}

func (server *Server) listCaptures(writer http.ResponseWriter, request *http.Request) {
	values, err := server.store.ListCaptures(limit(request))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, values)
}

func (server *Server) getCapture(writer http.ResponseWriter, request *http.Request) {
	value, err := server.store.LoadCapture(request.PathValue("id"))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, value)
}

func (server *Server) createCapture(writer http.ResponseWriter, request *http.Request) {
	if !server.cfg.RemoteExecutionEnabled {
		server.writeError(writer, request, http.StatusForbidden, "remote_execution_disabled", "remote execution is disabled", nil)
		return
	}
	var body model.CaptureJobRequest
	if err := server.decodeJSON(writer, request, &body); err != nil {
		return
	}
	principalValue := principalFromContext(request.Context())
	job, existing, err := server.service.EnqueueCapture(request.Context(), principalValue.Name, principalValue.Hash, request.Header.Get("Idempotency-Key"), body)
	if err != nil {
		server.writeServiceError(writer, request, err)
		return
	}
	if existing {
		server.writeJSON(writer, http.StatusOK, job)
		return
	}
	server.metrics.captures.Add(1)
	server.writeJSON(writer, http.StatusAccepted, job)
}

func (server *Server) listAnalyses(writer http.ResponseWriter, request *http.Request) {
	values, err := server.store.ListAnalyses(limit(request))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, values)
}

func (server *Server) getAnalysis(writer http.ResponseWriter, request *http.Request) {
	value, err := server.store.LoadAnalysis(request.PathValue("id"))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, value)
}

func (server *Server) createAnalysis(writer http.ResponseWriter, request *http.Request) {
	var body model.AnalysisJobRequest
	if err := server.decodeJSON(writer, request, &body); err != nil {
		return
	}
	principalValue := principalFromContext(request.Context())
	job, existing, err := server.service.EnqueueAnalysis(request.Context(), principalValue.Name, principalValue.Hash, request.Header.Get("Idempotency-Key"), body)
	if err != nil {
		server.writeServiceError(writer, request, err)
		return
	}
	if existing {
		server.writeJSON(writer, http.StatusOK, job)
		return
	}
	server.metrics.analyses.Add(1)
	server.writeJSON(writer, http.StatusAccepted, job)
}

func (server *Server) listJobs(writer http.ResponseWriter, request *http.Request) {
	values, err := server.store.ListJobs(limit(request))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, values)
}

func (server *Server) getJob(writer http.ResponseWriter, request *http.Request) {
	value, err := server.store.LoadJob(request.PathValue("id"))
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, value)
}

func (server *Server) verifyAudit(writer http.ResponseWriter, request *http.Request) {
	result, err := server.store.VerifyAudit()
	if err != nil {
		server.writeStoreError(writer, request, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, result)
}

func (server *Server) prometheus(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(writer, "# TYPE worldbisect_http_requests_total counter\nworldbisect_http_requests_total %d\n", server.metrics.requests.Load())
	fmt.Fprintf(writer, "# TYPE worldbisect_http_errors_total counter\nworldbisect_http_errors_total %d\n", server.metrics.errors.Load())
	fmt.Fprintf(writer, "# TYPE worldbisect_auth_failures_total counter\nworldbisect_auth_failures_total %d\n", server.metrics.authFails.Load())
	fmt.Fprintf(writer, "# TYPE worldbisect_capture_jobs_total counter\nworldbisect_capture_jobs_total %d\n", server.metrics.captures.Load())
	fmt.Fprintf(writer, "# TYPE worldbisect_analysis_jobs_total counter\nworldbisect_analysis_jobs_total %d\n", server.metrics.analyses.Load())
}

func (server *Server) dashboard(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	content, contentType, err := web.Asset(path)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content)
}

func (server *Server) decodeJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, server.cfg.Quotas.MaxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		server.writeError(writer, request, http.StatusBadRequest, "invalid_json", "invalid JSON request", map[string]any{"cause": err.Error()})
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		server.writeError(writer, request, http.StatusBadRequest, "invalid_json", "request must contain one JSON object", nil)
		return errors.New("trailing JSON")
	}
	return nil
}

func (server *Server) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func (server *Server) writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string, details map[string]any) {
	server.metrics.errors.Add(1)
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = randomID("req")
	}
	server.writeJSON(writer, status, map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"request_id": requestID,
			"details":    details,
		},
	})
}

func (server *Server) writeStoreError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, osErrNotExist()) {
		server.writeError(writer, request, http.StatusNotFound, "not_found", "entity not found", nil)
		return
	}
	server.writeError(writer, request, http.StatusInternalServerError, "store_error", "store operation failed", nil)
}

func (server *Server) writeServiceError(writer http.ResponseWriter, request *http.Request, err error) {
	var typed *service.Error
	if errors.As(err, &typed) {
		server.writeError(writer, request, typed.HTTPStatus, typed.Code, typed.Message, typed.Details)
		return
	}
	server.writeError(writer, request, http.StatusInternalServerError, "service_error", "service operation failed", nil)
}

func limit(request *http.Request) int {
	value, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	if value <= 0 {
		return 50
	}
	if value > 200 {
		return 200
	}
	return value
}

func randomID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func newTraceParent() string {
	trace := make([]byte, 16)
	span := make([]byte, 8)
	_, _ = rand.Read(trace)
	_, _ = rand.Read(span)
	return "00-" + hex.EncodeToString(trace) + "-" + hex.EncodeToString(span) + "-01"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

var principalContexts sync.Map

func contextWithPrincipal(ctx context.Context, value principal) context.Context {
	return context.WithValue(ctx, principalKey, value)
}

func principalFromContext(ctx context.Context) principal {
	value, _ := ctx.Value(principalKey).(principal)
	return value
}

func osErrNotExist() error {
	return os.ErrNotExist
}

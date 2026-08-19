package model

import (
	"encoding/json"
	"time"
)

type ProofStatus string

const (
	StatusProven     ProofStatus = "PROVEN"
	StatusSupported  ProofStatus = "SUPPORTED"
	StatusCorrelated ProofStatus = "CORRELATED"
	StatusUnproven   ProofStatus = "UNPROVEN"
)

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
)

type CommandSpec struct {
	Arguments   []string          `json:"arguments"`
	Directory   string            `json:"directory"`
	Environment map[string]string `json:"environment,omitempty"`
	TimeoutMS   int64             `json:"timeout_ms"`
}

type Oracle struct {
	Kind             string `json:"kind"`
	ExpectedExitCode *int   `json:"expected_exit_code,omitempty"`
	Pattern          string `json:"pattern,omitempty"`
	File             string `json:"file,omitempty"`
	Digest           string `json:"digest,omitempty"`
}

type OracleResult struct {
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type ProcessResult struct {
	ExitCode        int       `json:"exit_code"`
	Signal          string    `json:"signal,omitempty"`
	TimedOut        bool      `json:"timed_out"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	DurationMS      int64     `json:"duration_ms"`
	Stdout          string    `json:"stdout,omitempty"`
	Stderr          string    `json:"stderr,omitempty"`
	OutputTruncated bool      `json:"output_truncated"`
	ConsultedPaths  []string  `json:"consulted_paths,omitempty"`
	Boundaries      []string  `json:"boundaries,omitempty"`
}

type WorkspaceManifest struct {
	Root       string           `json:"root,omitempty"`
	Entries    []WorkspaceEntry `json:"entries"`
	TotalFiles int              `json:"total_files"`
	TotalBytes int64            `json:"total_bytes"`
	Digest     string           `json:"digest"`
}

type WorkspaceEntry struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Mode       uint32 `json:"mode"`
	Size       int64  `json:"size,omitempty"`
	Digest     string `json:"digest,omitempty"`
	BlobDigest string `json:"blob_digest,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type HostEvidence struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Kernel         string `json:"kernel,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	UID            int    `json:"uid"`
	GID            int    `json:"gid"`
	Groups         []int  `json:"groups,omitempty"`
	Capabilities   string `json:"capabilities,omitempty"`
	Seccomp        string `json:"seccomp,omitempty"`
	Cgroups        string `json:"cgroups,omitempty"`
	MountDigest    string `json:"mount_digest,omitempty"`
	ResourceDigest string `json:"resource_digest,omitempty"`
	SecurityDigest string `json:"security_digest,omitempty"`
}

type SecretEvidence struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

type ConsultedPath struct {
	Path        string `json:"path"`
	Relative    string `json:"relative,omitempty"`
	InWorkspace bool   `json:"in_workspace"`
}

type Capture struct {
	SchemaVersion      int               `json:"schema_version"`
	ID                 string            `json:"id"`
	CreatedAt          time.Time         `json:"created_at"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at"`
	Label              string            `json:"label,omitempty"`
	WorkspaceRoot      string            `json:"workspace_root"`
	Command            CommandSpec       `json:"command"`
	Oracle             Oracle            `json:"oracle"`
	Result             ProcessResult     `json:"result"`
	OracleResult       OracleResult      `json:"oracle_result"`
	Before             WorkspaceManifest `json:"before"`
	Workspace          WorkspaceManifest `json:"workspace"`
	Host               HostEvidence      `json:"host"`
	SecretEvidence     []SecretEvidence  `json:"secret_evidence,omitempty"`
	ConsultedPaths     []ConsultedPath   `json:"consulted_paths,omitempty"`
	EvidenceBoundaries []string          `json:"evidence_boundaries,omitempty"`
	Warnings           []string          `json:"warnings,omitempty"`
}

type FactorType string

const (
	FactorEnvironment FactorType = "environment"
	FactorWorkspace   FactorType = "workspace"
)

type Factor struct {
	ID           string         `json:"id"`
	Type         FactorType     `json:"type"`
	Key          string         `json:"key"`
	GoodPresent  bool           `json:"good_present"`
	BadPresent   bool           `json:"bad_present"`
	GoodValue    string         `json:"good_value,omitempty"`
	BadValue     string         `json:"bad_value,omitempty"`
	GoodEntry    WorkspaceEntry `json:"good_entry,omitempty"`
	BadEntry     WorkspaceEntry `json:"bad_entry,omitempty"`
	Intervenable bool           `json:"intervenable"`
}

type Experiment struct {
	ID              string        `json:"id"`
	Kind            string        `json:"kind"`
	StartedAt       time.Time     `json:"started_at"`
	FinishedAt      time.Time     `json:"finished_at"`
	BaseCaptureID   string        `json:"base_capture_id"`
	SourceCaptureID string        `json:"source_capture_id"`
	FactorIDs       []string      `json:"factor_ids,omitempty"`
	Result          ProcessResult `json:"result"`
	OracleResult    OracleResult  `json:"oracle_result"`
	Error           string        `json:"error,omitempty"`
}

type Analysis struct {
	SchemaVersion      int          `json:"schema_version"`
	ID                 string       `json:"id"`
	CreatedAt          time.Time    `json:"created_at"`
	GoodCaptureID      string       `json:"good_capture_id"`
	BadCaptureID       string       `json:"bad_capture_id"`
	Factors            []Factor     `json:"factors"`
	CausalFactors      []string     `json:"causal_factors,omitempty"`
	Experiments        []Experiment `json:"experiments"`
	Status             ProofStatus  `json:"status"`
	ForwardVerified    bool         `json:"forward_verified"`
	ReverseVerified    bool         `json:"reverse_verified"`
	MinimalInModel     bool         `json:"minimal_in_model"`
	Repetitions        int          `json:"repetitions"`
	ExperimentBudget   int          `json:"experiment_budget"`
	Summary            string       `json:"summary,omitempty"`
	EvidenceBoundaries []string     `json:"evidence_boundaries,omitempty"`
	Limitations        []string     `json:"limitations,omitempty"`
}

type Job struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	State         JobState        `json:"state"`
	Payload       json.RawMessage `json:"payload"`
	Principal     string          `json:"principal"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	LeaseOwner    string          `json:"lease_owner,omitempty"`
	LeaseExpires  time.Time       `json:"lease_expires,omitempty"`
	HeartbeatAt   time.Time       `json:"heartbeat_at,omitempty"`
	Attempts      int             `json:"attempts"`
	ResultRef     string          `json:"result_ref,omitempty"`
	Error         string          `json:"error,omitempty"`
}

type IdempotencyRecord struct {
	SchemaVersion        int       `json:"schema_version"`
	PrincipalFingerprint string    `json:"principal_fingerprint"`
	Route                string    `json:"route"`
	Key                  string    `json:"key"`
	RequestDigest        string    `json:"request_digest"`
	JobID                string    `json:"job_id"`
	CreatedAt            time.Time `json:"created_at"`
}

type AuditEvent struct {
	Version      int            `json:"version"`
	Sequence     uint64         `json:"sequence"`
	Timestamp    time.Time      `json:"timestamp"`
	Actor        string         `json:"actor"`
	Action       string         `json:"action"`
	EntityType   string         `json:"entity_type"`
	EntityID     string         `json:"entity_id"`
	RequestID    string         `json:"request_id,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
	PreviousHash string         `json:"previous_hash"`
	Hash         string         `json:"hash"`
}

type AuditVerification struct {
	Valid   bool   `json:"valid"`
	Entries uint64 `json:"entries"`
	Error   string `json:"error,omitempty"`
}

type TraceSpan struct {
	TraceParent string    `json:"traceparent"`
	RequestID   string    `json:"request_id"`
	Name        string    `json:"name"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	Status      int       `json:"status"`
}

type CaptureJobRequest struct {
	Command CommandSpec `json:"command"`
	Oracle  Oracle      `json:"oracle"`
	Label   string      `json:"label,omitempty"`
}

type AnalysisJobRequest struct {
	GoodCaptureID string `json:"good_capture_id"`
	BadCaptureID  string `json:"bad_capture_id"`
	Repetitions   int    `json:"repetitions,omitempty"`
}

type CaptureLimits struct {
	MaxWorkspaceFiles int
	MaxWorkspaceBytes int64
	MaxOutputBytes    int64
	Timeout           time.Duration
}

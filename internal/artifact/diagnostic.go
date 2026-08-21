package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/redact"
	"github.com/ClusterPilot-System/worldbisect/internal/report"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

const diagnosticBundleFormat = "worldbisect.diagnostic.v1"

var ErrNotDiagnosticBundle = errors.New("not a diagnostic bundle")

type DiagnosticPreview struct {
	IncidentID           string   `json:"incident_id"`
	AnalysisID           string   `json:"analysis_id"`
	Status               string   `json:"status"`
	Files                []string `json:"files"`
	RedactedFields       []string `json:"redacted_fields"`
	ConfirmationRequired bool     `json:"confirmation_required"`
	RetentionGuidance    string   `json:"retention_guidance"`
}

type diagnosticManifest struct {
	Format        string            `json:"format"`
	SchemaVersion int               `json:"schema_version"`
	IncidentID    string            `json:"incident_id"`
	AnalysisID    string            `json:"analysis_id"`
	CreatedAt     string            `json:"created_at"`
	Entries       []diagnosticEntry `json:"entries"`
}

type diagnosticEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

const (
	maxDiagnosticEntries = 32
	maxDiagnosticTotal   = 128 << 20
)

// ExportDiagnostic writes a deterministic, redacted handoff for an analysis.
// It intentionally contains metadata and evidence references, not raw output or
// workspace content, because those may contain customer secrets.
func ExportDiagnostic(dataStore *store.Store, analysisID, output string) error {
	analysis, err := dataStore.LoadAnalysis(analysisID)
	if err != nil {
		return err
	}
	if analysis.GoodCaptureID == "" || analysis.BadCaptureID == "" {
		return errors.New("analysis does not reference both good and bad captures")
	}
	good, err := dataStore.LoadCapture(analysis.GoodCaptureID)
	if err != nil {
		return err
	}
	bad, err := dataStore.LoadCapture(analysis.BadCaptureID)
	if err != nil {
		return err
	}
	redactedAnalysis := redactAnalysis(analysis)
	redactedGood := redactCapture(good)
	redactedBad := redactCapture(bad)

	certificate, err := certificateForAnalysisWithEvidence(dataStore, redactedAnalysis, redactedGood, redactedBad)
	if err != nil {
		return err
	}
	certificateBytes, err := canonicalJSON(certificate)
	if err != nil {
		return err
	}
	analysisBytes, err := canonicalJSON(redactedAnalysis)
	if err != nil {
		return err
	}
	goodBytes, err := canonicalJSON(redactedGood)
	if err != nil {
		return err
	}
	badBytes, err := canonicalJSON(redactedBad)
	if err != nil {
		return err
	}
	reportJSON, err := report.JSON(redactedAnalysis)
	if err != nil {
		return err
	}
	reportMarkdown := []byte(report.Markdown(redactedAnalysis))

	entries := []archiveEntry{
		{Name: "analysis.json", Content: analysisBytes},
		{Name: "captures/bad.json", Content: badBytes},
		{Name: "captures/good.json", Content: goodBytes},
		{Name: "certificate.json", Content: certificateBytes},
		{Name: "report.json", Content: reportJSON},
		{Name: "report.md", Content: reportMarkdown},
	}
	manifest := diagnosticManifest{
		Format: diagnosticBundleFormat, SchemaVersion: 1, IncidentID: diagnosticIncidentID(redactedAnalysis, redactedGood, redactedBad), AnalysisID: redactedAnalysis.ID,
		CreatedAt: redactedAnalysis.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	for _, entry := range entries {
		manifest.Entries = append(manifest.Entries, diagnosticEntry{Path: entry.Name, Size: int64(len(entry.Content)), Digest: digest(entry.Content)})
	}
	manifestBytes, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	entries = append(entries, archiveEntry{Name: "manifest.json", Content: manifestBytes})
	return writeDeterministicTarGzip(output, entries)
}

// PreviewDiagnostic performs the same load and redaction preparation as
// export without writing an artifact. The CLI displays this before requiring
// explicit operator confirmation.
func PreviewDiagnostic(dataStore *store.Store, analysisID string) (DiagnosticPreview, error) {
	analysis, err := dataStore.LoadAnalysis(analysisID)
	if err != nil {
		return DiagnosticPreview{}, err
	}
	if analysis.GoodCaptureID == "" || analysis.BadCaptureID == "" {
		return DiagnosticPreview{}, errors.New("analysis does not reference both good and bad captures")
	}
	good, err := dataStore.LoadCapture(analysis.GoodCaptureID)
	if err != nil {
		return DiagnosticPreview{}, err
	}
	bad, err := dataStore.LoadCapture(analysis.BadCaptureID)
	if err != nil {
		return DiagnosticPreview{}, err
	}
	redactedAnalysis := redactAnalysis(analysis)
	redactedGood := redactCapture(good)
	redactedBad := redactCapture(bad)
	return DiagnosticPreview{
		IncidentID:           diagnosticIncidentID(redactedAnalysis, redactedGood, redactedBad),
		AnalysisID:           analysis.ID,
		Status:               string(analysis.Status),
		Files:                []string{"analysis.json", "captures/good.json", "captures/bad.json", "certificate.json", "report.json", "report.md", "manifest.json"},
		RedactedFields:       []string{"workspace roots", "command arguments and directories", "command output and errors", "sensitive environment values", "oracle secrets and workspace digests", "factor values and experiment output"},
		ConfirmationRequired: true,
		RetentionGuidance:    "retain only for the support period; delete the bundle and certificate after the case is closed",
	}, nil
}

func diagnosticIncidentID(analysis *model.Analysis, good, bad *model.Capture) string {
	content, _ := canonicalJSON(struct {
		Analysis *model.Analysis `json:"analysis"`
		Good     *model.Capture  `json:"good"`
		Bad      *model.Capture  `json:"bad"`
	}{Analysis: analysis, Good: good, Bad: bad})
	return "inc-" + digest(content)[:16]
}

// ImportDiagnostic validates every declared byte before persisting any entity.
// The returned certificate is already verified and can be written for offline use.
func ImportDiagnostic(dataStore *store.Store, bundlePath string) (string, []byte, error) {
	entries, err := readDiagnosticArchive(bundlePath)
	if err != nil {
		return "", nil, err
	}
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		return "", nil, ErrNotDiagnosticBundle
	}
	var envelope struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(manifestBytes, &envelope); err != nil || envelope.Format != diagnosticBundleFormat {
		return "", nil, ErrNotDiagnosticBundle
	}
	var manifest diagnosticManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return "", nil, fmt.Errorf("decode diagnostic manifest: %w", err)
	}
	if manifest.Format != diagnosticBundleFormat {
		return "", nil, ErrNotDiagnosticBundle
	}
	if manifest.SchemaVersion != 1 || !validDiagnosticID(manifest.AnalysisID) || len(manifest.Entries) != 6 {
		return "", nil, errors.New("invalid diagnostic manifest")
	}
	declared := make(map[string]diagnosticEntry, len(manifest.Entries))
	for _, item := range manifest.Entries {
		if err := validateArchiveName(item.Path); err != nil || item.Size < 0 || item.Size > maxEntityBytes || !validDigest(item.Digest) {
			return "", nil, errors.New("invalid diagnostic entry")
		}
		if _, exists := declared[item.Path]; exists || item.Path == "manifest.json" {
			return "", nil, errors.New("duplicate diagnostic entry")
		}
		declared[item.Path] = item
		content, exists := entries[item.Path]
		if !exists || int64(len(content)) != item.Size || digest(content) != item.Digest {
			return "", nil, fmt.Errorf("diagnostic entry %s failed validation", item.Path)
		}
	}
	if len(entries) != len(declared)+1 {
		return "", nil, errors.New("unexpected diagnostic archive entry")
	}
	for name := range entries {
		if name != "manifest.json" {
			if _, exists := declared[name]; !exists {
				return "", nil, fmt.Errorf("unexpected diagnostic archive entry %q", name)
			}
		}
	}

	var analysis model.Analysis
	if err := decodeStrict(entries["analysis.json"], &analysis); err != nil {
		return "", nil, fmt.Errorf("decode diagnostic analysis: %w", err)
	}
	if analysis.ID != manifest.AnalysisID {
		return "", nil, errors.New("diagnostic analysis ID does not match manifest")
	}
	var good, bad model.Capture
	if err := decodeStrict(entries["captures/good.json"], &good); err != nil {
		return "", nil, fmt.Errorf("decode good capture: %w", err)
	}
	if err := decodeStrict(entries["captures/bad.json"], &bad); err != nil {
		return "", nil, fmt.Errorf("decode bad capture: %w", err)
	}
	if good.ID != analysis.GoodCaptureID || bad.ID != analysis.BadCaptureID {
		return "", nil, errors.New("diagnostic capture references do not match analysis")
	}
	var certificate Certificate
	if err := decodeStrict(entries["certificate.json"], &certificate); err != nil {
		return "", nil, fmt.Errorf("decode diagnostic certificate: %w", err)
	}
	if result := verifyCertificateValue(certificate, &analysis, &good, &bad); !result.Valid {
		return "", nil, fmt.Errorf("diagnostic certificate is invalid: %s", result.Error)
	}
	var structured report.AnalysisReport
	if err := decodeStrict(entries["report.json"], &structured); err != nil {
		return "", nil, fmt.Errorf("decode diagnostic report: %w", err)
	}
	if structured.SchemaVersion != report.AnalysisReportSchemaVersion || structured.Format != "worldbisect.analysis-report.v1" || structured.AnalysisID != analysis.ID || structured.Status != string(analysis.Status) {
		return "", nil, errors.New("diagnostic report does not match analysis")
	}
	if !bytes.HasPrefix(entries["report.md"], []byte("# WorldBisect diagnosis\n")) {
		return "", nil, errors.New("diagnostic Markdown report is invalid")
	}

	if err := dataStore.SaveCapture(&good); err != nil {
		return "", nil, err
	}
	if err := dataStore.SaveCapture(&bad); err != nil {
		return "", nil, err
	}
	if err := dataStore.SaveAnalysis(&analysis); err != nil {
		return "", nil, err
	}
	if err := dataStore.AppendAudit("diagnostic-import", "analysis", analysis.ID, map[string]any{"format": diagnosticBundleFormat}); err != nil {
		return "", nil, err
	}
	return analysis.ID, append([]byte(nil), entries["certificate.json"]...), nil
}

func readDiagnosticArchive(bundlePath string) (map[string][]byte, error) {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBundleBytes {
		return nil, fmt.Errorf("bundle exceeds %d bytes", maxBundleBytes)
	}
	file, err := os.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(io.LimitReader(file, maxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	entries := make(map[string][]byte)
	var total int64
	tarReader := tar.NewReader(gzipReader)
	for count := 0; ; count++ {
		if count >= maxDiagnosticEntries {
			return nil, errors.New("diagnostic archive entry limit exceeded")
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := validateArchiveHeader(header); err != nil {
			return nil, err
		}
		if _, exists := entries[header.Name]; exists {
			return nil, fmt.Errorf("duplicate archive entry %q", header.Name)
		}
		if header.Size > maxEntityBytes {
			return nil, fmt.Errorf("entry %q exceeds limit", header.Name)
		}
		total += header.Size
		if total > maxDiagnosticTotal {
			return nil, errors.New("diagnostic archive exceeds total size limit")
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, maxEntityBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(content)) != header.Size {
			return nil, fmt.Errorf("entry %q has invalid size", header.Name)
		}
		entries[header.Name] = content
	}
	return entries, nil
}

func decodeStrict(content []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func redactAnalysis(value *model.Analysis) *model.Analysis {
	copy := cloneAnalysis(value)
	for index := range copy.Factors {
		factor := &copy.Factors[index]
		factor.GoodValue = ""
		factor.BadValue = ""
		factor.GoodEntry.Digest = ""
		factor.GoodEntry.BlobDigest = ""
		factor.BadEntry.Digest = ""
		factor.BadEntry.BlobDigest = ""
		if redact.LooksSecret(factor.Key) {
			factor.Key = "[redacted factor]"
		}
	}
	for index := range copy.Experiments {
		experiment := &copy.Experiments[index]
		experiment.Result.Stdout = ""
		experiment.Result.Stderr = ""
		experiment.Result.ConsultedPaths = nil
		experiment.Error = ""
	}
	return copy
}

func redactCapture(value *model.Capture) *model.Capture {
	copy := cloneCapture(value)
	copy.WorkspaceRoot = "[redacted]"
	copy.Command.Directory = "[redacted]"
	copy.Command.Arguments = []string{"[redacted from diagnostic bundle]"}
	copy.Command.Environment = redactEnvironment(copy.Command.Environment)
	copy.Oracle.Pattern = ""
	copy.Oracle.File = ""
	copy.Oracle.Digest = ""
	copy.Result.Stdout = ""
	copy.Result.Stderr = ""
	copy.Result.ConsultedPaths = nil
	copy.Host.Hostname = ""
	copy.ConsultedPaths = nil
	redactManifest(&copy.Before)
	redactManifest(&copy.Workspace)
	return copy
}

func redactManifest(value *model.WorkspaceManifest) {
	value.Root = "[redacted]"
	value.Digest = ""
	for index := range value.Entries {
		entry := &value.Entries[index]
		entry.Digest = ""
		entry.BlobDigest = ""
		if redact.LooksSecret(entry.Path) {
			entry.Path = "[redacted path]"
		}
	}
}

func redactEnvironment(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		if redact.LooksSecret(name) {
			result[name] = "[redacted]"
		} else {
			result[name] = value
		}
	}
	return result
}

func cloneAnalysis(value *model.Analysis) *model.Analysis {
	encoded, _ := json.Marshal(value)
	var copy model.Analysis
	_ = json.Unmarshal(encoded, &copy)
	return &copy
}

func cloneCapture(value *model.Capture) *model.Capture {
	encoded, _ := json.Marshal(value)
	var copy model.Capture
	_ = json.Unmarshal(encoded, &copy)
	return &copy
}

func validDiagnosticID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return !strings.Contains(value, "..")
}

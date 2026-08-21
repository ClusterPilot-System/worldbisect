package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestDeterministicExportAndImport(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := dataStore.PutBlob([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	captureValue := &model.Capture{
		SchemaVersion: 3,
		ID:            "capture-example",
		CreatedAt:     time.Unix(1, 0).UTC(),
		Workspace: model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{
			Path: "file.txt", Type: "file", BlobDigest: blob,
		}}},
	}
	if err := dataStore.SaveCapture(captureValue); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.wcap")
	second := filepath.Join(t.TempDir(), "second.wcap")
	if err := ExportCapture(dataStore, captureValue.ID, first); err != nil {
		t.Fatal(err)
	}
	if err := ExportCapture(dataStore, captureValue.ID, second); err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("bundle is not deterministic")
	}
	importStore, err := store.Open(filepath.Join(t.TempDir(), "import"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := ImportBundle(importStore, first)
	if err != nil {
		t.Fatal(err)
	}
	if id != captureValue.ID {
		t.Fatalf("import ID = %s", id)
	}
	content, err := importStore.GetBlob(blob)
	if err != nil || string(content) != "content\n" {
		t.Fatalf("blob import failed: %q %v", content, err)
	}
}

func TestImportRejectsTraversalAndLinks(t *testing.T) {
	cases := []struct {
		name     string
		header   *tar.Header
		contents []byte
	}{
		{name: "traversal", header: &tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, contents: []byte("x")},
		{name: "absolute", header: &tar.Header{Name: "/escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, contents: []byte("x")},
		{name: "symlink", header: &tar.Header{Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{name: "device", header: &tar.Header{Name: "device", Mode: 0o600, Typeflag: tar.TypeChar}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bundle := filepath.Join(t.TempDir(), "bad.wcap")
			file, err := os.Create(bundle)
			if err != nil {
				t.Fatal(err)
			}
			gzipWriter := gzip.NewWriter(file)
			tarWriter := tar.NewWriter(gzipWriter)
			if err := tarWriter.WriteHeader(test.header); err != nil {
				t.Fatal(err)
			}
			if len(test.contents) > 0 {
				_, _ = tarWriter.Write(test.contents)
			}
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			_ = file.Close()
			dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
			if _, err := ImportBundle(dataStore, bundle); err == nil {
				t.Fatal("expected unsafe archive rejection")
			}
		})
	}
}

func TestDiagnosticBundleIsDeterministicRedactedAndVerifiable(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := dataStore.PutBlob([]byte("API_TOKEN=supersecret\n"))
	if err != nil {
		t.Fatal(err)
	}
	good := &model.Capture{
		SchemaVersion: 3, ID: "capture-good", CreatedAt: time.Unix(1, 0).UTC(),
		WorkspaceRoot: "/sensitive/workspace", Command: model.CommandSpec{
			Arguments: []string{"./check.sh", "supersecret"}, Directory: "/sensitive/workspace",
			Environment: map[string]string{"API_TOKEN": "supersecret", "MODE": "good"},
		}, Result: model.ProcessResult{Stdout: "supersecret output", Stderr: "secret error"},
		Workspace: model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{Path: "config.txt", Type: "file", BlobDigest: blob, Digest: blob}}},
	}
	bad := *good
	bad.ID = "capture-bad"
	bad.Result.Stdout = "bad supersecret output"
	analysis := &model.Analysis{
		SchemaVersion: 3, ID: "analysis-diagnostic", CreatedAt: time.Unix(2, 0).UTC(),
		GoodCaptureID: good.ID, BadCaptureID: bad.ID, Status: model.StatusProven,
		CausalFactors: []string{"workspace:config.txt"}, Factors: []model.Factor{{
			ID: "workspace:config.txt", Type: model.FactorWorkspace, Key: "config.txt",
			GoodValue: "supersecret", BadValue: "othersecret", GoodEntry: model.WorkspaceEntry{BlobDigest: blob},
		}},
	}
	if err := dataStore.SaveCapture(good); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveCapture(&bad); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewDiagnostic(dataStore, analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.IncidentID == "" || preview.AnalysisID != analysis.ID || !preview.ConfirmationRequired || len(preview.RedactedFields) == 0 {
		t.Fatalf("invalid handoff preview: %+v", preview)
	}
	first := filepath.Join(t.TempDir(), "first.wdiag")
	second := filepath.Join(t.TempDir(), "second.wdiag")
	if err := ExportDiagnostic(dataStore, analysis.ID, first); err != nil {
		t.Fatal(err)
	}
	if err := ExportDiagnostic(dataStore, analysis.ID, second); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("diagnostic bundle is not deterministic")
	}
	entries, err := readDiagnosticArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(entries["manifest.json"], []byte(preview.IncidentID)) {
		t.Fatal("diagnostic bundle does not contain the preview incident ID")
	}
	if bytes.Contains(firstBytes, []byte("supersecret")) || bytes.Contains(firstBytes, []byte("othersecret")) {
		t.Fatal("diagnostic bundle contains unredacted secret material")
	}

	importStore, err := store.Open(filepath.Join(t.TempDir(), "import"))
	if err != nil {
		t.Fatal(err)
	}
	id, certificate, err := ImportDiagnostic(importStore, first)
	if err != nil || id != analysis.ID {
		t.Fatalf("diagnostic import = %s, %v", id, err)
	}
	certificatePath := filepath.Join(t.TempDir(), "result.wbc")
	if err := os.WriteFile(certificatePath, certificate, 0o644); err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyCertificate(certificatePath, "")
	if err != nil || !verified.Valid || verified.AnalysisID != analysis.ID {
		t.Fatalf("offline certificate verification failed: %+v %v", verified, err)
	}
	imported, err := importStore.LoadAnalysis(analysis.ID)
	if err != nil || imported.Status != model.StatusProven {
		t.Fatalf("imported analysis unavailable: %+v %v", imported, err)
	}
	if _, err := importStore.GetBlob(blob); err == nil {
		t.Fatal("diagnostic import unexpectedly restored raw workspace blob")
	}
}

func TestDiagnosticImportRejectsUnsafeArchive(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "unsafe.wdiag")
	file, err := os.Create(bundle)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tarWriter.Write([]byte("x"))
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	_ = file.Close()
	dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
	if _, _, err := ImportDiagnostic(dataStore, bundle); err == nil {
		t.Fatal("expected unsafe diagnostic archive rejection")
	}
}

package artifact

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestCertificateRoundTrip(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	analysis := &model.Analysis{SchemaVersion: 3, ID: "analysis-1", CreatedAt: time.Now().UTC(), Status: model.StatusProven}
	if err := dataStore.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}
	certificate := filepath.Join(t.TempDir(), "result.wbc")
	if err := WriteCertificate(dataStore, analysis.ID, certificate); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyCertificate(certificate, filepath.Join(dataStore.Root(), "keys", "causal_ed25519_public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.AnalysisID != analysis.ID {
		t.Fatalf("verification failed: %+v", result)
	}
	content, err := os.ReadFile(certificate)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)/2] ^= 1
	if err := os.WriteFile(certificate, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCertificate(certificate, ""); err == nil {
		t.Fatal("expected malformed or invalid certificate")
	}
}

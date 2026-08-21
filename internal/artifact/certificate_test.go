package artifact

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestCertificateRoundTripAndSecretSafeClaims(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	good := &model.Capture{SchemaVersion: 3, ID: "good-1", CreatedAt: createdAt, Label: "good"}
	bad := &model.Capture{SchemaVersion: 3, ID: "bad-1", CreatedAt: createdAt, Label: "bad"}
	if err := dataStore.SaveCapture(good); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveCapture(bad); err != nil {
		t.Fatal(err)
	}
	analysis := &model.Analysis{
		SchemaVersion: 3, ID: "analysis-1", CreatedAt: createdAt,
		GoodCaptureID: good.ID, BadCaptureID: bad.ID, Status: model.StatusProven,
		Summary: "supersecret summary", Factors: []model.Factor{{ID: "factor-1", GoodValue: "supersecret"}},
		Experiments: []model.Experiment{{ID: "experiment-1", Error: "supersecret experiment output"}},
	}
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
	if !result.Valid || result.Format != certificateV2 || result.AnalysisID != analysis.ID || result.Evidence == nil {
		t.Fatalf("verification failed: %+v", result)
	}
	content, err := os.ReadFile(certificate)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "supersecret") || strings.Contains(string(content), "experiment-1") {
		t.Fatal("certificate contains raw or secret-bearing evidence")
	}

	var tampered Certificate
	if err := json.Unmarshal(content, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Claims.Status = model.StatusUnproven
	tamperedBytes, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificate, tamperedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	tamperedResult, err := VerifyCertificate(certificate, "")
	if err != nil {
		t.Fatal(err)
	}
	if tamperedResult.Valid || tamperedResult.Error == "" {
		t.Fatalf("tampered certificate unexpectedly verified: %+v", tamperedResult)
	}
}

func TestLegacyCertificateRemainsReadable(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	analysis := model.Analysis{SchemaVersion: 3, ID: "legacy-1", Status: model.StatusProven}
	payload, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	certificate := Certificate{
		Format: certificateV1, Payload: payload,
		PublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	}
	path := filepath.Join(t.TempDir(), "legacy.wbc")
	encoded, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyCertificate(path, "")
	if err != nil || !result.Valid || result.AnalysisID != analysis.ID {
		t.Fatalf("legacy certificate verification failed: %+v %v", result, err)
	}
}

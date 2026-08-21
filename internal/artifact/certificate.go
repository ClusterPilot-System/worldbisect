package artifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

const (
	certificateV1 = "worldbisect.causal-certificate.v1"
	certificateV2 = "worldbisect.causal-certificate.v2"
)

// CertificateEvidence contains only public digests. It deliberately excludes
// capture contents, command output, factor values, and other secret-bearing
// evidence from the offline certificate.
type CertificateEvidence struct {
	GoodCaptureSHA256 string `json:"good_capture_sha256"`
	BadCaptureSHA256  string `json:"bad_capture_sha256"`
	ExperimentsSHA256 string `json:"experiments_sha256"`
	FactorSetSHA256   string `json:"factor_set_sha256"`
}

type CertificateClaims struct {
	Model              string              `json:"model"`
	AnalysisID         string              `json:"analysis_id"`
	Status             model.ProofStatus   `json:"status"`
	GoodCaptureID      string              `json:"good_capture_id"`
	BadCaptureID       string              `json:"bad_capture_id"`
	CausalFactors      []string            `json:"causal_factors,omitempty"`
	ForwardVerified    bool                `json:"forward_verified"`
	ReverseVerified    bool                `json:"reverse_verified"`
	MinimalInModel     bool                `json:"minimal_in_model"`
	EvidenceBoundaries []string            `json:"evidence_boundaries,omitempty"`
	Limitations        []string            `json:"limitations,omitempty"`
	Evidence           CertificateEvidence `json:"evidence"`
}

// Certificate keeps v1 Payload as raw JSON so old certificates remain
// readable while v2 uses secret-safe Claims.
type Certificate struct {
	Format      string             `json:"format"`
	Payload     json.RawMessage    `json:"payload,omitempty"`
	Claims      *CertificateClaims `json:"claims,omitempty"`
	PublicKey   string             `json:"public_key"`
	Signature   string             `json:"signature"`
	GeneratedAt time.Time          `json:"generated_at"`
}

type signedV2Value struct {
	Format      string            `json:"format"`
	Claims      CertificateClaims `json:"claims"`
	GeneratedAt time.Time         `json:"generated_at"`
}

type VerificationResult struct {
	Valid      bool                 `json:"valid"`
	Format     string               `json:"format,omitempty"`
	AnalysisID string               `json:"analysis_id,omitempty"`
	Status     string               `json:"status,omitempty"`
	Trust      string               `json:"trust,omitempty"`
	Model      string               `json:"model,omitempty"`
	Evidence   *CertificateEvidence `json:"evidence,omitempty"`
	NextAction string               `json:"next_action,omitempty"`
	Error      string               `json:"error,omitempty"`
}

func WriteCertificate(dataStore *store.Store, analysisID, output string) error {
	analysis, err := dataStore.LoadAnalysis(analysisID)
	if err != nil {
		return err
	}
	certificate, err := certificateForAnalysis(dataStore, analysis)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(certificate, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(output, append(encoded, '\n'), 0o644)
}

func certificateForAnalysis(dataStore *store.Store, analysis *model.Analysis) (Certificate, error) {
	good, err := dataStore.LoadCapture(analysis.GoodCaptureID)
	if err != nil {
		return Certificate{}, err
	}
	bad, err := dataStore.LoadCapture(analysis.BadCaptureID)
	if err != nil {
		return Certificate{}, err
	}
	return certificateForAnalysisWithEvidence(dataStore, analysis, good, bad)
}

func certificateForAnalysisWithEvidence(dataStore *store.Store, analysis *model.Analysis, good, bad *model.Capture) (Certificate, error) {
	privateKey, publicKey, err := loadOrCreateSigningKey(dataStore.Root())
	if err != nil {
		return Certificate{}, err
	}
	claims := claimsForAnalysis(analysis, good, bad)
	certificate := Certificate{Format: certificateV2, Claims: &claims, PublicKey: base64.RawStdEncoding.EncodeToString(publicKey), GeneratedAt: analysis.CreatedAt}
	payload, err := signedV2Bytes(certificate)
	if err != nil {
		return Certificate{}, err
	}
	certificate.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return certificate, nil
}

func claimsForAnalysis(analysis *model.Analysis, good, bad *model.Capture) CertificateClaims {
	causalFactors := append([]string(nil), analysis.CausalFactors...)
	sort.Strings(causalFactors)
	evidenceBoundaries := append([]string(nil), analysis.EvidenceBoundaries...)
	sort.Strings(evidenceBoundaries)
	limitations := append([]string(nil), analysis.Limitations...)
	sort.Strings(limitations)
	factors := append([]model.Factor(nil), analysis.Factors...)
	sort.Slice(factors, func(i, j int) bool { return factors[i].ID < factors[j].ID })
	return CertificateClaims{
		Model:              "worldbisect.causal-model.v1",
		AnalysisID:         analysis.ID,
		Status:             analysis.Status,
		GoodCaptureID:      analysis.GoodCaptureID,
		BadCaptureID:       analysis.BadCaptureID,
		CausalFactors:      causalFactors,
		ForwardVerified:    analysis.ForwardVerified,
		ReverseVerified:    analysis.ReverseVerified,
		MinimalInModel:     analysis.MinimalInModel,
		EvidenceBoundaries: evidenceBoundaries,
		Limitations:        limitations,
		Evidence: CertificateEvidence{
			GoodCaptureSHA256: hashCertificateValue(good),
			BadCaptureSHA256:  hashCertificateValue(bad),
			ExperimentsSHA256: hashCertificateValue(analysis.Experiments),
			FactorSetSHA256: hashCertificateValue(struct {
				Factors       []model.Factor `json:"factors"`
				CausalFactors []string       `json:"causal_factors"`
			}{Factors: factors, CausalFactors: causalFactors}),
		},
	}
}

func hashCertificateValue(value any) string {
	encoded, _ := canonicalJSON(value)
	return digest(encoded)
}

func signedV2Bytes(certificate Certificate) ([]byte, error) {
	if certificate.Claims == nil {
		return nil, errors.New("certificate claims are required")
	}
	return canonicalJSON(signedV2Value{Format: certificate.Format, Claims: *certificate.Claims, GeneratedAt: certificate.GeneratedAt})
}

func VerifyCertificate(path, publicKeyPath string) (VerificationResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return VerificationResult{}, err
	}
	var certificate Certificate
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&certificate); err != nil {
		return VerificationResult{}, err
	}
	publicKeyBytes, err := base64.RawStdEncoding.DecodeString(certificate.PublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return VerificationResult{Valid: false, Format: certificate.Format, Trust: "untrusted", Error: "invalid embedded public key", NextAction: "obtain a complete certificate and inspect its trust key"}, nil
	}
	trust := "embedded key not independently trusted"
	if publicKeyPath != "" {
		trusted, err := readPublicKey(publicKeyPath)
		if err != nil {
			return VerificationResult{}, err
		}
		if !bytes.Equal(trusted, publicKeyBytes) {
			return VerificationResult{Valid: false, Format: certificate.Format, Trust: "untrusted", Error: "certificate public key is not trusted key", NextAction: "use the public key belonging to the signing trust root"}, nil
		}
		trust = "independently trusted key matched"
	}
	signature, err := base64.RawStdEncoding.DecodeString(certificate.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return VerificationResult{Valid: false, Format: certificate.Format, Trust: trust, Error: "invalid signature encoding", NextAction: "obtain a complete certificate"}, nil
	}
	result := VerificationResult{Valid: false, Format: certificate.Format, Trust: trust}
	var signed []byte
	switch certificate.Format {
	case certificateV1:
		var analysis model.Analysis
		if err := json.Unmarshal(certificate.Payload, &analysis); err != nil {
			return VerificationResult{}, err
		}
		signed, err = json.Marshal(analysis)
		result.AnalysisID = analysis.ID
		result.Status = string(analysis.Status)
		result.Model = "legacy embedded analysis; evidence hashes unavailable"
		result.NextAction = "treat this as a legacy certificate and independently review its evidence"
	case certificateV2:
		if certificate.Claims == nil || certificate.Claims.Model != "worldbisect.causal-model.v1" {
			return VerificationResult{Valid: false, Format: certificate.Format, Trust: trust, Error: "unsupported certificate model", NextAction: "use a certificate with a supported causal model"}, nil
		}
		signed, err = signedV2Bytes(certificate)
		result.AnalysisID = certificate.Claims.AnalysisID
		result.Status = string(certificate.Claims.Status)
		result.Model = certificate.Claims.Model
		result.Evidence = &certificate.Claims.Evidence
		result.NextAction = "compare the evidence hashes with the retained captures and review the model boundaries"
	default:
		return VerificationResult{Valid: false, Format: certificate.Format, Trust: trust, Error: "unsupported certificate format", NextAction: "use a supported certificate version"}, nil
	}
	if err != nil {
		return VerificationResult{}, err
	}
	result.Valid = ed25519.Verify(ed25519.PublicKey(publicKeyBytes), signed, signature)
	if !result.Valid {
		result.Error = "signature verification failed; certificate evidence or metadata was tampered with"
		result.NextAction = "discard this certificate and obtain a fresh signed evidence bundle"
	}
	return result, nil
}

func verifyCertificateValue(certificate Certificate, analysis *model.Analysis, good, bad *model.Capture) VerificationResult {
	result, err := verifyCertificateForAnalysis(certificate, analysis, good, bad)
	if err != nil {
		return VerificationResult{Valid: false, AnalysisID: analysis.ID, Error: err.Error(), NextAction: "discard the certificate and obtain a fresh diagnostic bundle"}
	}
	return result
}

func verifyCertificateForAnalysis(certificate Certificate, analysis *model.Analysis, good, bad *model.Capture) (VerificationResult, error) {
	if certificate.Format == certificateV1 {
		var payload model.Analysis
		if err := json.Unmarshal(certificate.Payload, &payload); err != nil {
			return VerificationResult{}, err
		}
		expected, err := json.Marshal(payload)
		if err != nil || !bytes.Equal(expected, mustJSON(analysis)) {
			return VerificationResult{}, errors.New("certificate payload does not match analysis")
		}
		return verifyCertificateSignature(certificate, expected, string(payload.Status), "legacy embedded analysis; evidence hashes unavailable"), nil
	}
	if certificate.Format != certificateV2 || certificate.Claims == nil {
		return VerificationResult{}, errors.New("unsupported certificate format or missing claims")
	}
	expected := claimsForAnalysis(analysis, good, bad)
	if !bytes.Equal(mustJSON(expected), mustJSON(*certificate.Claims)) {
		return VerificationResult{}, errors.New("certificate claims or evidence hashes do not match analysis")
	}
	signed, err := signedV2Bytes(certificate)
	if err != nil {
		return VerificationResult{}, err
	}
	return verifyCertificateSignature(certificate, signed, string(certificate.Claims.Status), certificate.Claims.Model), nil
}

func verifyCertificateSignature(certificate Certificate, signed []byte, status, modelName string) VerificationResult {
	key, keyErr := base64.RawStdEncoding.DecodeString(certificate.PublicKey)
	signature, sigErr := base64.RawStdEncoding.DecodeString(certificate.Signature)
	valid := keyErr == nil && sigErr == nil && len(key) == ed25519.PublicKeySize && len(signature) == ed25519.SignatureSize && ed25519.Verify(ed25519.PublicKey(key), signed, signature)
	result := VerificationResult{Valid: valid, Format: certificate.Format, AnalysisID: certificate.ClaimsID(), Status: status, Trust: "embedded key not independently trusted", Model: modelName, NextAction: "compare the evidence hashes with retained evidence and review model boundaries"}
	if certificate.Claims != nil {
		result.Evidence = &certificate.Claims.Evidence
	}
	if !valid {
		result.Error = "signature verification failed"
		result.NextAction = "discard this certificate and obtain a fresh signed evidence bundle"
	}
	return result
}

func (certificate Certificate) ClaimsID() string {
	if certificate.Claims != nil {
		return certificate.Claims.AnalysisID
	}
	var analysis model.Analysis
	_ = json.Unmarshal(certificate.Payload, &analysis)
	return analysis.ID
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func loadOrCreateSigningKey(root string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	keyDirectory := filepath.Join(root, "keys")
	privatePath := filepath.Join(keyDirectory, "causal_ed25519_private.pem")
	publicPath := filepath.Join(keyDirectory, "causal_ed25519_public.pem")
	if privateBytes, err := os.ReadFile(privatePath); err == nil {
		block, _ := pem.Decode(privateBytes)
		if block == nil {
			return nil, nil, errors.New("invalid private key PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, nil, err
		}
		privateKey, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, nil, errors.New("signing key is not Ed25519")
		}
		return privateKey, privateKey.Public().(ed25519.PublicKey), nil
	}
	if err := os.MkdirAll(keyDirectory, 0o700); err != nil {
		return nil, nil, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, nil, err
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	if err := atomicWriteFile(privatePath, privatePEM, 0o600); err != nil {
		return nil, nil, err
	}
	if err := atomicWriteFile(publicPath, publicPEM, 0o644); err != nil {
		return nil, nil, err
	}
	return privateKey, publicKey, nil
}

func readPublicKey(path string) (ed25519.PublicKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return key, nil
}

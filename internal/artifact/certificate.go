package artifact

import (
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
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

type Certificate struct {
	Format      string         `json:"format"`
	Payload     model.Analysis `json:"payload"`
	PublicKey   string         `json:"public_key"`
	Signature   string         `json:"signature"`
	GeneratedAt time.Time      `json:"generated_at"`
}

type VerificationResult struct {
	Valid      bool   `json:"valid"`
	AnalysisID string `json:"analysis_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

func WriteCertificate(dataStore *store.Store, analysisID, output string) error {
	analysis, err := dataStore.LoadAnalysis(analysisID)
	if err != nil {
		return err
	}
	privateKey, publicKey, err := loadOrCreateSigningKey(dataStore.Root())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(privateKey, payload)
	certificate := Certificate{
		Format:      "worldbisect.causal-certificate.v1",
		Payload:     *analysis,
		PublicKey:   base64.RawStdEncoding.EncodeToString(publicKey),
		Signature:   base64.RawStdEncoding.EncodeToString(signature),
		GeneratedAt: time.Now().UTC(),
	}
	encoded, err := json.MarshalIndent(certificate, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return atomicWriteFile(output, encoded, 0o644)
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
	if certificate.Format != "worldbisect.causal-certificate.v1" {
		return VerificationResult{Valid: false, Error: "unsupported certificate format"}, nil
	}
	publicKeyBytes, err := base64.RawStdEncoding.DecodeString(certificate.PublicKey)
	if err != nil {
		return VerificationResult{Valid: false, Error: "invalid embedded public key"}, nil
	}
	if publicKeyPath != "" {
		trusted, err := readPublicKey(publicKeyPath)
		if err != nil {
			return VerificationResult{}, err
		}
		if !bytes.Equal(trusted, publicKeyBytes) {
			return VerificationResult{Valid: false, Error: "certificate public key is not trusted key"}, nil
		}
	}
	signature, err := base64.RawStdEncoding.DecodeString(certificate.Signature)
	if err != nil {
		return VerificationResult{Valid: false, Error: "invalid signature encoding"}, nil
	}
	payload, err := json.Marshal(certificate.Payload)
	if err != nil {
		return VerificationResult{}, err
	}
	valid := ed25519.Verify(ed25519.PublicKey(publicKeyBytes), payload, signature)
	result := VerificationResult{Valid: valid, AnalysisID: certificate.Payload.ID, Status: certificate.Payload.Status}
	if !valid {
		result.Error = "signature verification failed"
	}
	return result, nil
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

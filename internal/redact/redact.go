package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

var secretName = regexp.MustCompile(`(?i)(secret|token|password|passwd|api[_-]?key|private[_-]?key|authorization|cookie|credential|session)`)

func Environment(values map[string]string, key []byte) (map[string]string, []model.SecretEvidence) {
	result := make(map[string]string, len(values))
	var evidence []model.SecretEvidence
	for name, value := range values {
		if secretName.MatchString(name) {
			fingerprint := hmacFingerprint(key, value)
			result[name] = "redacted:hmac:" + fingerprint
			evidence = append(evidence, model.SecretEvidence{Name: name, Fingerprint: fingerprint})
			continue
		}
		result[name] = value
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Name < evidence[j].Name })
	return result, evidence
}

func hmacFingerprint(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

func LooksSecret(name string) bool {
	return secretName.MatchString(strings.TrimSpace(name))
}

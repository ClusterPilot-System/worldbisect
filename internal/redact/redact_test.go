package redact

import "testing"

func TestEnvironment(t *testing.T) {
	result, evidence := Environment(map[string]string{"MODE": "good", "API_TOKEN": "secret"}, []byte("01234567890123456789012345678901"))
	if result["MODE"] != "good" || result["API_TOKEN"] == "secret" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(evidence) != 1 || evidence[0].Fingerprint == "" {
		t.Fatal("secret evidence missing")
	}
}

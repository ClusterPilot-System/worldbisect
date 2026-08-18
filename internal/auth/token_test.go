package auth

import "testing"

func TestHashAndVerify(t *testing.T) {
	encoded := HashToken("secret")
	if encoded == "secret" || !VerifyToken("secret", encoded) {
		t.Fatal("token verification failed")
	}
	if VerifyToken("wrong", encoded) || VerifyToken("secret", "short") {
		t.Fatal("invalid token accepted")
	}
	if Fingerprint("secret") == "secret" {
		t.Fatal("raw token leaked as fingerprint")
	}
}

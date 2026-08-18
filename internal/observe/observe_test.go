package observe

import "testing"

func TestHostEvidence(t *testing.T) {
	host := Host("")
	if host.OS == "" || host.Arch == "" || host.MountDigest == "" {
		t.Fatalf("incomplete host evidence: %+v", host)
	}
}

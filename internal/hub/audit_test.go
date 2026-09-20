package hub

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAudit(t *testing.T) *AuditLog {
	t.Helper()
	a, err := OpenAudit(filepath.Join(t.TempDir(), "audit"), 7)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func auditFixture() AuditEvent {
	return AuditEvent{Actor: "alice", Kind: "user", Workspace: "alpha", CredentialID: "alice-reader", Action: "reports.list", Outcome: "200"}
}

func TestAuditDurabilityHeadVerificationAndWriterLock(t *testing.T) {
	a := testAudit(t)
	if err := a.Append(auditFixture()); err != nil {
		t.Fatal(err)
	}
	status := a.Status()
	if _, err := OpenAudit(a.dir, 7); err == nil {
		t.Fatal("concurrent audit writer accepted")
	}
	a.Close()
	verified, err := VerifyAudit(a.dir, status.HeadHash)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Sequence != 1 || verified.HeadHash != status.HeadHash {
		t.Fatal(verified)
	}
	if _, err := VerifyAudit(a.dir, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong external checkpoint accepted")
	}
}

func TestAuditDetectsModificationAndCompleteTailTruncation(t *testing.T) {
	for _, kind := range []string{"modified", "truncated", "missing", "uncommitted"} {
		t.Run(kind, func(t *testing.T) {
			a := testAudit(t)
			a.Append(auditFixture())
			a.Append(auditFixture())
			name := a.state.Segments[0].Name
			a.Close()
			path := filepath.Join(a.dir, name)
			b, _ := os.ReadFile(path)
			switch kind {
			case "modified":
				b = bytes.Replace(b, []byte(`"alice"`), []byte(`"mallory"`), 1)
			case "truncated":
				lines := bytes.Split(b, []byte("\n"))
				b = append(lines[0], '\n')
			case "missing":
				os.Remove(path)
			case "uncommitted":
				b = append(b, b...)
			}
			if kind != "missing" {
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := VerifyAudit(a.dir, ""); err == nil {
				t.Fatal("audit tampering accepted")
			}
		})
	}
}

func TestAuditRotationAndRetentionPreserveChainAnchor(t *testing.T) {
	a := testAudit(t)
	a.segmentBytes = 450
	a.maxSegments = 2
	now := time.Now().UTC()
	a.now = func() time.Time { return now }
	for i := 0; i < 8; i++ {
		if err := a.Append(auditFixture()); err != nil {
			t.Fatal(err)
		}
	}
	status := a.Status()
	if status.RetainedSegments > 2 || status.AnchorSequence == 0 || status.Sequence != 8 {
		t.Fatal(status)
	}
	a.Close()
	if _, err := VerifyAudit(a.dir, status.HeadHash); err != nil {
		t.Fatal(err)
	}
	var err error
	a, err = OpenAudit(a.dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.now = func() time.Time { return now.Add(8 * 24 * time.Hour) }
	if err := a.PurgeExpired(); err != nil {
		t.Fatal(err)
	}
	status = a.Status()
	if status.RetainedSegments != 0 || status.AnchorSequence != status.Sequence {
		t.Fatal(status)
	}
	if err := a.Append(auditFixture()); err != nil {
		t.Fatal(err)
	}
	status = a.Status()
	a.Close()
	if _, err := VerifyAudit(a.dir, status.HeadHash); err != nil {
		t.Fatal(err)
	}
}

func TestAuditRejectsUnsafeFilesAndOversizedMetadata(t *testing.T) {
	a := testAudit(t)
	event := auditFixture()
	event.Actor = strings.Repeat("a", 129)
	if err := a.Append(event); err == nil {
		t.Fatal("oversized audit field accepted")
	}
	a.Append(auditFixture())
	name := a.state.Segments[0].Name
	a.Close()
	os.Remove(filepath.Join(a.dir, name))
	os.Symlink("/etc/passwd", filepath.Join(a.dir, name))
	if _, err := VerifyAudit(a.dir, ""); err == nil {
		t.Fatal("audit symlink accepted")
	}
}

package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func TestExitOracle(t *testing.T) {
	spec, err := Parse("exit=0")
	if err != nil {
		t.Fatal(err)
	}
	result := Evaluate(spec, model.ProcessResult{ExitCode: 0}, t.TempDir())
	if !result.Passed {
		t.Fatal("exit oracle did not pass")
	}
}

func TestFileDigestRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	result := Evaluate(model.Oracle{Kind: "file_digest", File: "../secret", Digest: string(make([]byte, 64))}, model.ProcessResult{}, root)
	if result.Passed || result.Detail == "" {
		t.Fatal("traversal not rejected")
	}
}

func TestFileDigest(t *testing.T) {
	root := t.TempDir()
	content := []byte("ok\n")
	if err := os.WriteFile(filepath.Join(root, "result"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	result := Evaluate(model.Oracle{Kind: "file_digest", File: "result", Digest: hex.EncodeToString(sum[:])}, model.ProcessResult{}, root)
	if !result.Passed {
		t.Fatalf("result = %+v", result)
	}
}

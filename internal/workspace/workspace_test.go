package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestScanAndMaterialize(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "file"), []byte("content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir/file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Scan(root, dataStore, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := Materialize(destination, manifest, dataStore); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "dir", "file"))
	if err != nil || string(content) != "content" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	target, err := os.Readlink(filepath.Join(destination, "link"))
	if err != nil || target != "dir/file" {
		t.Fatalf("target=%q err=%v", target, err)
	}
}

func TestApplyRejectsTraversal(t *testing.T) {
	dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
	if err := Apply(t.TempDir(), "../escape", model.WorkspaceEntry{}, false, dataStore); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

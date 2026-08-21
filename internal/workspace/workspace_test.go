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

func TestApplyRejectsSymlinkEscape(t *testing.T) {
	dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
	root := t.TempDir()
	for _, target := range []string{"/etc/passwd", "../../outside", `..\outside`} {
		if err := Apply(root, "nested/link", model.WorkspaceEntry{Type: "symlink", Mode: 0o777, LinkTarget: target}, true, dataStore); err == nil {
			t.Fatalf("symlink escape accepted: %q", target)
		}
	}
}

func TestScanMarksEscapingSymlinkUnsupported(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("../outside", filepath.Join(root, "escape")); err != nil {
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
	if len(manifest.Entries) != 1 || manifest.Entries[0].Type != "unsupported" {
		t.Fatalf("escaping symlink was not bounded: %+v", manifest.Entries)
	}
	if err := Materialize(filepath.Join(t.TempDir(), "destination"), manifest, dataStore); err == nil {
		t.Fatal("unsupported escaping symlink was materialized")
	}
}

func TestApplyPreservesFileAndDirectoryModes(t *testing.T) {
	dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
	digest, err := dataStore.PutBlob([]byte("content"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := Apply(root, "private", model.WorkspaceEntry{Type: "dir", Mode: 0o750}, true, dataStore); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, "private/file", model.WorkspaceEntry{Type: "file", Mode: 0o640, BlobDigest: digest}, true, dataStore); err != nil {
		t.Fatal(err)
	}
	directoryInfo, _ := os.Stat(filepath.Join(root, "private"))
	fileInfo, _ := os.Stat(filepath.Join(root, "private", "file"))
	if directoryInfo.Mode().Perm() != 0o750 || fileInfo.Mode().Perm() != 0o640 {
		t.Fatalf("modes not preserved: directory=%o file=%o", directoryInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
}

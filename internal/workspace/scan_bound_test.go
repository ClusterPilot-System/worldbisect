package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestScanRootMatchesOrdinaryWorkspaceManifest(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "input.txt"), []byte("input"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested/input.txt", filepath.Join(directory, "input-link")); err != nil {
		t.Fatal(err)
	}
	data, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	root, err := OpenBoundRoot(handle)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ordinary, err := Scan(directory, data, 20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := ScanRoot(root, directory, data, 20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Digest != ordinary.Digest || bound.TotalBytes != ordinary.TotalBytes || bound.TotalFiles != ordinary.TotalFiles {
		t.Fatalf("bound manifest differs: %#v versus %#v", bound, ordinary)
	}
}

func TestScanRootRetainsSuppliedDirectoryAfterRename(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "workspace")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	root, err := OpenBoundRoot(handle)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(directory, filepath.Join(parent, "renamed-workspace")); err != nil {
		t.Fatal(err)
	}
	// A display label is metadata, even if a different directory now has that name.
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "other.txt"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ScanRoot(root, directory, data, 20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "input.txt" {
		t.Fatalf("scan did not retain supplied root: %#v", manifest)
	}
	b, err := data.GetBlob(manifest.Entries[0].BlobDigest)
	if err != nil || string(b) != "original" {
		t.Fatalf("unexpected supplied-root content %q: %v", b, err)
	}
}

func TestScanRootPreservesQuotasAndDescriptorValidation(t *testing.T) {
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "a"), []byte("12345"), 0600)
	os.WriteFile(filepath.Join(directory, "b"), []byte("67890"), 0600)
	data, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	root, err := OpenBoundRoot(handle)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := ScanRoot(root, directory, data, 1, 1024); err == nil {
		t.Fatal("file quota ignored")
	}
	if _, err := ScanRoot(root, directory, data, 10, 4); err == nil {
		t.Fatal("byte quota ignored")
	}
	file, err := os.Open(filepath.Join(directory, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if unexpected, err := OpenBoundRoot(file); err == nil {
		unexpected.Close()
		t.Fatal("ordinary file used as directory")
	}
	if _, err := OpenBoundRoot(nil); err == nil {
		t.Fatal("missing descriptor accepted")
	}
}

package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestDeterministicExportAndImport(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := dataStore.PutBlob([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	captureValue := &model.Capture{
		SchemaVersion: 3,
		ID:            "capture-example",
		CreatedAt:     time.Unix(1, 0).UTC(),
		Workspace: model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{
			Path: "file.txt", Type: "file", BlobDigest: blob,
		}}},
	}
	if err := dataStore.SaveCapture(captureValue); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.wcap")
	second := filepath.Join(t.TempDir(), "second.wcap")
	if err := ExportCapture(dataStore, captureValue.ID, first); err != nil {
		t.Fatal(err)
	}
	if err := ExportCapture(dataStore, captureValue.ID, second); err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("bundle is not deterministic")
	}
	importStore, err := store.Open(filepath.Join(t.TempDir(), "import"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := ImportBundle(importStore, first)
	if err != nil {
		t.Fatal(err)
	}
	if id != captureValue.ID {
		t.Fatalf("import ID = %s", id)
	}
	content, err := importStore.GetBlob(blob)
	if err != nil || string(content) != "content\n" {
		t.Fatalf("blob import failed: %q %v", content, err)
	}
}

func TestImportRejectsTraversalAndLinks(t *testing.T) {
	cases := []struct {
		name     string
		header   *tar.Header
		contents []byte
	}{
		{name: "traversal", header: &tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, contents: []byte("x")},
		{name: "absolute", header: &tar.Header{Name: "/escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, contents: []byte("x")},
		{name: "symlink", header: &tar.Header{Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{name: "device", header: &tar.Header{Name: "device", Mode: 0o600, Typeflag: tar.TypeChar}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bundle := filepath.Join(t.TempDir(), "bad.wcap")
			file, err := os.Create(bundle)
			if err != nil {
				t.Fatal(err)
			}
			gzipWriter := gzip.NewWriter(file)
			tarWriter := tar.NewWriter(gzipWriter)
			if err := tarWriter.WriteHeader(test.header); err != nil {
				t.Fatal(err)
			}
			if len(test.contents) > 0 {
				_, _ = tarWriter.Write(test.contents)
			}
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			_ = file.Close()
			dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
			if _, err := ImportBundle(dataStore, bundle); err == nil {
				t.Fatal("expected unsafe archive rejection")
			}
		})
	}
}

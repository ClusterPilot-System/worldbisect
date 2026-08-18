package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

const (
	bundleFormat       = "worldbisect.capture.v1"
	maxBundleBytes     = 1 << 30
	maxArchiveEntries  = 20000
	maxManifestBytes   = 4 << 20
	maxEntityBytes     = 64 << 20
	maxBlobPayloadSize = 256 << 20
)

type bundleManifest struct {
	Format      string         `json:"format"`
	CaptureID   string         `json:"capture_id"`
	CreatedAt   time.Time      `json:"created_at"`
	EntityPath  string         `json:"entity_path"`
	EntityHash  string         `json:"entity_hash"`
	BlobEntries []bundleBlob   `json:"blob_entries"`
}

type bundleBlob struct {
	Digest string `json:"digest"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
}

func ExportCapture(dataStore *store.Store, captureID, output string) error {
	captureValue, err := dataStore.LoadCapture(captureID)
	if err != nil {
		return err
	}
	entity, err := canonicalJSON(captureValue)
	if err != nil {
		return err
	}
	manifest := bundleManifest{
		Format:     bundleFormat,
		CaptureID:  captureID,
		CreatedAt:  time.Unix(0, 0).UTC(),
		EntityPath: "capture.json",
		EntityHash: digest(entity),
	}
	digests := collectDigests(captureValue)
	for _, value := range digests {
		content, err := dataStore.GetBlob(value)
		if err != nil {
			return err
		}
		manifest.BlobEntries = append(manifest.BlobEntries, bundleBlob{
			Digest: value,
			Path:   "blobs/" + value,
			Size:   int64(len(content)),
		})
	}
	manifestBytes, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	entries := []archiveEntry{{Name: "manifest.json", Content: manifestBytes}, {Name: "capture.json", Content: entity}}
	for _, item := range manifest.BlobEntries {
		content, err := dataStore.GetBlob(item.Digest)
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{Name: item.Path, Content: content})
	}
	return writeDeterministicTarGzip(output, entries)
}

func ImportBundle(dataStore *store.Store, bundlePath string) (string, error) {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxBundleBytes {
		return "", fmt.Errorf("bundle exceeds %d bytes", maxBundleBytes)
	}
	file, err := os.Open(bundlePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	limited := io.LimitReader(file, maxBundleBytes+1)
	gzipReader, err := gzip.NewReader(limited)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()

	entries := make(map[string][]byte)
	tarReader := tar.NewReader(gzipReader)
	for count := 0; ; count++ {
		if count >= maxArchiveEntries {
			return "", errors.New("archive entry limit exceeded")
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if err := validateArchiveHeader(header); err != nil {
			return "", err
		}
		if _, exists := entries[header.Name]; exists {
			return "", fmt.Errorf("duplicate archive entry %q", header.Name)
		}
		limit := int64(maxBlobPayloadSize)
		if header.Name == "manifest.json" {
			limit = maxManifestBytes
		} else if header.Name == "capture.json" {
			limit = maxEntityBytes
		}
		if header.Size > limit {
			return "", fmt.Errorf("entry %q exceeds limit", header.Name)
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, limit+1))
		if err != nil {
			return "", err
		}
		if int64(len(content)) > limit {
			return "", fmt.Errorf("entry %q exceeds limit", header.Name)
		}
		entries[header.Name] = content
	}

	manifestBytes, exists := entries["manifest.json"]
	if !exists {
		return "", errors.New("manifest missing")
	}
	var manifest bundleManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return "", fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.Format != bundleFormat || manifest.CaptureID == "" || manifest.EntityPath != "capture.json" {
		return "", errors.New("invalid bundle manifest")
	}
	entityBytes, exists := entries[manifest.EntityPath]
	if !exists || digest(entityBytes) != manifest.EntityHash {
		return "", errors.New("capture entity digest mismatch")
	}
	var captureValue model.Capture
	decoder = json.NewDecoder(bytes.NewReader(entityBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&captureValue); err != nil {
		return "", fmt.Errorf("decode capture: %w", err)
	}
	if captureValue.ID != manifest.CaptureID {
		return "", errors.New("capture ID does not match manifest")
	}
	allowed := map[string]bool{"manifest.json": true, "capture.json": true}
	for _, blob := range manifest.BlobEntries {
		if !validDigest(blob.Digest) || blob.Path != "blobs/"+blob.Digest || blob.Size < 0 || blob.Size > maxBlobPayloadSize {
			return "", errors.New("invalid blob manifest entry")
		}
		if allowed[blob.Path] {
			return "", fmt.Errorf("duplicate declared entry %q", blob.Path)
		}
		allowed[blob.Path] = true
		content, exists := entries[blob.Path]
		if !exists || int64(len(content)) != blob.Size || digest(content) != blob.Digest {
			return "", fmt.Errorf("blob %s failed validation", blob.Digest)
		}
	}
	for name := range entries {
		if !allowed[name] {
			return "", fmt.Errorf("unexpected archive entry %q", name)
		}
	}

	for _, blob := range manifest.BlobEntries {
		content := entries[blob.Path]
		stored, err := dataStore.PutBlob(content)
		if err != nil {
			return "", err
		}
		if stored != blob.Digest {
			return "", errors.New("stored blob digest mismatch")
		}
	}
	if err := dataStore.SaveCapture(&captureValue); err != nil {
		return "", err
	}
	if err := dataStore.AppendAudit("bundle-import", "capture", captureValue.ID, map[string]any{"path": filepath.Base(bundlePath)}); err != nil {
		return "", err
	}
	return captureValue.ID, nil
}

type archiveEntry struct {
	Name    string
	Content []byte
	Mode    int64
}

func writeDeterministicTarGzip(output string, entries []archiveEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	buffer := &bytes.Buffer{}
	gzipWriter, err := gzip.NewWriterLevel(buffer, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := validateArchiveName(entry.Name); err != nil {
			return err
		}
		mode := entry.Mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:       entry.Name,
			Mode:       mode,
			Size:       int64(len(entry.Content)),
			Typeflag:   tar.TypeReg,
			ModTime:    time.Unix(0, 0).UTC(),
			AccessTime: time.Unix(0, 0).UTC(),
			ChangeTime: time.Unix(0, 0).UTC(),
			Uid:        0,
			Gid:        0,
			Uname:      "",
			Gname:      "",
			Format:     tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(entry.Content); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	return atomicWriteFile(output, buffer.Bytes(), 0o644)
}

func validateArchiveHeader(header *tar.Header) error {
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
		return fmt.Errorf("unsupported archive entry type for %q", header.Name)
	}
	if header.Linkname != "" {
		return fmt.Errorf("archive links are not allowed: %q", header.Name)
	}
	return validateArchiveName(header.Name)
}

func validateArchiveName(name string) error {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean != name || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	return nil
}

func collectDigests(captureValue *model.Capture) []string {
	set := make(map[string]bool)
	for _, entry := range captureValue.Workspace.Entries {
		if validDigest(entry.BlobDigest) {
			set[entry.BlobDigest] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func canonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".worldbisect-artifact-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

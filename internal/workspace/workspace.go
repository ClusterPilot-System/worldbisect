package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func CanonicalRoot(root string) (string, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace root is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func Scan(root string, dataStore *store.Store, maxFiles int, maxBytes int64) (model.WorkspaceManifest, error) {
	root, err := CanonicalRoot(root)
	if err != nil {
		return model.WorkspaceManifest{}, err
	}
	manifest := model.WorkspaceManifest{Root: root}
	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if unsafeRelative(relative) {
			return fmt.Errorf("unsafe workspace path %q", relative)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		item := model.WorkspaceEntry{Path: relative, Mode: uint32(info.Mode()), Size: info.Size()}
		switch {
		case info.Mode().IsRegular():
			item.Type = "file"
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			contentHash := sha256.New()
			limited := io.LimitReader(file, maxBytes+1)
			content, err := io.ReadAll(limited)
			file.Close()
			if err != nil {
				return err
			}
			manifest.TotalBytes += int64(len(content))
			if manifest.TotalBytes > maxBytes {
				return errors.New("workspace byte quota exceeded")
			}
			_, _ = contentHash.Write(content)
			item.Digest = hex.EncodeToString(contentHash.Sum(nil))
			item.BlobDigest, err = dataStore.PutBlob(content)
			if err != nil {
				return err
			}
		case info.IsDir():
			item.Type = "dir"
		case info.Mode()&os.ModeSymlink != 0:
			item.Type = "symlink"
			item.LinkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
			item.Digest = digestString(item.LinkTarget)
			if err := validateLinkTarget(relative, item.LinkTarget); err != nil {
				// Preserve the observation for comparison, but make it an
				// explicit boundary instead of an intervenable factor.
				item.Type = "unsupported"
			}
		default:
			item.Type = "unsupported"
		}
		manifest.Entries = append(manifest.Entries, item)
		manifest.TotalFiles++
		if manifest.TotalFiles > maxFiles {
			return errors.New("workspace file quota exceeded")
		}
		return nil
	})
	if err != nil {
		return model.WorkspaceManifest{}, err
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	for _, item := range manifest.Entries {
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\x00%s\n", item.Path, item.Type, item.Mode, item.Digest, item.LinkTarget)
	}
	manifest.Digest = hex.EncodeToString(hash.Sum(nil))
	return manifest, nil
}

func Materialize(root string, manifest model.WorkspaceManifest, dataStore *store.Store) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	entries := append([]model.WorkspaceEntry(nil), manifest.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		leftDepth := strings.Count(entries[i].Path, "/")
		rightDepth := strings.Count(entries[j].Path, "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return entries[i].Path < entries[j].Path
	})
	for _, entry := range entries {
		if err := Apply(root, entry.Path, entry, true, dataStore); err != nil {
			return err
		}
	}
	return nil
}

func Apply(root, relative string, entry model.WorkspaceEntry, present bool, dataStore *store.Store) error {
	path, err := safeJoin(root, relative)
	if err != nil {
		return err
	}
	if present && entry.Type == "symlink" {
		if err := validateLinkTarget(relative, entry.LinkTarget); err != nil {
			return err
		}
	}
	if err := removeExisting(path); err != nil {
		return err
	}
	if !present {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(entry.Mode)
	switch entry.Type {
	case "dir":
		if err := os.MkdirAll(path, mode.Perm()); err != nil {
			return err
		}
		return os.Chmod(path, mode.Perm())
	case "symlink":
		if entry.LinkTarget == "" {
			return errors.New("symlink target missing")
		}
		return os.Symlink(entry.LinkTarget, path)
	case "file":
		content, err := dataStore.GetBlob(entry.BlobDigest)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, content, mode.Perm()); err != nil {
			return err
		}
		return os.Chmod(path, mode.Perm())
	default:
		return fmt.Errorf("unsupported workspace entry type %q", entry.Type)
	}
}

func safeJoin(root, relative string) (string, error) {
	if unsafeRelative(relative) {
		return "", errors.New("unsafe relative workspace path")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	relativeCheck, err := filepath.Rel(root, path)
	if err != nil || unsafeRelative(filepath.ToSlash(relativeCheck)) {
		return "", errors.New("workspace path escapes root")
	}
	return path, nil
}

func unsafeRelative(relative string) bool {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	return clean != relative || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../")
}

func validateLinkTarget(relative, target string) error {
	if target == "" || filepath.IsAbs(target) || strings.Contains(target, "\\") {
		return errors.New("symlink target must be a non-empty relative Unix path")
	}
	linkPath := filepath.ToSlash(filepath.Join(filepath.Dir(relative), target))
	clean := filepath.ToSlash(filepath.Clean(linkPath))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("symlink target escapes workspace")
	}
	return nil
}

func removeExisting(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

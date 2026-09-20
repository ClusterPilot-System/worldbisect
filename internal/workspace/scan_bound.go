package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

// OpenBoundRoot duplicates an already-open directory reference. The descriptor
// path is used only to construct os.Root, never resolved back to a pathname.
// The caller retains ownership of directory and closes the returned Root.
func OpenBoundRoot(directory *os.File) (*os.Root, error) {
	if directory == nil {
		return nil, errors.New("bound workspace directory is required")
	}
	expected, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	if !expected.IsDir() {
		return nil, errors.New("bound workspace is not a directory")
	}
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", directory.Fd()))
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, actual) {
		root.Close()
		return nil, errors.New("bound workspace directory identity changed")
	}
	return root, nil
}

// ScanRoot reads exclusively through an already-open directory root. displayRoot
// is descriptive metadata and is never used to open or canonicalize a file.
func ScanRoot(root *os.Root, displayRoot string, dataStore *store.Store, maxFiles int, maxBytes int64) (model.WorkspaceManifest, error) {
	if root == nil {
		return model.WorkspaceManifest{}, errors.New("workspace root handle is required")
	}
	manifest := model.WorkspaceManifest{Root: displayRoot}
	err := fs.WalkDir(boundScanFS{root: root, maxEntries: maxFiles}, ".", func(relative string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relative == "." {
			return nil
		}
		if unsafeRelative(relative) {
			return fmt.Errorf("unsafe workspace path %q", relative)
		}
		info, err := root.Lstat(filepath.FromSlash(relative))
		if err != nil {
			return err
		}
		item := model.WorkspaceEntry{Path: relative, Mode: uint32(info.Mode()), Size: info.Size()}
		switch {
		case info.Mode().IsRegular():
			item.Type = "file"
			if hardlinkCount(info) > 1 {
				item.Type = "unsupported"
				break
			}
			content, err := readStableRootFile(root, relative, info, maxBytes)
			if err != nil {
				return err
			}
			manifest.TotalBytes += int64(len(content))
			if manifest.TotalBytes > maxBytes {
				return errors.New("workspace byte quota exceeded")
			}
			sum := sha256.Sum256(content)
			item.Digest = hex.EncodeToString(sum[:])
			item.BlobDigest, err = dataStore.PutBlob(content)
			if err != nil {
				return err
			}
		case info.IsDir():
			item.Type = "dir"
		case info.Mode()&os.ModeSymlink != 0:
			item.Type = "symlink"
			item.LinkTarget, err = root.Readlink(filepath.FromSlash(relative))
			if err != nil {
				return err
			}
			item.Digest = digestString(item.LinkTarget)
			if err := validateLinkTarget(relative, item.LinkTarget); err != nil {
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
	hash := sha256.New()
	for _, item := range manifest.Entries {
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\x00%s\n", item.Path, item.Type, item.Mode, item.Digest, item.LinkTarget)
	}
	manifest.Digest = hex.EncodeToString(hash.Sum(nil))
	return manifest, nil
}

func readStableRootFile(root *os.Root, relative string, expected os.FileInfo, maxBytes int64) ([]byte, error) {
	file, err := root.OpenFile(filepath.FromSlash(relative), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || hardlinkCount(opened) > 1 || !sameFileState(expected, opened) {
		return nil, fmt.Errorf("workspace file changed before read: %q", relative)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	finished, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if hardlinkCount(finished) > 1 || !sameFileState(expected, finished) {
		return nil, fmt.Errorf("workspace file changed during read: %q", relative)
	}
	return content, nil
}

// WalkDir uses this bounded directory reader instead of Root.FS().ReadDir,
// whose generic read-only open does not require a directory before opening.
type boundScanFS struct {
	root       *os.Root
	maxEntries int
}

func (f boundScanFS) Open(name string) (fs.File, error) {
	return f.root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
}
func (f boundScanFS) Stat(name string) (fs.FileInfo, error) {
	return f.root.Lstat(filepath.FromSlash(name))
}
func (f boundScanFS) ReadDir(name string) ([]fs.DirEntry, error) {
	directory, err := f.root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(f.maxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > f.maxEntries {
		return nil, errors.New("workspace file quota exceeded")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

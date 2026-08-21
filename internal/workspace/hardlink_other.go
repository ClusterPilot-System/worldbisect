//go:build !linux

package workspace

import "os"

// Non-Linux release targets do not expose a portable link-count contract.
// The regular-file read still uses descriptor identity and post-read metadata
// checks; Linux additionally rejects files with multiple directory links.
func hardlinkCount(_ os.FileInfo) uint64 { return 1 }

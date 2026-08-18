package workspace

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func BenchmarkScan(b *testing.B) {
	root := b.TempDir()
	for index := 0; index < 1000; index++ {
		_ = os.WriteFile(filepath.Join(root, string(rune('a'+index%26))+"-file"+strconv.Itoa(index)), []byte("content"), 0o644)
	}
	dataStore, _ := store.Open(filepath.Join(b.TempDir(), "store"))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_, _ = Scan(root, dataStore, 2000, 1<<30)
	}
}

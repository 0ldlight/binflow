//go:build unix

package httpapi

import (
	"math"
	"syscall"
)

// filestoreSpace returns (total, free) bytes of the volume hosting the
// filestore — the /api/system/storage/info size fields (the live
// instance's usageSpace+freeSpace==totalSpace identity).
func filestoreSpace(dir string) (int64, int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	total := uint64(st.Bsize) * st.Blocks
	free := uint64(st.Bsize) * st.Bavail
	return clampBytes(total), clampBytes(free)
}

// clampBytes narrows a statfs product without lying: anything past
// MaxInt64 saturates instead of wrapping.
func clampBytes(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v) //nolint:gosec // G115: guarded by the saturation check above
}

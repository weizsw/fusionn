package subtitlellm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// PruneStats reports what a cache prune removed.
type PruneStats struct {
	Files int
	Dirs  int
	Bytes int64
}

// PruneCache removes every record in this package's cache namespace that no job
// has used for maxAge. Records are touched on every hit, so an old record is one
// nothing reads any more. The walk covers the flat namespace only, including
// the .tmp-* leftovers of interrupted writes, and touches no other directory
// under cacheDir. A maxAge of 0 or less keeps everything. Files that cannot be
// removed are kept and returned so the caller can warn; the walk continues.
func PruneCache(cacheDir string, maxAge time.Duration) (PruneStats, error) {
	stats := PruneStats{}
	if maxAge <= 0 || cacheDir == "" {
		return stats, nil
	}

	dir := filepath.Join(cacheDir, cacheNamespace)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		stats.Files++
		stats.Bytes += info.Size()
	}
	return stats, errors.Join(errs...)
}

// touchCacheRecord marks a served record as used, so expiry keeps what jobs
// still hit. A touch failure never affects the merge.
func touchCacheRecord(ctx context.Context, path string) {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		logger.Component("llm_alignment").Debug(ctx, "cache_touch_failed", "Failed to touch a cache record",
			zap.String("path", path), zap.Error(err))
	}
}

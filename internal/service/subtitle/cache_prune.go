package subtitle

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/subtitlellm"
	"github.com/fusionn/pkg/logger"
)

// cachePruneInterval is the minimum time between two cache prunes in one
// process.
const cachePruneInterval = 24 * time.Hour

// The prune guard is process-wide: MergerProcessor is rebuilt on every config
// reload, so the guard must not live in the processor.
var (
	cachePruneMu      sync.Mutex
	cachePruneLastRun time.Time
)

// maybePruneLLMCache drops the cache records and alignment artifacts that no
// job has used for subtitle.llm.cache_max_age_days. It runs synchronously at
// the start of an LLM merge, at most once per cachePruneInterval per process,
// and never fails the merge.
func maybePruneLLMCache(ctx context.Context, cfg config.LLMConfig) {
	if !cfg.CacheEnabled || cfg.CacheMaxAgeDays <= 0 || strings.TrimSpace(cfg.CacheDir) == "" {
		return
	}

	cachePruneMu.Lock()
	defer cachePruneMu.Unlock()
	now := time.Now()
	if !cachePruneLastRun.IsZero() && now.Sub(cachePruneLastRun) < cachePruneInterval {
		return
	}
	cachePruneLastRun = now

	log := logger.Component("merger")
	maxAge := time.Duration(cfg.CacheMaxAgeDays) * 24 * time.Hour

	// subtitlellm owns its namespace; the merger owns the artifact directory.
	stats, err := subtitlellm.PruneCache(cfg.CacheDir, maxAge)
	if err != nil {
		log.Warn(ctx, "cache_prune_failed", "Failed to prune the alignment cache", zap.Error(err))
	}
	artifacts, artifactErr := pruneLLMArtifacts(cfg.CacheDir, maxAge, cfg.MappingPath)
	if artifactErr != nil {
		log.Warn(ctx, "cache_prune_failed", "Failed to prune the alignment artifacts", zap.Error(artifactErr))
	}
	stats.Files += artifacts.Files
	stats.Dirs += artifacts.Dirs
	stats.Bytes += artifacts.Bytes

	if stats.Files+stats.Dirs > 0 {
		log.Info(ctx, "cache_pruned", "Pruned the LLM alignment cache",
			zap.Int("files", stats.Files), zap.Int("dirs", stats.Dirs), zap.Int64("bytes", stats.Bytes))
	}
}

// pruneLLMArtifacts removes every directory under <cacheDir>/llm-alignment that
// no job has written for maxAge, except the directory that holds the configured
// mapping_path: that mapping is an input of the current merge. Nothing else in
// cacheDir is touched.
func pruneLLMArtifacts(cacheDir string, maxAge time.Duration, mappingPath string) (subtitlellm.PruneStats, error) {
	stats := subtitlellm.PruneStats{}
	if maxAge <= 0 || cacheDir == "" {
		return stats, nil
	}

	root := filepath.Join(cacheDir, llmArtifactDirName)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}

	mappingAbs := ""
	if path := strings.TrimSpace(mappingPath); path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			mappingAbs = abs
		}
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		absPath, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// Absolute-path comparison: the artifact directory that contains the
		// configured mapping is kept.
		if mappingAbs != "" && strings.HasPrefix(mappingAbs, absPath+string(filepath.Separator)) {
			continue
		}
		newest, bytes, err := artifactDirStats(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if newest.After(cutoff) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, err)
			continue
		}
		stats.Dirs++
		stats.Bytes += bytes
	}
	return stats, errors.Join(errs...)
}

// artifactDirStats returns the newest modification time and the total file size
// inside an artifact directory.
func artifactDirStats(path string) (time.Time, int64, error) {
	var newest time.Time
	var bytes int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	return newest, bytes, err
}

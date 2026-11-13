package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// tempSweepMaxAge is how old a leftover fusionn temp entry must be to be
// swept: an English extract can be waiting for a fusionn-subs translation that
// is still queued in Redis across restarts.
const tempSweepMaxAge = 7 * 24 * time.Hour

// tempSweepPrefixes are the hidden names fusionn reserves in a media
// directory: the extraction files, both merge directories and the
// dual-language directory.
var tempSweepPrefixes = []string{".fusionn-extract-", ".fusionn-merge-", ".fusionn-dual-"}

// CleanupProcessor removes temporary files.
type CleanupProcessor struct{}

// NewCleanupProcessor creates a new cleanup processor.
func NewCleanupProcessor() *CleanupProcessor {
	return &CleanupProcessor{}
}

// Name returns the processor name.
func (p *CleanupProcessor) Name() string {
	return ProcessorNameCleanup
}

// ShouldRun determines if cleanup should run.
func (p *CleanupProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Always run cleanup
	return true
}

// Process removes temporary files.
func (p *CleanupProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("cleanup")
	log.Info(ctx, "cleanup_started", "Cleaning up temporary files")

	// Clean up extracted subtitles. LLM-mode extraction hands out unique hidden
	// temp paths; removing exactly those paths never touches existing sidecars.
	if pctx.EnglishSubPath != "" {
		if err := os.Remove(pctx.EnglishSubPath); err != nil && !os.IsNotExist(err) {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary file",
				zap.String("path", pctx.EnglishSubPath), zap.Error(err))
		}
	}

	if pctx.ChineseSubPath != "" {
		if err := os.Remove(pctx.ChineseSubPath); err != nil && !os.IsNotExist(err) {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary file",
				zap.String("path", pctx.ChineseSubPath), zap.Error(err))
		}
	}

	if pctx.TempMergeDir != "" {
		if err := os.RemoveAll(pctx.TempMergeDir); err != nil {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary merge directory",
				zap.String("path", pctx.TempMergeDir), zap.Error(err))
		}
	}

	log.Info(ctx, "cleanup_completed", "Cleanup completed")
	return nil
}

// cleanupFailedMerge removes the files a failed or panicking merge stage owns:
// always its temporary merge directory, and the input subtitle files only when
// fusionn created them for this job. A Bazarr-downloaded Chinese subtitle is
// kept, and a translation callback job removes no input file at all because
// those paths come from the HTTP payload and belong to the caller. Removal
// failures are a WARN and never change the job error.
func cleanupFailedMerge(ctx context.Context, pctx *ProcessingContext) {
	log := logger.Component("cleanup")

	if pctx.TempMergeDir != "" {
		if err := os.RemoveAll(pctx.TempMergeDir); err != nil {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary merge directory",
				zap.String("path", pctx.TempMergeDir), zap.Error(err))
		}
	}

	switch pctx.ChineseSubSource {
	case ChineseSourceExtracted:
		removeMergeInput(ctx, log, pctx.EnglishSubPath)
		removeMergeInput(ctx, log, pctx.ChineseSubPath)
	case ChineseSourceBazarr:
		removeMergeInput(ctx, log, pctx.EnglishSubPath)
	}
}

// removeMergeInput removes one temporary merge input file.
func removeMergeInput(ctx context.Context, log logger.Logger, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Warn(ctx, "cleanup_failed", "Failed to remove temporary file",
			zap.String("path", path), zap.Error(err))
	}
}

// sweepStaleTempFiles removes the temp entries an interrupted job left in the
// video's own directory. It lists that directory only, without recursion, and
// matches the fusionn-specific prefixes only, so a Bazarr download and the
// DuoSubs <video>.eng.srt/<video>.chs.srt sidecars are never touched. Errors
// are a WARN and never fail the job.
func sweepStaleTempFiles(ctx context.Context, videoPath string) {
	log := logger.Component("cleanup")
	// A relative video path has no media directory of its own; never run a
	// deletion pass against the process working directory.
	if !filepath.IsAbs(videoPath) {
		return
	}
	dir := filepath.Dir(videoPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn(ctx, "temp_sweep_failed", "Failed to list media directory for stale temp files",
			zap.String("path", dir), zap.Error(err))
		return
	}

	cutoff := time.Now().Add(-tempSweepMaxAge)
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !hasTempSweepPrefix(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Warn(ctx, "temp_sweep_failed", "Failed to inspect temp entry",
				zap.String("path", filepath.Join(dir, name)), zap.Error(err))
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		path := filepath.Join(dir, name)
		if entry.IsDir() {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			log.Warn(ctx, "temp_sweep_failed", "Failed to remove stale temp entry",
				zap.String("path", path), zap.Error(err))
			continue
		}
		removed++
	}

	if removed > 0 {
		log.Info(ctx, "temp_swept", "Removed stale temp files", zap.Int("count", removed))
	}
}

func hasTempSweepPrefix(name string) bool {
	for _, prefix := range tempSweepPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

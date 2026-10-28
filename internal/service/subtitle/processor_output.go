package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// OutputProcessor copies the merged subtitle to the final destination.
type OutputProcessor struct {
	outputSameDir bool
}

// NewOutputProcessor creates a new output processor.
func NewOutputProcessor(outputSameDir bool) *OutputProcessor {
	return &OutputProcessor{
		outputSameDir: outputSameDir,
	}
}

// Name returns the processor name.
func (p *OutputProcessor) Name() string {
	return ProcessorNameOutput
}

// ShouldRun determines if output should run.
func (p *OutputProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if we have a merged subtitle
	return pctx.MergedSubPath != ""
}

// Process copies merged subtitle to final destination.
func (p *OutputProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("output")
	if pctx.MergedSubPath == "" {
		return fmt.Errorf("merged subtitle path not set")
	}

	// Determine output path
	videoDir := filepath.Dir(pctx.VideoPath)
	videoBase := filepath.Base(pctx.VideoPath)
	videoExt := filepath.Ext(videoBase)
	videoName := videoBase[:len(videoBase)-len(videoExt)]

	finalPath := filepath.Join(videoDir, videoName+".zh-CN.ass")

	// Never overwrite the source media or either input subtitle.
	for _, input := range []string{pctx.VideoPath, pctx.EnglishSubPath, pctx.ChineseSubPath} {
		if input != "" && samePath(finalPath, input) {
			return fmt.Errorf("refusing to overwrite input file with merged subtitle: %s", finalPath)
		}
	}

	// If not outputting to same directory, use a configured output directory
	// For now, we always output to the same directory
	if !p.outputSameDir {
		log.Warn(ctx, "output_dir_unsupported", "Output to separate directory not yet implemented, using video directory")
	}

	log.Debug(ctx, "output_copy", "Copying merged subtitle", logger.Preview(pctx.MergedSubPath+" -> "+finalPath))

	// Copy file
	if err := copyFile(pctx.MergedSubPath, finalPath); err != nil {
		return fmt.Errorf("failed to copy merged subtitle: %w", err)
	}

	// Update context with final path
	pctx.MergedSubPath = finalPath

	log.Info(ctx, "output_saved", "Merged subtitle saved", zap.String("path", finalPath))
	return nil
}

// copyFile is a helper function to copy files
func copyFile(src, dst string) error {
	if samePath(src, dst) {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write destination: %w", err)
	}

	return nil
}

// samePath reports whether two paths refer to the same file, resolving
// directory symlinks while tolerating not-yet-created destinations.
func samePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil && leftAbs == rightAbs {
		return true
	}
	leftReal, leftErr := filepath.EvalSymlinks(left)
	rightReal, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && leftReal == rightReal
}

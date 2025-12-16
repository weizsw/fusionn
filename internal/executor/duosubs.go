package executor

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fusionn/pkg/logger"
)

// MergeSubtitles merges two subtitle files using DuoSubs CLI.
// primary: Path to primary subtitle file (Chinese)
// secondary: Path to secondary subtitle file (English)
// model: DuoSubs model name (e.g., "sentence-transformers/LaBSE")
// device: Device to run model on ("auto", "cuda", "cpu")
// timeoutMinutes: Maximum execution time in minutes
//
// Returns the path to the merged subtitle file (ASS format).
func MergeSubtitles(
	ctx context.Context,
	primary, secondary string,
	model, device string,
	timeoutMinutes int,
) (string, error) {
	// Create timeout context
	timeout := time.Duration(timeoutMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// DuoSubs outputs a ZIP file, we'll extract it later
	outputDir := filepath.Dir(primary)
	zipFile := filepath.Join(outputDir, "merged_bilingual.zip")

	// Build duosubs command
	// duosubs merge --primary <primary> --secondary <secondary> --output <output> --model <model> --device <device>
	args := []string{
		"merge",
		"--primary", primary,
		"--secondary", secondary,
		"--output", zipFile,
		"--model", model,
		"--device", device,
	}

	logger.Infof("Executing DuoSubs: duosubs %v", args)

	cmd := exec.CommandContext(ctx, "duosubs", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("duosubs merge timed out after %d minutes", timeoutMinutes)
		}
		return "", fmt.Errorf("duosubs merge failed: %w", err)
	}

	// Verify ZIP file exists
	if _, err := os.Stat(zipFile); err != nil {
		return "", fmt.Errorf("duosubs output ZIP not created: %w", err)
	}

	logger.Infof("📦 DuoSubs output ZIP: %s", zipFile)

	// Extract the merged subtitle from ZIP
	extractedFile, err := extractSubtitleFromZip(zipFile, outputDir)
	if err != nil {
		return "", fmt.Errorf("failed to extract subtitle from ZIP: %w", err)
	}

	// Clean up ZIP file
	if err := os.Remove(zipFile); err != nil {
		logger.Warnf("Failed to remove ZIP file %s: %v", zipFile, err)
	}

	logger.Infof("✅ DuoSubs merge completed: %s", extractedFile)
	return extractedFile, nil
}

// extractSubtitleFromZip extracts the combined subtitle file from DuoSubs ZIP output.
// DuoSubs creates a ZIP with 3 files: *_combined.ass, *_primary.ass, *_secondary.ass
// We want the _combined.ass file which contains the bilingual merged subtitle.
func extractSubtitleFromZip(zipPath, destDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open ZIP: %w", err)
	}
	defer r.Close()

	// Find and extract the *_combined.ass file
	for _, f := range r.File {
		fileName := strings.ToLower(f.Name)
		if strings.HasSuffix(fileName, "_combined.ass") {
			// Extract to destination directory
			destPath := filepath.Join(destDir, filepath.Base(f.Name))

			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("failed to open file in ZIP: %w", err)
			}

			outFile, err := os.Create(destPath)
			if err != nil {
				rc.Close()
				return "", fmt.Errorf("failed to create output file: %w", err)
			}

			_, copyErr := io.Copy(outFile, rc)
			rc.Close()
			outFile.Close()

			if copyErr != nil {
				return "", fmt.Errorf("failed to extract file: %w", copyErr)
			}

			logger.Infof("📤 Extracted combined subtitle: %s", destPath)
			return destPath, nil
		}
	}

	return "", fmt.Errorf("no _combined.ass file found in ZIP archive (expected DuoSubs output format)")
}

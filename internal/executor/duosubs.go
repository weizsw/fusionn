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

// DuoSubsConfig holds DuoSubs execution parameters.
type DuoSubsConfig struct {
	Model   string
	Device  string
	Timeout time.Duration
}

// MergeDuoSubs runs DuoSubs to merge English and Chinese subtitles into bilingual ASS.
// DuoSubs creates a zip file containing three ASS files:
// - {basename}_combined.ass (the merged bilingual subtitle)
// - {basename}_primary.ass
// - {basename}_secondary.ass
//
// This function extracts the zip and returns the path to the combined ASS file.
func MergeDuoSubs(ctx context.Context, chinesePath, englishPath, outputDir string, cfg DuoSubsConfig) (string, error) {
	logger.Infof("Running DuoSubs: primary=%s, secondary=%s, out=%s", chinesePath, englishPath, outputDir)

	// Determine basename for output files (use Chinese subtitle filename as base)
	basename := filepath.Base(chinesePath)
	basename = strings.TrimSuffix(basename, filepath.Ext(basename))

	// Build DuoSubs command
	// duosubs merge -p <chinese> -s <english> --output-dir <dir> --output-name <basename>
	args := []string{
		"merge",
		"-p", chinesePath,      // Primary language (Chinese)
		"-s", englishPath,      // Secondary language (English)
		"--output-dir", outputDir,
		"--output-name", basename,
	}

	// Add optional model parameter (use DuoSubs default if empty)
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}

	// Add device parameter
	if cfg.Device != "" {
		args = append(args, "--device", cfg.Device)
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Execute DuoSubs
	cmd := exec.CommandContext(timeoutCtx, "duosubs", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check if it was a timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("duosubs timed out after %v", cfg.Timeout)
		}
		return "", fmt.Errorf("duosubs failed: %w\nOutput: %s", err, string(output))
	}

	logger.Debugf("DuoSubs output: %s", string(output))

	// DuoSubs creates a zip file at outputDir/basename.zip
	zipPath := filepath.Join(outputDir, basename+".zip")

	// Verify zip was created
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		return "", fmt.Errorf("duosubs did not create expected zip: %s", zipPath)
	}

	// Extract zip to outputDir
	if err := extractZip(zipPath, outputDir); err != nil {
		return "", fmt.Errorf("failed to extract zip: %w", err)
	}

	// Clean up zip file
	if err := os.Remove(zipPath); err != nil {
		logger.Warnf("Failed to remove zip file %s: %v", zipPath, err)
	}

	// Locate the combined ASS file
	combinedPath := filepath.Join(outputDir, basename+"_combined.ass")
	if _, err := os.Stat(combinedPath); os.IsNotExist(err) {
		return "", fmt.Errorf("combined file not found in zip: %s", combinedPath)
	}

	logger.Infof("✅ DuoSubs completed: %s", combinedPath)
	return combinedPath, nil
}

// extractZip extracts a zip file to the specified destination directory.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		// Construct destination path
		fpath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			// Create directory
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Extract file
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open file in zip: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}

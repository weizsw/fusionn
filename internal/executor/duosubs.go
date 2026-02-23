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

	"github.com/fusionn/internal/client/duosubs"
	"github.com/fusionn/pkg/logger"
)

// DuoSubsMode determines how DuoSubs is executed
type DuoSubsMode string

const (
	DuoSubsModeLocal DuoSubsMode = "local" // Execute local binary
	DuoSubsModeHTTP  DuoSubsMode = "http"  // Call HTTP service
)

// DuoSubsConfig holds DuoSubs execution parameters.
type DuoSubsConfig struct {
	Mode    DuoSubsMode
	Model   string
	Device  string
	Timeout time.Duration

	// HTTP mode settings
	HTTPURL             string
	HTTPContainerPrefix string
	HTTPHostPrefix      string

	// Internal: set by NewDuoSubsExecutor
	httpClient *duosubs.HTTPClient
}

// NewDuoSubsExecutor creates a configured DuoSubs executor
func NewDuoSubsExecutor(cfg DuoSubsConfig) (*DuoSubsConfig, error) {
	if cfg.Mode == "" {
		cfg.Mode = DuoSubsModeLocal // Default to local
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Minute
	}

	// Initialize HTTP client if in HTTP mode
	if cfg.Mode == DuoSubsModeHTTP {
		if cfg.HTTPURL == "" {
			return nil, fmt.Errorf("http mode requires HTTPURL")
		}
		if cfg.HTTPContainerPrefix == "" || cfg.HTTPHostPrefix == "" {
			return nil, fmt.Errorf("http mode requires HTTPContainerPrefix and HTTPHostPrefix")
		}

		cfg.httpClient = duosubs.NewHTTPClient(duosubs.HTTPConfig{
			BaseURL:         cfg.HTTPURL,
			ContainerPrefix: cfg.HTTPContainerPrefix,
			HostPrefix:      cfg.HTTPHostPrefix,
			Timeout:         cfg.Timeout,
		})

		// Health check
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cfg.httpClient.HealthCheck(ctx); err != nil {
			logger.Warnf("DuoSubs HTTP service health check failed: %v", err)
		} else {
			logger.Info("✅ DuoSubs HTTP service is healthy")
		}
	}

	return &cfg, nil
}

// MergeDuoSubs runs DuoSubs to merge English and Chinese subtitles into bilingual ASS.
func MergeDuoSubs(ctx context.Context, chinesePath, englishPath, outputDir string, cfg DuoSubsConfig) (string, error) {
	if cfg.Mode == DuoSubsModeHTTP {
		return mergeDuoSubsHTTP(ctx, chinesePath, englishPath, outputDir, cfg)
	}
	return mergeDuoSubsLocal(ctx, chinesePath, englishPath, outputDir, cfg)
}

// mergeDuoSubsHTTP calls the HTTP service
func mergeDuoSubsHTTP(ctx context.Context, chinesePath, englishPath, outputDir string, cfg DuoSubsConfig) (string, error) {
	log := logger.Indent()
	log.Infof("Calling DuoSubs HTTP service: %s", cfg.HTTPURL)
	log.Infof("Primary (Chinese): %s", chinesePath)
	log.Infof("Secondary (English): %s", englishPath)

	if cfg.httpClient == nil {
		return "", fmt.Errorf("http client not initialized")
	}

	outputPath, err := cfg.httpClient.Merge(ctx, chinesePath, englishPath, outputDir)
	if err != nil {
		return "", fmt.Errorf("http merge failed: %w", err)
	}

	log.Infof("✅ DuoSubs HTTP merge completed: %s", outputPath)
	return outputPath, nil
}

// mergeDuoSubsLocal executes local binary
// DuoSubs creates a zip file containing three ASS files:
// - {basename}_combined.ass (the merged bilingual subtitle)
// - {basename}_primary.ass
// - {basename}_secondary.ass
//
// This function extracts the zip and returns the path to the combined ASS file.
func mergeDuoSubsLocal(ctx context.Context, chinesePath, englishPath, outputDir string, cfg DuoSubsConfig) (string, error) {
	log := logger.Indent()
	log.Infof("Running DuoSubs: primary=%s, secondary=%s, out=%s", chinesePath, englishPath, outputDir)

	// Determine basename for output files (use Chinese subtitle filename as base)
	basename := filepath.Base(chinesePath)
	basename = strings.TrimSuffix(basename, filepath.Ext(basename))

	// Build DuoSubs command
	// duosubs merge -p <chinese> -s <english> --output-dir <dir> --output-name <basename>
	args := []string{
		"merge",
		"-p", chinesePath, // Primary language (Chinese)
		"-s", englishPath, // Secondary language (English)
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

	// Execute DuoSubs with streaming output
	cmd := exec.CommandContext(timeoutCtx, "duosubs", args...)

	// Disable Python output buffering for real-time progress
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	// Stream stdout/stderr to logs in real-time
	cmd.Stdout = logger.NewInfoWriter()
	cmd.Stderr = logger.NewInfoWriter()

	log.Info("🚀 Starting DuoSubs (this may take a while on first run to download models...)")
	err := cmd.Run()
	if err != nil {
		// Check if it was a timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("duosubs timed out after %v", cfg.Timeout)
		}
		return "", fmt.Errorf("duosubs failed: %w", err)
	}

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
		log.Warnf("Failed to remove zip file %s: %v", zipPath, err)
	}

	// Locate the combined ASS file
	combinedPath := filepath.Join(outputDir, basename+"_combined.ass")
	if _, err := os.Stat(combinedPath); os.IsNotExist(err) {
		return "", fmt.Errorf("combined file not found in zip: %s", combinedPath)
	}

	log.Infof("✅ DuoSubs completed: %s", combinedPath)
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

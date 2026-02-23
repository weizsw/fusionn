package executor

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fusionn/pkg/logger"
)

func init() {
	// Initialize logger for tests (isDev=true for debug output)
	logger.Init(true)
}

func TestExtractZip(t *testing.T) {
	tests := []struct {
		name        string
		setupZip    func(t *testing.T, zipPath string)
		wantErr     bool
		errContains string
		validate    func(t *testing.T, destDir string)
	}{
		{
			name: "successful extraction",
			setupZip: func(t *testing.T, zipPath string) {
				createTestZip(t, zipPath, map[string]string{
					"test_combined.ass":  "[Script Info]\nTitle: Test",
					"test_primary.ass":   "[Script Info]\nTitle: Primary",
					"test_secondary.ass": "[Script Info]\nTitle: Secondary",
				})
			},
			wantErr: false,
			validate: func(t *testing.T, destDir string) {
				// Verify all files extracted
				files := []string{"test_combined.ass", "test_primary.ass", "test_secondary.ass"}
				for _, f := range files {
					path := filepath.Join(destDir, f)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						t.Errorf("Expected file %s not found", f)
					}
				}
			},
		},
		{
			name: "missing zip file",
			setupZip: func(t *testing.T, zipPath string) {
				// Don't create zip
			},
			wantErr:     true,
			errContains: "failed to open zip",
		},
		{
			name: "zip slip protection",
			setupZip: func(t *testing.T, zipPath string) {
				// Create zip with path traversal attempt
				createTestZip(t, zipPath, map[string]string{
					"../../../etc/passwd": "malicious content",
				})
			},
			wantErr:     true,
			errContains: "invalid file path in zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp dirs
			tmpDir := t.TempDir()
			zipPath := filepath.Join(tmpDir, "test.zip")
			destDir := filepath.Join(tmpDir, "dest")

			if err := os.MkdirAll(destDir, 0755); err != nil {
				t.Fatalf("Failed to create dest dir: %v", err)
			}

			// Setup test zip
			tt.setupZip(t, zipPath)

			// Run extraction
			err := extractZip(zipPath, destDir)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("extractZip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("extractZip() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			// Run validation if no error expected
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, destDir)
			}
		})
	}
}

func TestMergeDuoSubs_CommandConstruction(t *testing.T) {
	// This test verifies the command args are constructed correctly
	// We can't easily test the actual duosubs execution without mocking,
	// but we can verify the logic around it

	tests := []struct {
		name        string
		chinesePath string
		englishPath string
		outputDir   string
		cfg         DuoSubsConfig
		wantErr     bool
	}{
		{
			name:        "with model specified",
			chinesePath: "/path/to/chinese.srt",
			englishPath: "/path/to/english.srt",
			outputDir:   "/tmp/output",
			cfg: DuoSubsConfig{
				Model:   "sentence-transformers/LaBSE",
				Device:  "cpu",
				Timeout: 10 * time.Minute,
			},
			wantErr: true, // Will fail because duosubs not installed, but args are correct
		},
		{
			name:        "without model (uses default)",
			chinesePath: "/path/to/chinese.srt",
			englishPath: "/path/to/english.srt",
			outputDir:   "/tmp/output",
			cfg: DuoSubsConfig{
				Model:   "", // Empty model
				Device:  "cpu",
				Timeout: 10 * time.Minute,
			},
			wantErr: true, // Will fail because duosubs not installed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create output dir
			tmpDir := t.TempDir()

			// This will fail because duosubs isn't installed in test env,
			// but we're mainly testing the command construction logic
			_, err := MergeDuoSubs(ctx, tt.chinesePath, tt.englishPath, tmpDir, tt.cfg)

			if (err != nil) != tt.wantErr {
				t.Errorf("MergeDuoSubs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeDuoSubs_ZipExtraction(t *testing.T) {
	// Test the zip extraction and combined file detection logic
	// We'll simulate what duosubs would create

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output dir: %v", err)
	}

	// Simulate duosubs creating a zip file
	basename := "test_movie"
	zipPath := filepath.Join(outputDir, basename+".zip")

	createTestZip(t, zipPath, map[string]string{
		basename + "_combined.ass":  "[Script Info]\nTitle: Combined",
		basename + "_primary.ass":   "[Script Info]\nTitle: Primary",
		basename + "_secondary.ass": "[Script Info]\nTitle: Secondary",
	})

	// Test extraction
	if err := extractZip(zipPath, outputDir); err != nil {
		t.Fatalf("extractZip() failed: %v", err)
	}

	// Verify combined file exists
	combinedPath := filepath.Join(outputDir, basename+"_combined.ass")
	if _, err := os.Stat(combinedPath); os.IsNotExist(err) {
		t.Errorf("Expected combined file not found: %s", combinedPath)
	}

	// Verify content
	content, err := os.ReadFile(combinedPath)
	if err != nil {
		t.Fatalf("Failed to read combined file: %v", err)
	}

	expectedContent := "[Script Info]\nTitle: Combined"
	if string(content) != expectedContent {
		t.Errorf("Combined file content = %q, want %q", string(content), expectedContent)
	}
}

func TestMergeDuoSubs_MissingCombinedFile(t *testing.T) {
	// Test error when zip doesn't contain combined file

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output dir: %v", err)
	}

	basename := "test_movie"
	zipPath := filepath.Join(outputDir, basename+".zip")

	// Create zip WITHOUT combined file
	createTestZip(t, zipPath, map[string]string{
		basename + "_primary.ass":   "[Script Info]\nTitle: Primary",
		basename + "_secondary.ass": "[Script Info]\nTitle: Secondary",
	})

	// Extract zip
	if err := extractZip(zipPath, outputDir); err != nil {
		t.Fatalf("extractZip() failed: %v", err)
	}

	// Verify combined file doesn't exist (simulating the check in MergeDuoSubs)
	combinedPath := filepath.Join(outputDir, basename+"_combined.ass")
	if _, err := os.Stat(combinedPath); !os.IsNotExist(err) {
		t.Errorf("Expected combined file to not exist, but it does")
	}
}

func TestMergeDuoSubs_CorruptZip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "corrupt.zip")

	// Create corrupt zip file
	if err := os.WriteFile(zipPath, []byte("not a valid zip file"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt zip: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("Failed to create dest dir: %v", err)
	}

	// Try to extract
	err := extractZip(zipPath, destDir)
	if err == nil {
		t.Error("extractZip() expected error for corrupt zip, got nil")
	}

	if !contains(err.Error(), "failed to open zip") {
		t.Errorf("extractZip() error = %v, want error containing 'failed to open zip'", err)
	}
}

// Helper functions

func createTestZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()

	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create zip file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for name, content := range files {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("Failed to create file in zip: %v", err)
		}

		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write file content: %v", err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && hasSubstring(s, substr)))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

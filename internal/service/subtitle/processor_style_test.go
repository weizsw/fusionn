package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init(true)
}

func TestStyleProcessor(t *testing.T) {
	// Create temp directory for test files
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		assContent  string
		config      config.ASSStyleConfig
		shouldStyle bool
		wantErr     bool
	}{
		{
			name: "Valid ASS file with styling enabled",
			assContent: `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,16

[Events]
Format: Layer, Start, End, Style, Text
Dialog: 0,0:00:00.00,0:00:05.00,Default,Test subtitle
`,
			config: config.ASSStyleConfig{
				Enabled:        true,
				PrimaryFont:    "WenQuanYi Micro Hei",
				PrimarySize:    20,
				PrimaryColor:   "&H00c8c8c8",
				SecondaryFont:  "WenQuanYi Micro Hei",
				SecondarySize:  13,
				SecondaryColor: "&H0010b8ff",
				Bold:           true,
				Outline:        0.5,
				Shadow:         0.5,
				MarginV:        5,
			},
			shouldStyle: true,
			wantErr:     false,
		},
		{
			name: "Styling disabled",
			assContent: `[Script Info]
[V4+ Styles]
[Events]
Dialog: test
`,
			config: config.ASSStyleConfig{
				Enabled: false,
			},
			shouldStyle: false,
			wantErr:     false,
		},
		{
			name: "Invalid ASS file (missing Events)",
			assContent: `[Script Info]
[V4+ Styles]
No events here
`,
			config: config.ASSStyleConfig{
				Enabled: true,
			},
			shouldStyle: true,
			wantErr:     false, // Should not error, just skip styling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test ASS file
			assFile := filepath.Join(tempDir, "test.ass")
			if err := os.WriteFile(assFile, []byte(tt.assContent), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Create processor
			processor := NewStyleProcessor(tt.config)

			// Create context
			pctx := &ProcessingContext{
				MergedSubPath: assFile,
			}

			// Test ShouldRun
			if processor.ShouldRun(pctx) != tt.shouldStyle {
				t.Errorf("ShouldRun() = %v, want %v", processor.ShouldRun(pctx), tt.shouldStyle)
			}

			if !tt.shouldStyle {
				return // Skip processing test
			}

			// Process
			err := processor.Process(context.Background(), pctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Read result
			result, err := os.ReadFile(assFile)
			if err != nil {
				t.Fatalf("Failed to read result file: %v", err)
			}

			resultStr := string(result)

			// Verify results if styling should have happened
			if strings.Contains(tt.assContent, "[Events]") {
				// Should have custom Script Info
				if !strings.Contains(resultStr, "WrapStyle: 0") {
					t.Error("Result missing custom Script Info")
				}

				// Should have custom styles
				if !strings.Contains(resultStr, "Style: Default,WenQuanYi Micro Hei,20") {
					t.Error("Result missing Default style")
				}
				if !strings.Contains(resultStr, "Style: Default_1,WenQuanYi Micro Hei,13") {
					t.Error("Result missing Default_1 style")
				}

				// Should preserve Events section
				if !strings.Contains(resultStr, "[Events]") {
					t.Error("Result missing Events section")
				}
				if strings.Contains(tt.assContent, "Test subtitle") {
					if !strings.Contains(resultStr, "Test subtitle") {
						t.Error("Result missing subtitle content")
					}
				}
			}
		})
	}
}

func TestGenerateStyles(t *testing.T) {
	cfg := config.ASSStyleConfig{
		PrimaryFont:    "Arial",
		PrimarySize:    20,
		PrimaryColor:   "&H00FFFFFF",
		SecondaryFont:  "Arial",
		SecondarySize:  13,
		SecondaryColor: "&H00FF0000",
		Bold:           true,
		Outline:        0.5,
		Shadow:         0.5,
		MarginV:        5,
	}

	result := GenerateStyles(cfg)

	// Check format line
	if !strings.Contains(result, "[V4+ Styles]") {
		t.Error("Missing [V4+ Styles] header")
	}

	// Check Format line
	if !strings.Contains(result, "Format: Name, Fontname, Fontsize") {
		t.Error("Missing Format line")
	}

	// Check Default style
	if !strings.Contains(result, "Style: Default,Arial,20,&H00FFFFFF") {
		t.Error("Missing or incorrect Default style")
	}

	// Check Default_1 style
	if !strings.Contains(result, "Style: Default_1,Arial,13,&H00FF0000") {
		t.Error("Missing or incorrect Default_1 style")
	}

	// Check bold flag (should be 1)
	if !strings.Contains(result, ",1,0,0,0,") { // bold=1, italic=0, underline=0, strikeout=0
		t.Error("Bold flag not set correctly")
	}
}

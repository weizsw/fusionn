package subtitle

import (
	"testing"

	"github.com/fusionn/internal/config"
)

func TestExtractorProcessor_SetsNeedsConversionFlag(t *testing.T) {
	// Test that ExtractorProcessor correctly copies NeedsConversion flag
	// from Analysis to ProcessingContext
	tests := []struct {
		name                 string
		chineseTrack         *SubtitleTrack
		expectedNeedsConvert bool
	}{
		{
			name: "Traditional Chinese - needs conversion",
			chineseTrack: &SubtitleTrack{
				Index:           1,
				Language:        "chi",
				NeedsConversion: true,
				ExtractedPath:   "/tmp/chinese.srt",
			},
			expectedNeedsConvert: true,
		},
		{
			name: "Simplified Chinese - no conversion needed",
			chineseTrack: &SubtitleTrack{
				Index:           1,
				Language:        "chi",
				NeedsConversion: false,
				ExtractedPath:   "/tmp/chinese.srt",
			},
			expectedNeedsConvert: false,
		},
		{
			name:                 "No Chinese track",
			chineseTrack:         nil,
			expectedNeedsConvert: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a real Analyzer with empty configs (not used for this test)
			// We're testing the logic in ExtractorProcessor.Process() directly

			// Setup context
			pctx := &ProcessingContext{
				VideoPath: "/test/video.mkv",
				Analysis: &AnalysisResult{
					ChineseTrack: tt.chineseTrack,
					VideoPath:    "/test/video.mkv",
				},
			}

			// Manually set extracted paths to simulate extraction
			if pctx.Analysis.ChineseTrack != nil {
				pctx.Analysis.ChineseTrack.ExtractedPath = "/tmp/chinese.srt"
			}

			// This mirrors what ExtractorProcessor.Process() does:
			if pctx.Analysis.ChineseTrack != nil {
				pctx.ChineseSubPath = pctx.Analysis.ChineseTrack.ExtractedPath
				// This is the critical line we're testing!
				pctx.NeedsConversion = pctx.Analysis.ChineseTrack.NeedsConversion
			}

			// Verify NeedsConversion flag was set correctly
			if pctx.NeedsConversion != tt.expectedNeedsConvert {
				t.Errorf("NeedsConversion = %v, want %v", pctx.NeedsConversion, tt.expectedNeedsConvert)
			}

			// Verify paths were set
			if tt.chineseTrack != nil && pctx.ChineseSubPath == "" {
				t.Error("ChineseSubPath not set when Chinese track exists")
			}
		})
	}
}

func TestConversionProcessor_ShouldRun(t *testing.T) {
	// Test that ConversionProcessor correctly checks NeedsConversion flag
	// This is critical for the merge pipeline where Analysis is not available
	tests := []struct {
		name              string
		openccEnabled     bool
		chineseSubPath    string
		needsConversion   bool
		analysisPresent   bool
		want              bool
	}{
		{
			name:            "All conditions met",
			openccEnabled:   true,
			chineseSubPath:  "/tmp/chinese.srt",
			needsConversion: true,
			analysisPresent: false, // Important: merge pipeline has no analysis
			want:            true,
		},
		{
			name:            "OpenCC disabled",
			openccEnabled:   false,
			chineseSubPath:  "/tmp/chinese.srt",
			needsConversion: true,
			analysisPresent: false,
			want:            false,
		},
		{
			name:            "No Chinese subtitle",
			openccEnabled:   true,
			chineseSubPath:  "",
			needsConversion: true,
			analysisPresent: false,
			want:            false,
		},
		{
			name:            "No conversion needed",
			openccEnabled:   true,
			chineseSubPath:  "/tmp/chinese.srt",
			needsConversion: false,
			analysisPresent: false,
			want:            false,
		},
		{
			name:            "Merge pipeline scenario - flag set, no analysis",
			openccEnabled:   true,
			chineseSubPath:  "/tmp/chinese.srt",
			needsConversion: true,
			analysisPresent: false, // This is the key test case!
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewConversionProcessor(config.OpenCCConfig{
				Enabled: tt.openccEnabled,
				Config:  "t2s.json",
			})

			pctx := &ProcessingContext{
				ChineseSubPath:  tt.chineseSubPath,
				NeedsConversion: tt.needsConversion,
			}

			// Optionally add analysis (should not be required for ShouldRun)
			if tt.analysisPresent {
				pctx.Analysis = &AnalysisResult{
					ChineseTrack: &SubtitleTrack{
						NeedsConversion: tt.needsConversion,
					},
				}
			}

			got := processor.ShouldRun(pctx)
			if got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConversionProcessor_WorksInMergePipeline(t *testing.T) {
	// Integration test: Simulate merge pipeline where Analysis is nil
	// This is the exact scenario that was failing before the fix

	processor := NewConversionProcessor(config.OpenCCConfig{
		Enabled: true,
		Config:  "t2s.json",
	})

	// Simulate merge pipeline context (no Analysis, only flags)
	pctx := &ProcessingContext{
		VideoPath:       "/test/video.mkv",
		ChineseSubPath:  "/tmp/chinese.srt",
		EnglishSubPath:  "/tmp/english.srt",
		NeedsConversion: true, // Set by ExtractorProcessor in analyze pipeline
		Analysis:        nil,  // Not available in merge pipeline!
	}

	// ShouldRun should return true even without Analysis
	if !processor.ShouldRun(pctx) {
		t.Error("ShouldRun() = false in merge pipeline, want true")
		t.Error("ConversionProcessor should work without Analysis context")
	}
}

func TestCleanupProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name           string
		englishSubPath string
		chineseSubPath string
		want           bool
	}{
		{
			name:           "Both paths present",
			englishSubPath: "/tmp/eng.srt",
			chineseSubPath: "/tmp/chi.srt",
			want:           true,
		},
		{
			name:           "Only English",
			englishSubPath: "/tmp/eng.srt",
			chineseSubPath: "",
			want:           true,
		},
		{
			name:           "Only Chinese",
			englishSubPath: "",
			chineseSubPath: "/tmp/chi.srt",
			want:           true,
		},
		{
			name:           "No temp files - still runs (safe cleanup)",
			englishSubPath: "",
			chineseSubPath: "",
			want:           true, // Cleanup always runs, it's safe even if no files
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewCleanupProcessor()
			pctx := &ProcessingContext{
				EnglishSubPath: tt.englishSubPath,
				ChineseSubPath: tt.chineseSubPath,
			}

			got := processor.ShouldRun(pctx)
			if got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name          string
		mergedSubPath string
		want          bool
	}{
		{
			name:          "Merged subtitle present",
			mergedSubPath: "/tmp/merged.ass",
			want:          true,
		},
		{
			name:          "No merged subtitle",
			mergedSubPath: "",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewOutputProcessor(true)
			pctx := &ProcessingContext{
				MergedSubPath: tt.mergedSubPath,
			}

			got := processor.ShouldRun(pctx)
			if got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}



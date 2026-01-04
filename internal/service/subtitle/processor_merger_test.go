package subtitle

import (
	"context"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestMergerProcessor_Name(t *testing.T) {
	p := NewMergerProcessor(config.DuoSubsConfig{})
	if got := p.Name(); got != "Merger" {
		t.Errorf("Name() = %v, want %v", got, "Merger")
	}
}

func TestMergerProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name    string
		pctx    *ProcessingContext
		want    bool
	}{
		{
			name: "both subtitles present",
			pctx: &ProcessingContext{
				EnglishSubPath: "/path/to/english.srt",
				ChineseSubPath: "/path/to/chinese.srt",
			},
			want: true,
		},
		{
			name: "missing english subtitle",
			pctx: &ProcessingContext{
				EnglishSubPath: "",
				ChineseSubPath: "/path/to/chinese.srt",
			},
			want: false,
		},
		{
			name: "missing chinese subtitle",
			pctx: &ProcessingContext{
				EnglishSubPath: "/path/to/english.srt",
				ChineseSubPath: "",
			},
			want: false,
		},
		{
			name: "both subtitles missing",
			pctx: &ProcessingContext{
				EnglishSubPath: "",
				ChineseSubPath: "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewMergerProcessor(config.DuoSubsConfig{})
			if got := p.ShouldRun(tt.pctx); got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergerProcessor_Process_ShouldNotPanic(t *testing.T) {
	// This test verifies that Process doesn't panic with various inputs
	// It will fail because duosubs isn't installed, but shouldn't panic

	tests := []struct {
		name string
		cfg  config.DuoSubsConfig
		pctx *ProcessingContext
	}{
		{
			name: "with model specified",
			cfg: config.DuoSubsConfig{
				Model:          "sentence-transformers/LaBSE",
				Device:         "cpu",
				TimeoutMinutes: 10,
			},
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/english.srt",
				ChineseSubPath: "/tmp/chinese.srt",
			},
		},
		{
			name: "without model (uses default)",
			cfg: config.DuoSubsConfig{
				Model:          "", // Empty - should use DuoSubs default
				Device:         "cpu",
				TimeoutMinutes: 10,
			},
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/english.srt",
				ChineseSubPath: "/tmp/chinese.srt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Process() panicked: %v", r)
				}
			}()

			p := NewMergerProcessor(tt.cfg)
			ctx := context.Background()

			// This will error because duosubs isn't installed and files don't exist,
			// but it shouldn't panic
			err := p.Process(ctx, tt.pctx)
			if err == nil {
				t.Error("Process() expected error (duosubs not installed), got nil")
			}
		})
	}
}

func TestMergerProcessor_Process_CreatesOutputDirectory(t *testing.T) {
	// Test that the processor creates the output directory
	// Even though duosubs will fail, the directory creation should succeed

	p := NewMergerProcessor(config.DuoSubsConfig{
		Model:          "test-model",
		Device:         "cpu",
		TimeoutMinutes: 1,
	})

	pctx := &ProcessingContext{
		EnglishSubPath: "/nonexistent/english.srt",
		ChineseSubPath: "/nonexistent/chinese.srt",
	}

	ctx := context.Background()

	// This will fail at duosubs execution, but should create the temp dir first
	err := p.Process(ctx, pctx)
	if err == nil {
		t.Error("Process() expected error, got nil")
	}

	// The error should be from duosubs execution, not directory creation
	// If directory creation failed, error would contain "failed to create output directory"
	if contains(err.Error(), "failed to create output directory") {
		t.Errorf("Process() failed at directory creation, should fail at duosubs execution: %v", err)
	}
}

// Helper function
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


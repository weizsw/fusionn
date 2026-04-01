package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/executor"
)

func TestSDHFilterProcessor_Name(t *testing.T) {
	p := NewSDHFilterProcessor()
	if p.Name() != "SDHFilter" {
		t.Errorf("Name() = %q, want %q", p.Name(), "SDHFilter")
	}
}

func TestSDHFilterProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name string
		pctx *ProcessingContext
		want bool
	}{
		{
			name: "SDH English track present",
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/eng.srt",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: true, ExtractedPath: "/tmp/eng.srt"},
				},
			},
			want: true,
		},
		{
			name: "Non-SDH English track",
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/eng.srt",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: false, ExtractedPath: "/tmp/eng.srt"},
				},
			},
			want: false,
		},
		{
			name: "No English track",
			pctx: &ProcessingContext{
				Analysis: &AnalysisResult{},
			},
			want: false,
		},
		{
			name: "No analysis",
			pctx: &ProcessingContext{},
			want: false,
		},
		{
			name: "No English sub path",
			pctx: &ProcessingContext{
				EnglishSubPath: "",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: true},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSDHFilterProcessor()
			got := p.ShouldRun(tt.pctx)
			if got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSDHFilterProcessor_Process_CleanitNotAvailable(t *testing.T) {
	if executor.IsCleanitAvailable() {
		t.Skip("cleanit is available, skipping unavailable test")
	}

	tmpDir := t.TempDir()
	srtPath := filepath.Join(tmpDir, "test.srt")
	content := "1\n00:00:01,000 --> 00:00:02,000\n[music playing] Hello\n"
	os.WriteFile(srtPath, []byte(content), 0644)

	p := NewSDHFilterProcessor()
	pctx := &ProcessingContext{
		EnglishSubPath: srtPath,
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{IsSDH: true},
		},
	}

	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Errorf("Process() should return nil on cleanit failure, got: %v", err)
	}

	// Original file should be untouched
	data, _ := os.ReadFile(srtPath)
	if string(data) != content {
		t.Error("Original file was modified despite cleanit not being available")
	}
}

func TestSDHFilterProcessor_Process_WithCleanit(t *testing.T) {
	if !executor.IsCleanitAvailable() {
		t.Skip("cleanit not available")
	}

	tmpDir := t.TempDir()
	srtPath := filepath.Join(tmpDir, "test.srt")
	content := "1\n00:00:01,000 --> 00:00:02,000\n[music playing]\n\n2\n00:00:03,000 --> 00:00:04,000\nHello world\n\n"
	os.WriteFile(srtPath, []byte(content), 0644)

	p := NewSDHFilterProcessor()
	pctx := &ProcessingContext{
		EnglishSubPath: srtPath,
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{IsSDH: true},
		},
	}

	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	data, _ := os.ReadFile(srtPath)
	cleaned := string(data)
	if strings.Contains(cleaned, "[music playing]") {
		t.Errorf("SDH content was not removed: %s", cleaned)
	}
}

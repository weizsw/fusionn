package subtitle

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestMergerProcessor_Name(t *testing.T) {
	p, err := NewMergerProcessor(config.DuoSubsConfig{Mode: "local"})
	if err != nil {
		t.Fatalf("NewMergerProcessor() error = %v", err)
	}
	if got := p.Name(); got != "Merger" {
		t.Errorf("Name() = %v, want %v", got, "Merger")
	}
}

func TestMergerProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name string
		pctx *ProcessingContext
		want bool
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
		{
			name: "skip when merged path already set",
			pctx: &ProcessingContext{
				EnglishSubPath: "/path/to/english.srt",
				ChineseSubPath: "/path/to/chinese.srt",
				MergedSubPath:  "/path/to/already_merged.ass",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewMergerProcessor(config.DuoSubsConfig{Mode: "local"})
			if err != nil {
				t.Fatalf("NewMergerProcessor() error = %v", err)
			}
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
				Mode:           "local",
				Model:          "sentence-transformers/LaBSE",
				Device:         "cpu",
				TimeoutMinutes: 10,
			},
			pctx: &ProcessingContext{},
		},
		{
			name: "without model (uses default)",
			cfg: config.DuoSubsConfig{
				Mode:           "local",
				Model:          "", // Empty - should use DuoSubs default
				Device:         "cpu",
				TimeoutMinutes: 10,
			},
			pctx: &ProcessingContext{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// VideoPath decides where Process() creates its temp merge
			// directory, so keep it - and the subtitle inputs - under
			// t.TempDir(): the testing framework then removes the merge
			// directory with the test instead of leaving an empty
			// .fusionn-merge-* directory in the package directory.
			root := t.TempDir()
			tt.pctx.VideoPath = filepath.Join(root, "video.mkv")
			tt.pctx.EnglishSubPath = filepath.Join(root, "english.srt")
			tt.pctx.ChineseSubPath = filepath.Join(root, "chinese.srt")

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Process() panicked: %v", r)
				}
			}()

			p, err := NewMergerProcessor(tt.cfg)
			if err != nil {
				t.Fatalf("NewMergerProcessor() error = %v", err)
			}
			ctx := context.Background()

			// This will error because duosubs isn't installed and files don't exist,
			// but it shouldn't panic
			err = p.Process(ctx, tt.pctx)
			if err == nil {
				t.Error("Process() expected error (duosubs not installed), got nil")
			}

			assertTempMergeDirUnder(t, tt.pctx, root)
		})
	}
}

func TestMergerProcessor_Process_CreatesOutputDirectory(t *testing.T) {
	// Test that the processor creates the output directory
	// Even though duosubs will fail, the directory creation should succeed

	p, err := NewMergerProcessor(config.DuoSubsConfig{
		Mode:           "local",
		Model:          "test-model",
		Device:         "cpu",
		TimeoutMinutes: 1,
	})
	if err != nil {
		t.Fatalf("NewMergerProcessor() error = %v", err)
	}

	root := t.TempDir()
	pctx := &ProcessingContext{
		VideoPath:      filepath.Join(root, "video.mkv"),
		EnglishSubPath: filepath.Join(root, "english.srt"),
		ChineseSubPath: filepath.Join(root, "chinese.srt"),
	}

	ctx := context.Background()

	// This will fail at duosubs execution, but should create the temp dir first
	err = p.Process(ctx, pctx)
	if err == nil {
		t.Error("Process() expected error, got nil")
	}

	// The error should be from duosubs execution, not directory creation
	// If directory creation failed, error would contain "failed to create output directory"
	if contains(err.Error(), "failed to create output directory") {
		t.Errorf("Process() failed at directory creation, should fail at duosubs execution: %v", err)
	}

	assertTempMergeDirUnder(t, pctx, root)
}

// assertTempMergeDirUnder checks that Process() created its merge directory and
// that it sits directly under the test-owned root, so cleanup follows the test.
func assertTempMergeDirUnder(t *testing.T, pctx *ProcessingContext, root string) {
	t.Helper()
	if pctx.TempMergeDir == "" {
		t.Fatal("Process() did not set TempMergeDir")
	}
	if got := filepath.Dir(pctx.TempMergeDir); got != root {
		t.Errorf("TempMergeDir parent = %q, want %q", got, root)
	}
	if info, err := os.Stat(pctx.TempMergeDir); err != nil || !info.IsDir() {
		t.Errorf("TempMergeDir %q was not created: %v", pctx.TempMergeDir, err)
	}
}

// A merge directory under t.TempDir() disappears with the subtest that created
// it. The earlier shape (no VideoPath) left .fusionn-merge-* in the package
// directory after every run, because an isolated Process call has no pipeline
// cleanup to remove it.
func TestMergerProcessor_Process_TempMergeDirRemovedWithTestRoot(t *testing.T) {
	p, err := NewMergerProcessor(config.DuoSubsConfig{
		Mode:           "local",
		Model:          "test-model",
		Device:         "cpu",
		TimeoutMinutes: 1,
	})
	if err != nil {
		t.Fatalf("NewMergerProcessor() error = %v", err)
	}

	var root, mergeDir string
	t.Run("merge run", func(t *testing.T) {
		root = t.TempDir()
		pctx := &ProcessingContext{
			VideoPath:      filepath.Join(root, "video.mkv"),
			EnglishSubPath: filepath.Join(root, "english.srt"),
			ChineseSubPath: filepath.Join(root, "chinese.srt"),
		}

		if err := p.Process(context.Background(), pctx); err == nil {
			t.Error("Process() expected error (duosubs not installed), got nil")
		}
		mergeDir = pctx.TempMergeDir
		if mergeDir == "" {
			t.Fatal("Process() did not set TempMergeDir")
		}
		if info, err := os.Stat(mergeDir); err != nil || !info.IsDir() {
			t.Fatalf("TempMergeDir %q was not created: %v", mergeDir, err)
		}
	})

	// The subtest ended, so the testing framework removed its temp root and
	// the merge directory with it.
	if _, err := os.Stat(mergeDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("merge directory %q survived the subtest: %v", mergeDir, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("test root %q survived the subtest: %v", root, err)
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

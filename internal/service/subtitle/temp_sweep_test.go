package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// staleTempAge is older than the sweep's 7-day limit.
const staleTempAge = 8 * 24 * time.Hour

// writeTempEntry creates a file (with a file inside when it is a directory) and
// sets its modification time to age ago.
func writeTempEntry(t *testing.T, path string, isDir bool, age time.Duration) {
	t.Helper()
	if isDir {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("create directory %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "bilingual.ass"), []byte("partial"), 0644); err != nil {
			t.Fatalf("write into %s: %v", path, err)
		}
	} else if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("create file %s: %v", path, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("set mtime of %s: %v", path, err)
	}
}

// The sweep removes old entries of exactly the three fusionn temp prefixes from
// the video's own directory, leaves fresh ones and every other name alone, and
// does not recurse.
func TestSweepStaleTempFilesRemovesOnlyOldFusionnEntries(t *testing.T) {
	buf := captureLogs(t)
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "Episode.mkv")

	stale := []struct {
		name  string
		isDir bool
	}{
		{name: ".fusionn-extract-Episode-eng.srt"},
		{name: ".fusionn-extract-Episode-chs.srt"},
		{name: ".fusionn-merge-11111111-1111-1111-1111-111111111111", isDir: true},
		{name: ".fusionn-dual-22222222-2222-2222-2222-222222222222", isDir: true},
	}
	for _, entry := range stale {
		writeTempEntry(t, filepath.Join(dir, entry.name), entry.isDir, staleTempAge)
	}

	fresh := []struct {
		name  string
		isDir bool
	}{
		{name: ".fusionn-extract-Episode-eng-fresh.srt"},
		{name: ".fusionn-merge-33333333-3333-3333-3333-333333333333", isDir: true},
		{name: ".fusionn-dual-44444444-4444-4444-4444-444444444444", isDir: true},
	}
	for _, entry := range fresh {
		writeTempEntry(t, filepath.Join(dir, entry.name), entry.isDir, 0)
	}

	untouched := []string{
		"Episode.eng.srt",
		"Episode.chs.srt",
		"Episode.zh.srt",
		"Episode.mkv",
		"fusionn-extract-Episode-eng.srt", // without the leading dot, not a fusionn temp name
		".fusionn-other-thing",
	}
	for _, name := range untouched {
		writeTempEntry(t, filepath.Join(dir, name), false, staleTempAge)
	}

	// A matching entry one directory deeper is outside the video's directory.
	nested := filepath.Join(dir, "sub", ".fusionn-extract-nested.srt")
	if err := os.MkdirAll(filepath.Dir(nested), 0755); err != nil {
		t.Fatal(err)
	}
	writeTempEntry(t, nested, false, staleTempAge)

	sweepStaleTempFiles(context.Background(), videoPath)

	for _, entry := range stale {
		assertGone(t, filepath.Join(dir, entry.name))
	}
	for _, entry := range fresh {
		assertKept(t, filepath.Join(dir, entry.name), true)
	}
	for _, name := range untouched {
		assertKept(t, filepath.Join(dir, name), true)
	}
	assertKept(t, nested, true)

	logs := buf.String()
	if !strings.Contains(logs, "event=temp_swept") || !strings.Contains(logs, "count=4") {
		t.Errorf("missing temp_swept INFO with count=4:\n%s", logs)
	}
}

// A relative video path must not turn the sweep into a deletion pass over the
// process working directory: it returns before listing anything.
func TestSweepIgnoresRelativeVideoPath(t *testing.T) {
	buf := captureLogs(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, ".fusionn-extract-Episode-eng.srt")
	writeTempEntry(t, stale, false, staleTempAge)

	// The module still targets go1.23, so testing.T.Chdir is unavailable.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	sweepStaleTempFiles(context.Background(), "Episode.mkv")

	assertKept(t, stale, true)
	if logs := buf.String(); strings.Contains(logs, "event=temp_swept") || strings.Contains(logs, "event=temp_sweep_failed") {
		t.Errorf("relative video path reached the sweep:\n%s", logs)
	}
}

// A media directory the sweep cannot list is a WARN, not a job failure.
func TestSweepFailureDoesNotFailProcessMedia(t *testing.T) {
	buf := captureLogs(t)
	service := newStubService(t, idleQueue(), stubAnalyzingProcessor{})

	missing := filepath.Join(t.TempDir(), "missing", "Episode.mkv")
	if err := service.ProcessMedia(context.Background(), MediaParams{Path: missing, Title: "Episode"}); err != nil {
		t.Fatalf("ProcessMedia() failed on an unlistable media directory: %v", err)
	}
	if !strings.Contains(buf.String(), "event=temp_sweep_failed") {
		t.Errorf("missing temp_sweep_failed warning:\n%s", buf.String())
	}
}

// A stale entry the sweep cannot remove is a WARN, not a job failure.
func TestSweepRemovalFailureDoesNotFailProcessMedia(t *testing.T) {
	buf := captureLogs(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, ".fusionn-extract-Episode-eng.srt")
	writeTempEntry(t, stale, false, staleTempAge)

	// Without write permission on the directory the entry cannot be removed.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	// Runs before the t.TempDir cleanup, which needs the permission back.
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	service := newStubService(t, idleQueue(), stubAnalyzingProcessor{})
	if err := service.ProcessMedia(context.Background(), MediaParams{Path: filepath.Join(dir, "Episode.mkv"), Title: "Episode"}); err != nil {
		t.Fatalf("ProcessMedia() failed on a failed sweep removal: %v", err)
	}
	if _, err := os.Stat(stale); err == nil && !strings.Contains(buf.String(), "event=temp_sweep_failed") {
		t.Errorf("removal failed without a temp_sweep_failed warning:\n%s", buf.String())
	}
}

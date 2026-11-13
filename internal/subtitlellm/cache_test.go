package subtitlellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A window record served from cache is touched, so expiry keeps what jobs read.
func TestAlignCacheHitTouchesRecord(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	engine := NewEngine()

	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("first Align: %v", err)
	}
	path := singleCacheFile(t, filepath.Join(cfg.CacheDir, cacheNamespace))

	aged := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, aged, aged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("second Align: %v", err)
	}
	if calls != 1 {
		t.Fatalf("cached run sent %d HTTP call(s), want the cache to answer", calls)
	}
	assertTouched(t, path, aged)
}

// A split-check record served from cache is touched the same way.
func TestCheckSplitsCacheHitTouchesRecord(t *testing.T) {
	calls := 0
	server := splitCheckServer(t, &calls, http.StatusOK)
	cfg := testConfig(t, server.URL+"/v1")
	cfg.EnChars, cfg.ZhChars = 40, 20
	bundle, mapping := londonInputs()
	cacheDir := t.TempDir()

	if _, _, _, hits := NewEngine().checkSplits(context.Background(), bundle, mapping.Groups, cfg, cacheDir); hits != 0 {
		t.Fatalf("first run hits = %d, want 0", hits)
	}
	path := singleCacheFile(t, cacheDir)

	aged := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, aged, aged); err != nil {
		t.Fatal(err)
	}
	if _, _, _, hits := NewEngine().checkSplits(context.Background(), bundle, mapping.Groups, cfg, cacheDir); hits != 1 {
		t.Fatalf("cached run hits = %d, want 1", hits)
	}
	if calls != 1 {
		t.Fatalf("cached run sent %d HTTP call(s), want 1", calls)
	}
	assertTouched(t, path, aged)
}

// The prune removes the old records of this package's namespace only: fresh
// records, other files in the cache directory and directories stay.
func TestPruneCacheRemovesOnlyStaleRecords(t *testing.T) {
	cacheDir := t.TempDir()
	namespace := filepath.Join(cacheDir, cacheNamespace)
	if err := os.MkdirAll(namespace, 0755); err != nil {
		t.Fatal(err)
	}

	stale := []string{"000-abcdef.json", "split-abcdef.json", "000-abcdef.json.tmp-91"}
	for _, name := range stale {
		writeAgedCacheFile(t, filepath.Join(namespace, name), 8*24*time.Hour)
	}
	fresh := []string{"001-abcdef.json", "split-fedcba.json"}
	for _, name := range fresh {
		writeAgedCacheFile(t, filepath.Join(namespace, name), 0)
	}
	subdir := filepath.Join(namespace, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(cacheDir, "unrelated.json")
	writeAgedCacheFile(t, outside, 8*24*time.Hour)

	stats, err := PruneCache(cacheDir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("PruneCache: %v", err)
	}
	if stats.Files != len(stale) || stats.Bytes != int64(len(stale)*len(cacheRecordContent)) {
		t.Errorf("stats = %+v, want files=%d bytes=%d", stats, len(stale), len(stale)*len(cacheRecordContent))
	}
	for _, name := range stale {
		requirePathMissing(t, filepath.Join(namespace, name))
	}
	for _, name := range fresh {
		requirePathPresent(t, filepath.Join(namespace, name))
	}
	requirePathPresent(t, subdir)
	requirePathPresent(t, outside)
}

// A max age of 0 or less keeps every record.
func TestPruneCacheWithoutExpiryKeepsRecords(t *testing.T) {
	cacheDir := t.TempDir()
	randomRecord := filepath.Join(cacheDir, cacheNamespace, "000-abcdef.json")
	writeAgedCacheFile(t, randomRecord, 30*24*time.Hour)

	for _, maxAge := range []time.Duration{0, -time.Hour} {
		stats, err := PruneCache(cacheDir, maxAge)
		if err != nil {
			t.Fatalf("PruneCache(%v): %v", maxAge, err)
		}
		if stats.Files != 0 || stats.Bytes != 0 {
			t.Fatalf("PruneCache(%v) = %+v, want nothing removed", maxAge, stats)
		}
	}
	requirePathPresent(t, randomRecord)
}

func singleCacheFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries in %s = %v, err = %v", dir, entries, err)
	}
	return filepath.Join(dir, entries[0].Name())
}

func assertTouched(t *testing.T, path string, aged time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().After(aged.Add(24 * time.Hour)) {
		t.Errorf("cache hit did not touch %s: mtime = %v, aged to %v", path, info.ModTime(), aged)
	}
}

// cacheRecordContent is the fixed body of a fixture record; its length makes
// the removed byte counts predictable.
const cacheRecordContent = "0123456789"

func writeAgedCacheFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(cacheRecordContent), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}
}

func requirePathPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path %q is gone: %v", path, err)
	}
}

func requirePathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("path %q survived the prune", path)
	}
}

package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
)

// staleCacheAge is older than the 7-day expiry of these tests.
const staleCacheAge = 8 * 24 * time.Hour

// The merger only knows the record namespace through subtitlellm.PruneCache, so
// the fixture builds it with the layout literal.
const cachePruneTestNamespace = "subtitlellm-v1"

// The prune removes only the stale entries of both cache areas and keeps the
// artifact directory of the configured mapping_path.
func TestCachePruneRemovesOnlyStaleCacheEntries(t *testing.T) {
	buf := captureLogs(t)
	cacheDir := t.TempDir()
	useCachePruneGuard(t, time.Time{})

	staleRecord := writeAgedCacheRecord(t, cacheDir, "000-abcdef.json", staleCacheAge)
	freshRecord := writeAgedCacheRecord(t, cacheDir, "001-abcdef.json", 0)
	staleArtifacts := writeArtifactFixture(t, cacheDir, "aaa-111111111111", staleCacheAge)
	freshArtifacts := writeArtifactFixture(t, cacheDir, "ccc-333333333333", 0)
	keptArtifacts := writeArtifactFixture(t, cacheDir, "bbb-222222222222", staleCacheAge)
	mappingPath := filepath.Join(keptArtifacts, "alignment", "mapping.json")

	maybePruneLLMCache(context.Background(), config.LLMConfig{
		CacheEnabled: true, CacheDir: cacheDir, CacheMaxAgeDays: 7, MappingPath: mappingPath,
	})

	assertGone(t, staleRecord)
	assertKept(t, freshRecord, true)
	assertGone(t, staleArtifacts)
	assertKept(t, freshArtifacts, true)
	assertKept(t, keptArtifacts, true)
	assertKept(t, mappingPath, true)

	logs := buf.String()
	if !strings.Contains(logs, "event=cache_pruned") ||
		!strings.Contains(logs, "files=1") || !strings.Contains(logs, "dirs=1") {
		t.Errorf("missing cache_pruned INFO with files=1 dirs=1:\n%s", logs)
	}
}

// The prune runs at most once per process per day.
func TestCachePruneRunsAtMostOncePerDay(t *testing.T) {
	buf := captureLogs(t)
	cacheDir := t.TempDir()
	useCachePruneGuard(t, time.Time{})
	cfg := config.LLMConfig{CacheEnabled: true, CacheDir: cacheDir, CacheMaxAgeDays: 7}

	first := writeAgedCacheRecord(t, cacheDir, "000-abcdef.json", staleCacheAge)
	maybePruneLLMCache(context.Background(), cfg)
	assertGone(t, first)

	second := writeAgedCacheRecord(t, cacheDir, "001-abcdef.json", staleCacheAge)
	maybePruneLLMCache(context.Background(), cfg)
	assertKept(t, second, true)

	useCachePruneGuard(t, time.Now().Add(-25*time.Hour))
	maybePruneLLMCache(context.Background(), cfg)
	assertGone(t, second)

	if got := strings.Count(buf.String(), "event=cache_pruned"); got != 2 {
		t.Errorf("cache_pruned events = %d, want 2:\n%s", got, buf.String())
	}
}

// A disabled cache and a max age of 0 (= never expire) never prune.
func TestCachePruneDisabledOrWithoutExpiryDoesNothing(t *testing.T) {
	buf := captureLogs(t)
	cacheDir := t.TempDir()
	stale := writeAgedCacheRecord(t, cacheDir, "000-abcdef.json", staleCacheAge)

	useCachePruneGuard(t, time.Time{})
	maybePruneLLMCache(context.Background(), config.LLMConfig{
		CacheEnabled: false, CacheDir: cacheDir, CacheMaxAgeDays: 7,
	})
	assertKept(t, stale, true)

	useCachePruneGuard(t, time.Time{})
	maybePruneLLMCache(context.Background(), config.LLMConfig{
		CacheEnabled: true, CacheDir: cacheDir, CacheMaxAgeDays: 0,
	})
	assertKept(t, stale, true)

	if strings.Contains(buf.String(), "event=cache_pruned") {
		t.Errorf("prune ran with the cache disabled or without expiry:\n%s", buf.String())
	}
}

// A cache the prune cannot walk is a WARN, never a merge failure.
func TestCachePruneFailureWarnsOnly(t *testing.T) {
	buf := captureLogs(t)
	cacheDir := t.TempDir()
	// A file where the artifact directory belongs makes ReadDir fail.
	if err := os.WriteFile(filepath.Join(cacheDir, llmArtifactDirName), []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	useCachePruneGuard(t, time.Time{})

	maybePruneLLMCache(context.Background(), config.LLMConfig{
		CacheEnabled: true, CacheDir: cacheDir, CacheMaxAgeDays: 7,
	})

	if !strings.Contains(buf.String(), "event=cache_prune_failed") {
		t.Errorf("missing cache_prune_failed warning:\n%s", buf.String())
	}
}

// useCachePruneGuard injects the guard state and restores it after the test.
func useCachePruneGuard(t *testing.T, lastRun time.Time) {
	t.Helper()
	cachePruneMu.Lock()
	previous := cachePruneLastRun
	cachePruneLastRun = lastRun
	cachePruneMu.Unlock()
	t.Cleanup(func() {
		cachePruneMu.Lock()
		cachePruneLastRun = previous
		cachePruneMu.Unlock()
	})
}

// writeAgedCacheRecord creates one record of the subtitlellm namespace, age old.
func writeAgedCacheRecord(t *testing.T, cacheDir, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(cacheDir, cachePruneTestNamespace)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("0123456789"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	stampPath(t, path, age)
	return path
}

// writeArtifactFixture creates one artifact directory with its files, stamped so
// the directory counts as age old.
func writeArtifactFixture(t *testing.T, cacheDir, id string, age time.Duration) string {
	t.Helper()
	root := filepath.Join(cacheDir, llmArtifactDirName, id)
	alignment := filepath.Join(root, "alignment")
	if err := os.MkdirAll(alignment, 0755); err != nil {
		t.Fatalf("create %s: %v", alignment, err)
	}
	for _, path := range []string{
		filepath.Join(root, "sources.json"),
		filepath.Join(alignment, "mapping.json"),
		filepath.Join(alignment, "audit.json"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		stampPath(t, path, age)
	}
	// A fresh directory modification time would otherwise decide the age.
	stampPath(t, alignment, age)
	stampPath(t, root, age)
	return root
}

func stampPath(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}
}

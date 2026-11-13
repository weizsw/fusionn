package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/subtitlellm"
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

// A prune that scans a stale artifact directory while a merge persists into it
// must never break that merge's artifact write: the prune must not remove a
// directory while persistLLMArtifacts is writing it.
func TestArtifactPruneDoesNotBreakConcurrentPersistence(t *testing.T) {
	cacheDir := t.TempDir()
	processor := &MergerProcessor{llmConfig: config.LLMConfig{CacheEnabled: true, CacheDir: cacheDir}}
	bundle := subtitlellm.Bundle{Fingerprint: strings.Repeat("a", 32), EN: artifactBundleCues(20)}
	mapping := subtitlellm.Mapping{Fingerprint: strings.Repeat("b", 32)}
	outputDir := t.TempDir()

	type pruneResult struct {
		stats subtitlellm.PruneStats
		err   error
	}

	for trial := 0; trial < 60; trial++ {
		root := writeArtifactFixture(t, cacheDir, artifactID(bundle, mapping), staleCacheAge)
		// The fillers keep the staleness walk busy, so the artifact write below
		// lands inside the window between the scan and the removal.
		fillStaleArtifactDir(t, root, 80)
		sourcesPath := filepath.Join(root, "sources.json")

		start := make(chan struct{})
		written := make(chan error, 1)
		pruned := make(chan pruneResult, 1)
		go func() {
			<-start
			stats, err := pruneLLMArtifacts(cacheDir, 7*24*time.Hour, "")
			pruned <- pruneResult{stats: stats, err: err}
		}()
		go func() {
			<-start
			written <- processor.persistLLMArtifacts(context.Background(), outputDir, bundle, mapping, subtitlellm.Audit{})
		}()
		close(start)

		writeErr := <-written
		result := <-pruned
		if result.stats.Dirs == 0 {
			continue // the merge refreshed the directory before the removal
		}
		if writeErr != nil {
			t.Fatalf("trial %d: pruning stale artifacts failed a concurrent persistence: %v (prune err=%v)", trial, writeErr, result.err)
		}
		if _, err := os.Stat(sourcesPath); err != nil {
			t.Fatalf("trial %d: prune removed a successfully refreshed sources.json: %v", trial, err)
		}
	}
}

// artifactBundleCues make the artifact payload long enough that its write lands
// inside the staleness walk of a prune that starts with it.
func artifactBundleCues(count int) []subtitlellm.Cue {
	cues := make([]subtitlellm.Cue, 0, count)
	for i := 0; i < count; i++ {
		text := strings.Repeat("cue text ", 12)
		cues = append(cues, subtitlellm.Cue{
			ID: i + 1, Start: i * 1000, End: i*1000 + 500,
			Raw: text, Plain: text, Display: text,
			Style: "Default", Alignment: 2, Changes: []string{},
		})
	}
	return cues
}

// fillStaleArtifactDir adds age-old entries to an artifact directory so the
// staleness walk of the prune lasts longer than the artifact write it races.
// The names sort before sources.json, the entry the walk visits last.
func fillStaleArtifactDir(t *testing.T, root string, entries int) {
	t.Helper()
	for i := 0; i < entries; i++ {
		path := filepath.Join(root, fmt.Sprintf("filler-%04d.json", i))
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		stampPath(t, path, staleCacheAge)
	}
	// Creating the fillers refreshed the directory itself; keep it stale.
	stampPath(t, root, staleCacheAge)
}

// Artifact pruning waits for artifact persistence that is in flight: it takes
// the write side of the lock a whole artifact write holds.
func TestArtifactPruneWaitsForInFlightPersistence(t *testing.T) {
	cacheDir := t.TempDir()
	root := writeArtifactFixture(t, cacheDir, "aaa-111111111111", staleCacheAge)

	type pruneOutcome struct {
		stats subtitlellm.PruneStats
		err   error
	}

	// Stand in for a merge that is writing its artifacts right now.
	llmArtifactSetMu.RLock()
	done := make(chan pruneOutcome, 1)
	go func() {
		stats, err := pruneLLMArtifacts(cacheDir, 7*24*time.Hour, "")
		done <- pruneOutcome{stats: stats, err: err}
	}()

	select {
	case outcome := <-done:
		llmArtifactSetMu.RUnlock()
		t.Fatalf("prune ran while artifact persistence was in flight: %+v", outcome)
	case <-time.After(100 * time.Millisecond):
	}
	llmArtifactSetMu.RUnlock()

	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatalf("pruneLLMArtifacts: %v", outcome.err)
		}
		if outcome.stats.Dirs != 1 {
			t.Errorf("prune after persistence = %+v, want one directory removed", outcome.stats)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("prune did not finish after persistence ended")
	}
	assertGone(t, root)
}

// The mapping exemption follows filesystem aliases: the configured mapping is
// kept whether cache_dir or mapping_path is a symlink or a relative path, and an
// unrelated stale directory is still removed.
func TestCachePruneKeepsMappingThroughSymlinkAliases(t *testing.T) {
	tests := []struct {
		name            string
		cacheSymlink    bool
		mappingSymlink  bool
		cacheRelative   bool
		mappingRelative bool
	}{
		{name: "cache_dir through a symlink", cacheSymlink: true},
		{name: "mapping_path through a symlink", mappingSymlink: true},
		{name: "relative cache_dir, absolute mapping_path", cacheRelative: true},
		{name: "absolute cache_dir, relative mapping_path", mappingRelative: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			realCache := t.TempDir()
			kept := writeArtifactFixture(t, realCache, "selected-input", staleCacheAge)
			other := writeArtifactFixture(t, realCache, "other-artifacts", staleCacheAge)
			alias := filepath.Join(t.TempDir(), "cache-alias")
			if err := os.Symlink(realCache, alias); err != nil {
				t.Fatal(err)
			}
			// A relative path resolves against the working directory.
			if tt.cacheRelative || tt.mappingRelative {
				chdirTest(t, filepath.Dir(realCache))
			}

			pruneDir := realCache
			switch {
			case tt.cacheSymlink:
				pruneDir = alias
			case tt.cacheRelative:
				pruneDir = filepath.Base(realCache)
			}
			mappingPath := filepath.Join(kept, "alignment", "mapping.json")
			switch {
			case tt.mappingSymlink:
				mappingPath = filepath.Join(alias, llmArtifactDirName, "selected-input", "alignment", "mapping.json")
			case tt.mappingRelative:
				mappingPath = filepath.Join(filepath.Base(realCache), llmArtifactDirName, "selected-input", "alignment", "mapping.json")
			}

			stats, err := pruneLLMArtifacts(pruneDir, 7*24*time.Hour, mappingPath)
			if err != nil {
				t.Fatalf("pruneLLMArtifacts: %v", err)
			}
			assertKept(t, kept, true)
			assertKept(t, filepath.Join(kept, "alignment", "mapping.json"), true)
			assertKept(t, filepath.Join(kept, "sources.json"), true)
			assertGone(t, other)
			if stats.Dirs != 1 {
				t.Errorf("stats = %+v, want exactly the unrelated directory removed", stats)
			}
		})
	}
}

// chdirTest moves the process into dir for the rest of the test.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

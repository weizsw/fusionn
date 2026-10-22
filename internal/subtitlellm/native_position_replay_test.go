package subtitlellm

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fusionn/internal/config"
)

// recordManifestEntry is one entry of a recorded-response manifest.
type recordManifestEntry struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// replayConfig describes one offline replay of recorded LLM responses. Every path is
// explicit; nothing is silently skipped. Dir is only used to derive defaults when a
// specific path is empty.
type replayConfig struct {
	Dir          string
	ManifestPath string
	SourcesPath  string
	StylePath    string
	WordsPath    string
	ASSPath      string
}

type replayResult struct {
	Bundle        Bundle
	Mapping       Mapping
	Audit         Audit
	Events        []renderedEvent
	Style         config.ASSStyleConfig
	Words         WordTimings
	RecordedCount int
	DeliveredASS  string
	RenderedASS   string
}

// resolveManifest picks the manifest path from cfg.
func (cfg replayConfig) resolveManifest() string {
	if cfg.ManifestPath != "" {
		return cfg.ManifestPath
	}
	if cfg.Dir == "" {
		return ""
	}
	for _, cand := range []string{
		filepath.Join(cfg.Dir, "recorded-responses", "manifest.json"),
		filepath.Join(cfg.Dir, "manifest.json"),
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

// replayRecordedResponses replays recorded LLM responses through production decode,
// validation, and reconcile, then validates and re-renders the whole episode. It makes
// zero network requests. Any declared input that is missing, unreadable, or malformed
// is reported as an error; nothing is silently skipped.
func replayRecordedResponses(cfg replayConfig) (*replayResult, error) {
	manifestPath := cfg.resolveManifest()
	if manifestPath == "" {
		return nil, fmt.Errorf("recorded-response manifest not found (Dir=%q)", cfg.Dir)
	}

	sourcesPath := cfg.SourcesPath
	if sourcesPath == "" && cfg.Dir != "" {
		sourcesPath = filepath.Join(cfg.Dir, "sources.json")
	}
	if sourcesPath == "" {
		return nil, fmt.Errorf("sources path required")
	}

	stylePath := cfg.StylePath
	if stylePath == "" && cfg.Dir != "" {
		stylePath = filepath.Join(cfg.Dir, "delivered-style.json")
	}
	if stylePath == "" {
		return nil, fmt.Errorf("style path required")
	}

	wordsPath := cfg.WordsPath
	if wordsPath == "" && cfg.Dir != "" {
		cand := filepath.Join(cfg.Dir, "word-timings.json")
		if _, err := os.Stat(cand); err == nil {
			wordsPath = cand
		}
	}

	rawBundle, err := os.ReadFile(sourcesPath)
	if err != nil {
		return nil, fmt.Errorf("read sources %s: %w", sourcesPath, err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		return nil, fmt.Errorf("decode sources %s: %w", sourcesPath, err)
	}

	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", manifestPath, err)
	}
	var manifestEntries []recordManifestEntry
	if err := json.Unmarshal(rawManifest, &manifestEntries); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", manifestPath, err)
	}

	windows := buildWindows(bundle, 210000, 15000)
	if len(windows) != len(manifestEntries) {
		return nil, fmt.Errorf("window count %d != manifest entry count %d", len(windows), len(manifestEntries))
	}

	manifestDir := filepath.Dir(manifestPath)
	batches := make([]batch, 0, len(windows))
	for i, w := range windows {
		entry := manifestEntries[i]
		if entry.Index != w.Index {
			return nil, fmt.Errorf("manifest entry %d has index %d, want window index %d", i, entry.Index, w.Index)
		}

		recPath := entry.Path
		if !filepath.IsAbs(recPath) {
			recPath = filepath.Join(manifestDir, recPath)
		}
		rawRecord, err := os.ReadFile(recPath)
		if err != nil {
			return nil, fmt.Errorf("read record %s: %w", recPath, err)
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(rawRecord))
		if hash != entry.SHA256 {
			return nil, fmt.Errorf("record %s sha256 changed: got %s, want %s", filepath.Base(recPath), hash, entry.SHA256)
		}

		var rec cacheRecord
		if err := json.Unmarshal(rawRecord, &rec); err != nil {
			return nil, fmt.Errorf("decode record %s: %w", filepath.Base(recPath), err)
		}
		if rec.Index != w.Index {
			return nil, fmt.Errorf("record %s Index %d != window index %d", filepath.Base(recPath), rec.Index, w.Index)
		}

		// The recorded request must describe this exact window: core range and the
		// language-qualified cue membership, in order.
		var reqPayload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(rec.Request, &reqPayload); err != nil {
			return nil, fmt.Errorf("window %d decode request payload: %w", i, err)
		}
		var userContent string
		for _, m := range reqPayload.Messages {
			if m.Role == "user" {
				userContent = m.Content
				break
			}
		}
		if userContent == "" {
			return nil, fmt.Errorf("window %d has no user prompt in recorded request", i)
		}
		var reqView windowView
		if err := json.Unmarshal([]byte(userContent), &reqView); err != nil {
			return nil, fmt.Errorf("window %d decode user view: %w", i, err)
		}
		if len(reqView.CoreMS) != 2 || reqView.CoreMS[0] != w.CoreStart || reqView.CoreMS[1] != w.CoreEnd {
			return nil, fmt.Errorf("window %d core_ms mismatch: got %v, want [%d, %d]", i, reqView.CoreMS, w.CoreStart, w.CoreEnd)
		}
		if len(reqView.EN) != len(w.EN) {
			return nil, fmt.Errorf("window %d EN cue count mismatch: got %d, want %d", i, len(reqView.EN), len(w.EN))
		}
		for ci, cue := range w.EN {
			if reqView.EN[ci].ID != cue.ID {
				return nil, fmt.Errorf("window %d EN cue %d ID mismatch: got %d, want %d", i, ci, reqView.EN[ci].ID, cue.ID)
			}
		}
		if len(reqView.ZH) != len(w.ZH) {
			return nil, fmt.Errorf("window %d ZH cue count mismatch: got %d, want %d", i, len(reqView.ZH), len(w.ZH))
		}
		for ci, cue := range w.ZH {
			if reqView.ZH[ci].ID != cue.ID {
				return nil, fmt.Errorf("window %d ZH cue %d ID mismatch: got %d, want %d", i, ci, reqView.ZH[ci].ID, cue.ID)
			}
		}

		valid, rejected, err := validateResponse(rec.Response, w, MaxDistanceMS)
		if err != nil {
			return nil, fmt.Errorf("window %d validateResponse: %w", i, err)
		}

		// Reconciliation needs the real per-language request membership, exactly as
		// Engine.Align records it. Leaving these nil disables window arbitration.
		enInput := make([]int, len(w.EN))
		for ci, cue := range w.EN {
			enInput[ci] = cue.ID
		}
		zhInput := make([]int, len(w.ZH))
		for ci, cue := range w.ZH {
			zhInput[ci] = cue.ID
		}

		batches = append(batches, batch{
			Index:    w.Index,
			Valid:    valid,
			Rejected: rejected,
			Missing:  missingWindow(w, valid),
			ENInput:  enInput,
			ZHInput:  zhInput,
		})
	}

	// Offline replay of recorded responses performs no HTTP request and therefore
	// has no cache hits; CacheHits stays zero on purpose.
	audit := Audit{
		Rejected:       []RejectedRow{},
		Conflicts:      []Group{},
		Restored:       map[string][]int{"en": {}, "zh": {}},
		WindowMissing:  []WindowMissing{},
		CacheCorrupted: []CacheCorruption{},
		Coverage:       Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		ResolvedModels: []string{"qfmodel"},
		Provenance: &Provenance{
			Source:            "native",
			ConfiguredModel:   "group/auto-qfmodel",
			SourceFingerprint: bundle.Fingerprint,
		},
	}
	for _, b := range batches {
		audit.Rejected = append(audit.Rejected, b.Rejected...)
		audit.WindowMissing = append(audit.WindowMissing, b.Missing)
	}

	groups, reconcileAudit := reconcile(bundle, batches, 210000)
	audit.Conflicts = reconcileAudit.Conflicts
	audit.ContextDuplicates = reconcileAudit.ContextDuplicates
	audit.Restored = reconcileAudit.Restored

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups:            groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		return nil, fmt.Errorf("mappingDigest: %w", err)
	}
	mapping.Fingerprint = fingerprint

	// A declared word-timings file must be present and valid. Omitting it entirely is
	// the separate no-word-timings fixture and is allowed.
	var words WordTimings
	if wordsPath != "" {
		rawWords, err := os.ReadFile(wordsPath)
		if err != nil {
			return nil, fmt.Errorf("read word timings %s: %w", wordsPath, err)
		}
		if err := json.Unmarshal(rawWords, &words); err != nil {
			return nil, fmt.Errorf("decode word timings %s: %w", wordsPath, err)
		}
	}

	rawStyle, err := os.ReadFile(stylePath)
	if err != nil {
		return nil, fmt.Errorf("read style %s: %w", stylePath, err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(rawStyle, &style); err != nil {
		return nil, fmt.Errorf("decode style %s: %w", stylePath, err)
	}

	width, height := 3840, 1632
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		return nil, fmt.Errorf("validateEpisode: %w", err)
	}
	if failures := v.Verify(); len(failures) != 0 {
		return nil, fmt.Errorf("validateEpisode reported %d failures: %v", len(failures), failures)
	}

	rerendered, err := RenderWithWordTimings(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		return nil, fmt.Errorf("RenderWithWordTimings: %w", err)
	}

	// A declared ASS must be readable and must equal what the inputs re-render to.
	var delivered string
	if cfg.ASSPath != "" {
		rawASS, err := os.ReadFile(cfg.ASSPath)
		if err != nil {
			return nil, fmt.Errorf("read delivered ASS %s: %w", cfg.ASSPath, err)
		}
		delivered = string(rawASS)
		if err := verifySerializedASS(delivered, events); err != nil {
			return nil, fmt.Errorf("delivered ASS does not match audits: %w", err)
		}
		if string(rerendered) != delivered {
			return nil, fmt.Errorf("re-rendered ASS is not byte-identical to delivered ASS %s", cfg.ASSPath)
		}
	}

	return &replayResult{
		Bundle:        bundle,
		Mapping:       mapping,
		Audit:         audit,
		Events:        events,
		Style:         style,
		Words:         words,
		RecordedCount: len(windows),
		DeliveredASS:  delivered,
		RenderedASS:   string(rerendered),
	}, nil
}

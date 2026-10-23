package subtitlellm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fusionn/internal/config"
)

// sourcesSibling is the immutable source bundle persisted next to a mapping.
const sourcesSibling = "sources.json"

// importMapping loads a precomputed prototype mapping, verifies it strictly
// against this bundle, and records provenance. It never bypasses a mismatch:
// a mapping that describes different source text or timing is rejected.
func importMapping(bundle Bundle, cfg config.LLMConfig) (Mapping, Audit, error) {
	data, err := os.ReadFile(cfg.MappingPath)
	if err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("read mapping %q: %w", cfg.MappingPath, err)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("parse mapping %q: %w", cfg.MappingPath, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("parse mapping %q: %w", cfg.MappingPath, err)
	}
	allowed := map[string]bool{
		"version": true, "source_fingerprint": true, "parameters": true, "groups": true, "split_checks": true, "fingerprint": true,
	}
	for key := range top {
		if !allowed[key] {
			return Mapping{}, Audit{}, fmt.Errorf("mapping %q has unknown field %q", cfg.MappingPath, key)
		}
	}
	if err := requireModelOwnerWindows(top["groups"]); err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("mapping %q: %w", cfg.MappingPath, err)
	}
	var mapping Mapping
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&mapping); err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("parse mapping %q: %w", cfg.MappingPath, err)
	}
	if mapping.Version != Version {
		return Mapping{}, Audit{}, fmt.Errorf("mapping %q has unsupported version %d", cfg.MappingPath, mapping.Version)
	}
	if mapping.SourceFingerprint != bundle.Fingerprint {
		return Mapping{}, Audit{}, fmt.Errorf("mapping %q belongs to a different source bundle (mapping %s, sources %s)",
			cfg.MappingPath, mapping.SourceFingerprint, bundle.Fingerprint)
	}
	if mapping.Parameters.SchemaEnforcement == "" {
		mapping.Parameters.SchemaEnforcement = schemaEnforcement
	}
	computed, err := mappingDigest(mapping)
	if err != nil {
		return Mapping{}, Audit{}, err
	}
	if mapping.Fingerprint == "" || mapping.Fingerprint != computed {
		return Mapping{}, Audit{}, fmt.Errorf("mapping %q fingerprint mismatch", cfg.MappingPath)
	}

	// Verify text/time correspondence with the on-disk source bundle. Both our
	// own flat artifact layout (<bundle>/<mapping>/{sources,mapping}.json) and
	// the prototype layout (<bundle>/<mapping>/mapping.json with
	// <bundle>/sources.json) are accepted; a candidate only counts when its
	// fingerprint matches this bundle exactly.
	sourcesPath, err := resolveSourcesPath(bundle, cfg.MappingPath)
	if err != nil {
		return Mapping{}, Audit{}, err
	}
	if err := validateImportedGroups(mapping, bundle); err != nil {
		return Mapping{}, Audit{}, fmt.Errorf("mapping %q failed revalidation: %w", cfg.MappingPath, err)
	}

	provenance := &Provenance{
		Source:            "prototype-import",
		MappingPath:       cfg.MappingPath,
		SourcesPath:       sourcesPath,
		MappingModel:      mapping.Parameters.Model,
		ConfiguredModel:   cfg.Model,
		ModelMismatch:     mapping.Parameters.Model != cfg.Model,
		SourceFingerprint: bundle.Fingerprint,
	}
	audit := Audit{
		Rejected:      []RejectedRow{},
		Conflicts:     []Group{},
		Restored:      map[string][]int{"en": {}, "zh": {}},
		WindowMissing: []WindowMissing{},
		Pairing:       pairingStats(bundle, mapping.Groups),
		Coverage:      Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		Provenance:    provenance,
	}
	return mapping, audit, nil
}

// sourcesCandidates lists layouts that may hold the source bundle for a
// mapping, most specific first:
//   - our own artifacts (<bundle>/<mapping>/sources.json),
//   - the prototype alignment directory (<alignment>/sources.json),
//   - a GLM-style nested run (<run>/alignment/mapping.json at
//     <bundle>/<mapping>/alignment with <bundle>/sources.json), i.e. two
//     directories above the mapping.
func sourcesCandidates(mappingPath string) []string {
	dir := filepath.Dir(mappingPath)
	candidates := []string{filepath.Join(dir, sourcesSibling)}
	parent := filepath.Dir(dir)
	if !samePath(parent, dir) {
		candidates = append(candidates, filepath.Join(parent, sourcesSibling))
	}
	grandparent := filepath.Dir(parent)
	if !samePath(grandparent, parent) && !samePath(grandparent, dir) {
		candidates = append(candidates, filepath.Join(grandparent, sourcesSibling))
	}
	return candidates
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// resolveSourcesPath finds and verifies the source bundle for a mapping. A file
// whose fingerprint matches the supplied bundle exactly wins; a present but
// mismatched file is an error (never bypassed). Corrupt or unreadable files
// other than "not exist" are reported.
func resolveSourcesPath(bundle Bundle, mappingPath string) (string, error) {
	for _, path := range sourcesCandidates(mappingPath) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read source bundle %q: %w", path, err)
		}
		if err := rejectDuplicateJSONKeys(data); err != nil {
			return "", fmt.Errorf("parse source bundle %q: %w", path, err)
		}
		var sibling Bundle
		decoder := json.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&sibling); err != nil {
			return "", fmt.Errorf("parse source bundle %q: %w", path, err)
		}
		expected, err := bundleDigest(sibling)
		if err != nil {
			return "", err
		}
		if sibling.Fingerprint == "" || sibling.Fingerprint != expected {
			return "", fmt.Errorf("source bundle %q fingerprint mismatch", path)
		}
		// A present source bundle must match exactly; never fall through to a
		// different directory to bypass a mismatched one.
		if sibling.Fingerprint != bundle.Fingerprint {
			return "", fmt.Errorf("source bundle %q does not match the supplied subtitles", path)
		}
		if err := verifySiblingSources(bundle, path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("mapping %q requires a source bundle in the mapping directory or its parent", mappingPath)
}

// verifySiblingSources checks that the given sources.json has the exact cue
// text and times of the bundle supplied to Align.
func verifySiblingSources(bundle Bundle, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read source bundle %q: %w", path, err)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("parse source bundle %q: %w", path, err)
	}
	var sibling Bundle
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&sibling); err != nil {
		return fmt.Errorf("parse source bundle %q: %w", path, err)
	}
	if sibling.Fingerprint != bundle.Fingerprint {
		return fmt.Errorf("source bundle %q does not match the supplied subtitles", path)
	}
	for _, lang := range []string{"en", "zh"} {
		siblingCues, cues := sibling.EN, bundle.EN
		if lang == "zh" {
			siblingCues, cues = sibling.ZH, bundle.ZH
		}
		if len(siblingCues) != len(cues) {
			return fmt.Errorf("sibling source bundle %q has %d %s cues, sources have %d", path, len(siblingCues), lang, len(cues))
		}
		for i := range cues {
			left, right := siblingCues[i], cues[i]
			if left.ID != right.ID || left.Start != right.Start || left.End != right.End ||
				left.OriginalStart != right.OriginalStart || left.OriginalEnd != right.OriginalEnd ||
				left.Display != right.Display || left.Raw != right.Raw {
				return fmt.Errorf("sibling source bundle %q differs from local %s cue %d", path, lang, right.ID)
			}
		}
	}
	return nil
}

// validateImportedGroups revalidates every group as if it were one model
// response covering both complete tracks, then requires exact coverage.
func validateImportedGroups(mapping Mapping, bundle Bundle) error {
	tracks := map[string]map[int]Cue{"en": {}, "zh": {}}
	for _, cue := range bundle.EN {
		tracks["en"][cue.ID] = cue
	}
	for _, cue := range bundle.ZH {
		tracks["zh"][cue.ID] = cue
	}
	uses := map[string]map[int]int{"en": {}, "zh": {}}
	for index, group := range mapping.Groups {
		if group.Layer != LayerTop && group.Layer != LayerBottom {
			return fmt.Errorf("group %d has invalid layer %q", index, group.Layer)
		}
		if group.Origin != OriginModel && group.Origin != OriginRestored && group.Origin != OriginTiming {
			return fmt.Errorf("group %d has invalid origin %q", index, group.Origin)
		}
		if len(group.ENIDs) == 0 && len(group.ZHIDs) == 0 {
			return fmt.Errorf("group %d is empty", index)
		}
		if group.Origin == OriginRestored && len(group.ENIDs)+len(group.ZHIDs) != 1 {
			return fmt.Errorf("restored group %d must contain exactly one cue", index)
		}
		if group.Origin == OriginTiming && (len(group.ENIDs) == 0 || len(group.ZHIDs) == 0) {
			return fmt.Errorf("timing group %d must contain both languages", index)
		}
		if reason := validateRowValues(group, tracks, MaxDistanceMS); reason != "" {
			return fmt.Errorf("group %d: %s", index, reason)
		}
		for _, lang := range []string{"en", "zh"} {
			for _, id := range rowIDs(group, lang) {
				uses[lang][id]++
				if uses[lang][id] > 1 {
					return fmt.Errorf("cue %s:%d appears more than once", lang, id)
				}
			}
		}
		if group.Origin == OriginModel && group.OwnerWindow < 0 {
			return fmt.Errorf("group %d has negative owner_window", index)
		}
	}
	for _, lang := range []string{"en", "zh"} {
		for _, cue := range trackCues(bundle, lang) {
			if uses[lang][cue.ID] != 1 {
				return fmt.Errorf("coverage: %s cue %d is not mapped exactly once", lang, cue.ID)
			}
		}
	}
	return nil
}

// requireModelOwnerWindows enforces owner_window presence on model groups at the
// JSON level, where a numeric zero is still distinguishable from absence.
func requireModelOwnerWindows(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("missing groups")
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return fmt.Errorf("invalid groups: %w", err)
	}
	for index, group := range groups {
		var origin string
		if value, ok := group["origin"]; ok {
			_ = json.Unmarshal(value, &origin)
		}
		if origin == OriginModel {
			if _, ok := group["owner_window"]; !ok {
				return fmt.Errorf("group %d has no owner_window", index)
			}
		}
	}
	return nil
}

func trackCues(bundle Bundle, lang string) []Cue {
	if lang == "zh" {
		return bundle.ZH
	}
	return bundle.EN
}

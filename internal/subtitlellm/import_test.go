package subtitlellm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

func prototypeMappingConfig(t *testing.T) config.LLMConfig {
	t.Helper()
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = filepath.Join(prototypeSources, "alignment", "mapping.json")
	cfg.Model = "group/flash-models"
	return cfg
}

func TestImportPrototypeMapping(t *testing.T) {
	bundle, err := LoadSources(
		filepath.Join(prototypeSources, "en.source.srt"),
		filepath.Join(prototypeSources, "zh.source.srt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg := prototypeMappingConfig(t)
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(mapping.Groups) != 712 {
		t.Fatalf("groups = %d, want 712", len(mapping.Groups))
	}
	if mapping.Fingerprint != "b96a000b0fdd93db02fdec22e4b34abede2ea421997873db3c046739909ab9b9" {
		t.Fatalf("fingerprint = %s", mapping.Fingerprint)
	}
	if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
		t.Fatalf("restored = %v", audit.Restored)
	}
	if audit.Provenance == nil || audit.Provenance.Source != "prototype-import" || audit.Provenance.ModelMismatch {
		t.Fatalf("provenance = %+v", audit.Provenance)
	}
	if audit.Provenance.MappingModel != "group/flash-models" {
		t.Fatalf("mapping model = %q", audit.Provenance.MappingModel)
	}
	assertCoverage(t, mapping, bundle)
}

func TestImportPrototypeMappingReportsModelMismatch(t *testing.T) {
	bundle, err := LoadSources(
		filepath.Join(prototypeSources, "en.source.srt"),
		filepath.Join(prototypeSources, "zh.source.srt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg := prototypeMappingConfig(t)
	cfg.Model = "another-model"
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if audit.Provenance == nil || !audit.Provenance.ModelMismatch {
		t.Fatalf("provenance = %+v", audit.Provenance)
	}
	if audit.Provenance.ConfiguredModel != "another-model" {
		t.Fatalf("configured model = %q", audit.Provenance.ConfiguredModel)
	}
}

// writePrototypeMapping creates a prototype-shaped alignment directory with a
// sibling sources.json and returns the mapping path.
func writePrototypeMapping(t *testing.T, bundle Bundle, groups []Group, mutate func(*Mapping)) string {
	t.Helper()
	root := t.TempDir()
	alignment := filepath.Join(root, "alignment")
	if err := os.MkdirAll(alignment, 0o755); err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, sourcesSibling), string(bundleJSON))

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Parameters: MappingParameters{
			WindowMS: 210000, OverlapMS: 15000, MaxDistanceMS: MaxDistanceMS,
			Model: "test-model", Endpoint: "http://127.0.0.1:1/v1",
			ResponseFormat: "json_object", SchemaEnforcement: schemaEnforcement,
		},
		Groups: groups,
	}
	if mutate != nil {
		mutate(&mapping)
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = fingerprint
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(alignment, "mapping.json")
	writeFile(t, path, string(data))
	return path
}

// writeMappingAt writes a fingerprinted mapping for bundle into dir.
func writeMappingAt(t *testing.T, dir string, bundle Bundle, groups []Group) string {
	t.Helper()
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Parameters: MappingParameters{
			WindowMS: 210000, OverlapMS: 15000, MaxDistanceMS: MaxDistanceMS,
			Model: "test-model", Endpoint: "http://127.0.0.1:1/v1",
			ResponseFormat: "json_object", SchemaEnforcement: schemaEnforcement,
		},
		Groups: groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = fingerprint
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mapping.json")
	writeFile(t, path, string(data))
	return path
}

func importFixture(t *testing.T) Bundle {
	t.Helper()
	en, zh := testCues(2, 1000)
	return testBundle(t, en, zh)
}

func TestImportMappingStrictValidation(t *testing.T) {
	bundle := importFixture(t)
	modelGroups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	cases := []struct {
		name   string
		groups []Group
		mutate func(*Mapping)
		want   string
	}{
		{
			name: "duplicate coverage", groups: []Group{modelGroups[0]},
			want: "coverage",
		},
		{
			name: "duplicate id across groups",
			groups: []Group{
				modelGroups[0],
				{ENIDs: []int{1}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
			},
			want: "appears more than once",
		},
		{
			name:   "unknown id",
			groups: []Group{{ENIDs: []int{9}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0}, modelGroups[1]},
			want:   "id_outside_window",
		},
		{
			name:   "empty group",
			groups: []Group{{ENIDs: []int{}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0}, modelGroups[0], modelGroups[1]},
			want:   "empty",
		},
		{
			name:   "invalid layer",
			groups: []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: "middle", Origin: OriginModel, OwnerWindow: 0}, modelGroups[1]},
			want:   "invalid layer",
		},
		{
			name:   "restored group with too many cues",
			groups: []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginRestored}, modelGroups[0]},
			want:   "exactly one cue",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writePrototypeMapping(t, bundle, tc.groups, tc.mutate)
			cfg := testConfig(t, "http://127.0.0.1:1/v1")
			cfg.MappingPath = path
			_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestImportMappingAcceptsOwnArtifactLayout(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	// Our own persisted layout: artifacts/<bundle>/<mapping>/{sources,mapping}.json.
	dir := filepath.Join(t.TempDir(), "artifacts", "bundle", "mapping")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, sourcesSibling), string(bundleJSON))
	path := writeMappingAt(t, dir, bundle, groups)

	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if audit.Provenance == nil || audit.Provenance.SourcesPath != filepath.Join(dir, sourcesSibling) {
		t.Fatalf("provenance = %+v", audit.Provenance)
	}
}

func TestImportMappingAcceptsGLMNestedSources(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	// GLM prototype layout: <bundle>/glm-run/alignment/mapping.json with
	// sources at <bundle>/sources.json (two directories above the mapping).
	root := t.TempDir()
	alignment := filepath.Join(root, "glm-run", "alignment")
	if err := os.MkdirAll(alignment, 0o755); err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, sourcesSibling), string(bundleJSON))
	path := writeMappingAt(t, alignment, bundle, groups)

	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if audit.Provenance == nil || audit.Provenance.SourcesPath != filepath.Join(root, sourcesSibling) {
		t.Fatalf("provenance = %+v", audit.Provenance)
	}
}

func TestImportMappingRejectsWrongNestedSources(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	root := t.TempDir()
	alignment := filepath.Join(root, "glm-run", "alignment")
	if err := os.MkdirAll(alignment, 0o755); err != nil {
		t.Fatal(err)
	}
	otherEn, otherZh := testCues(2, 1500)
	other := testBundle(t, otherEn, otherZh)
	otherJSON, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, sourcesSibling), string(otherJSON))
	path := writeMappingAt(t, alignment, bundle, groups)
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want nested source mismatch", err)
	}
}

func TestImportMappingOfflineNeedsNoAPIKeyEnv(t *testing.T) {
	bundle, err := LoadSources(
		filepath.Join(prototypeSources, "en.source.srt"),
		filepath.Join(prototypeSources, "zh.source.srt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNSET_IMPORT_KEY", "")
	cfg := prototypeMappingConfig(t)
	cfg.APIKeyEnv = "UNSET_IMPORT_KEY"
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("offline import required an API key: %v", err)
	}
}

func TestImportRejectsDuplicateJSONKeys(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	path := writePrototypeMapping(t, bundle, groups, nil)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(data), `"version":1`, `"version":1,"version":1`, 1)
	writeFile(t, path, forged)
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "duplicate object key") {
		t.Fatalf("error = %v", err)
	}
}

func TestImportMappingRequiresOwnerWindowForModelGroups(t *testing.T) {
	bundle := importFixture(t)
	groups := []any{
		map[string]any{"en_ids": []any{1}, "zh_ids": []any{1}, "layer": "bottom", "origin": "model"},
		map[string]any{"en_ids": []any{2}, "zh_ids": []any{2}, "layer": "bottom", "origin": "model", "owner_window": 0},
	}
	path := writeRawPrototypeMapping(t, bundle, groups)
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "owner_window") {
		t.Fatalf("error = %v, want owner_window error", err)
	}
}

// writeRawPrototypeMapping writes groups exactly as given, computing the
// prototype fingerprint over the same structure.
func writeRawPrototypeMapping(t *testing.T, bundle Bundle, groups []any) string {
	t.Helper()
	root := t.TempDir()
	alignment := filepath.Join(root, "alignment")
	if err := os.MkdirAll(alignment, 0o755); err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, sourcesSibling), string(bundleJSON))

	content := map[string]any{
		"version":            Version,
		"source_fingerprint": bundle.Fingerprint,
		"parameters": map[string]any{
			"window_ms": 210000, "overlap_ms": 15000, "max_distance_ms": MaxDistanceMS,
			"model": "test-model", "endpoint": "http://127.0.0.1:1/v1",
			"response_format": "json_object", "schema_enforcement": schemaEnforcement,
		},
		"groups": groups,
	}
	fingerprint, err := digestValue(content)
	if err != nil {
		t.Fatal(err)
	}
	content["fingerprint"] = fingerprint
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(alignment, "mapping.json")
	writeFile(t, path, string(data))
	return path
}

func TestImportMappingFingerprintMismatch(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	t.Run("wrong fingerprint", func(t *testing.T) {
		path := writePrototypeMapping(t, bundle, groups, nil)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["fingerprint"] = json.RawMessage(`"` + strings.Repeat("0", 64) + `"`)
		forged, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, string(forged))
		cfg := testConfig(t, "http://127.0.0.1:1/v1")
		cfg.MappingPath = path
		if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong source bundle", func(t *testing.T) {
		path := writePrototypeMapping(t, bundle, groups, func(mapping *Mapping) {
			mapping.SourceFingerprint = strings.Repeat("1", 64)
		})
		cfg := testConfig(t, "http://127.0.0.1:1/v1")
		cfg.MappingPath = path
		if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "different source bundle") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing sibling sources", func(t *testing.T) {
		path := writePrototypeMapping(t, bundle, groups, nil)
		if err := os.Remove(filepath.Join(filepath.Dir(filepath.Dir(path)), sourcesSibling)); err != nil {
			t.Fatal(err)
		}
		cfg := testConfig(t, "http://127.0.0.1:1/v1")
		cfg.MappingPath = path
		if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil || !strings.Contains(err.Error(), "requires a source bundle") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("sibling text mismatch", func(t *testing.T) {
		path := writePrototypeMapping(t, bundle, groups, nil)
		siblingPath := filepath.Join(filepath.Dir(filepath.Dir(path)), sourcesSibling)
		var sibling Bundle
		data, err := os.ReadFile(siblingPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &sibling); err != nil {
			t.Fatal(err)
		}
		sibling.EN[0].Display = "forged"
		forged, err := bundleDigest(sibling)
		if err != nil {
			t.Fatal(err)
		}
		sibling.Fingerprint = forged
		data, err = json.Marshal(sibling)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, siblingPath, string(data))
		// The mapping was written for the original fingerprint, so rejection can
		// happen either at mapping/source comparison or at sibling comparison.
		cfg := testConfig(t, "http://127.0.0.1:1/v1")
		cfg.MappingPath = path
		if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil {
			t.Fatal("mismatched sibling sources accepted")
		}
	})
}

func TestImportMappingUnknownFieldsRejected(t *testing.T) {
	bundle := importFixture(t)
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
	}
	path := writePrototypeMapping(t, bundle, groups, nil)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["unexpected"] = true
	forged, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(forged))
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.MappingPath = path
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil {
		t.Fatal("unknown mapping field accepted")
	}
}

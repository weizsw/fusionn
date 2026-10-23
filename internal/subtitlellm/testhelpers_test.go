package subtitlellm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/config"
)

// decodeJSON and prototype event loading are shared test helpers used by the
// render tests; they live here so alignment-owned tests stay self-contained.

func decodeJSON(data []byte, value any) error {
	return json.Unmarshal(data, value)
}

// reboundPrototypeMapping binds the committed Dark Matter mapping to bundle and
// writes it, with the bundle as sources.json, to a temp directory. Load-time
// text cleanup changed the source fingerprint; the groups still refer to the
// same cue IDs. It returns the mapping path for imports and the mapping.
func reboundPrototypeMapping(t *testing.T, bundle Bundle) (string, Mapping) {
	t.Helper()
	path, mapping, err := rebindMapping(filepath.Join(prototypeSources, "alignment", "mapping.json"), bundle, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path, mapping
}

func rebindMapping(mappingPath string, bundle Bundle, dir string) (string, Mapping, error) {
	data, err := os.ReadFile(mappingPath)
	if err != nil {
		return "", Mapping{}, err
	}
	var mapping Mapping
	if err := json.Unmarshal(data, &mapping); err != nil {
		return "", Mapping{}, err
	}
	mapping.SourceFingerprint = bundle.Fingerprint
	if mapping.Parameters.SchemaEnforcement == "" {
		mapping.Parameters.SchemaEnforcement = schemaEnforcement
	}
	if mapping.Fingerprint, err = mappingDigest(mapping); err != nil {
		return "", Mapping{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Mapping{}, err
	}
	for name, value := range map[string]any{"mapping.json": mapping, sourcesSibling: bundle} {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return "", Mapping{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), encoded, 0o644); err != nil {
			return "", Mapping{}, err
		}
	}
	return filepath.Join(dir, "mapping.json"), mapping, nil
}

type prototypeEvent struct {
	Group     int        `json:"group"`
	Strategy  string     `json:"strategy"`
	ENIDs     []int      `json:"en_ids"`
	ZHIDs     []int      `json:"zh_ids"`
	ASSStart  int        `json:"ass_start"`
	ASSEnd    int        `json:"ass_end"`
	Alignment int        `json:"alignment"`
	Position  [2]float64 `json:"position_used"`
	Band      [2]float64 `json:"band"`
}

func readPrototypeEvents(path string) ([]prototypeEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []prototypeEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// confirmAllSplits answers every order question the renderer asks with
// same=true, as the model does for subtitles whose parts follow in order.
func confirmAllSplits(t testing.TB, bundle Bundle, mapping Mapping, cfg config.LLMConfig) Mapping {
	t.Helper()
	known := append([]SplitCheck(nil), mapping.SplitChecks...)
	for round := 0; round < maxSplitCheckRounds; round++ {
		pending, err := collectSplitChecks(bundle, mapping.Groups, cfg, known)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		for _, check := range pending {
			check.Same = true
			known = append(known, check)
		}
	}
	mapping.SplitChecks = known
	return mapping
}

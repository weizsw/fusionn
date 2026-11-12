package subtitlellm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// decodeJSON, the prototype event loaders and the shared ASS dialogue parser
// live here so tests can use them whatever rule they cover.

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

// dialogue is one Dialogue line of a rendered ASS file.
type dialogue struct {
	start, end int
	style      string
	text       string
	an         int
	posX, posY float64
	hasPos     bool
}

var (
	dialoguePattern = regexp.MustCompile(`^Dialogue: 0,([^,]+),([^,]+),([^,]+),[^,]*,[^,]*,[^,]*,[^,]*,[^,]*,(.*)$`)
	posParsePattern = regexp.MustCompile(`\\pos\((-?[\d.]+),(-?[\d.]+)\)`)
	anParsePattern  = regexp.MustCompile(`\\an([1-9])`)
)

// parseDialogues returns the Dialogue lines of a rendered ASS file. Lines that
// are not Dialogue lines are ignored; a malformed time fails the test.
func parseDialogues(t *testing.T, content string) []dialogue {
	t.Helper()
	result := parseASSDialogues(content)
	for _, d := range result {
		if d.start < 0 || d.end < 0 {
			t.Fatalf("bad ASS time in Dialogue line %q", d.text)
		}
	}
	return result
}

// parseASSDialogues is the shared Dialogue-line parser. It skips lines that are
// not well-formed Dialogue lines and reports a malformed time as -1, so helpers
// that return errors can handle it. ParseSerializedASSWithPositions
// (cross_regression_harness_test.go) additionally normalizes the bilingual text
// and rejects malformed override tags, so it keeps its own stricter field
// parsing.
func parseASSDialogues(content string) []dialogue {
	var result []dialogue
	for _, line := range strings.Split(content, "\n") {
		match := dialoguePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		d := dialogue{
			start: parseTimeMS(match[1]),
			end:   parseTimeMS(match[2]),
			style: match[3],
			text:  match[4],
		}
		if pos := posParsePattern.FindStringSubmatch(d.text); pos != nil {
			d.posX, _ = strconv.ParseFloat(pos[1], 64)
			d.posY, _ = strconv.ParseFloat(pos[2], 64)
			d.hasPos = true
		}
		if an := anParsePattern.FindStringSubmatch(d.text); an != nil {
			d.an, _ = strconv.Atoi(an[1])
		}
		result = append(result, d)
	}
	return result
}

// parseTimeMS converts H:MM:SS.cc into milliseconds, or -1 when malformed.
func parseTimeMS(text string) int {
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) != 3 {
		return -1
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	rest := strings.Split(parts[2], ".")
	if len(rest) != 2 {
		return -1
	}
	sec, errS := strconv.Atoi(rest[0])
	cs, errC := strconv.Atoi(rest[1])
	if errH != nil || errM != nil || errS != nil || errC != nil || h < 0 || m < 0 || sec < 0 || cs < 0 {
		return -1
	}
	return ((h*60+m)*60+sec)*1000 + cs*10
}

func review939Cue(id, start, end int, plain string, multi bool) Cue {
	display := strings.Join(sourceLines(plain), " ")
	if multi {
		display = strings.Join(sourceLines(plain), "   ")
	}
	return Cue{ID: id, Start: start, End: end, Plain: plain, Display: display, MultiSpeaker: multi, Alignment: 2}
}

func review939Inputs(t *testing.T, es, zs []Cue, groups []Group) (Bundle, Mapping) {
	t.Helper()
	b := Bundle{Version: Version, EN: es, ZH: zs}
	var err error
	b.Fingerprint, err = bundleDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	m := Mapping{Version: Version, SourceFingerprint: b.Fingerprint, Groups: groups}
	m.Fingerprint, err = mappingDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	return b, m
}

func review939Config() config.LLMConfig {
	return config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
}

func review939Group(es, zs []int) Group {
	return Group{ENIDs: es, ZHIDs: zs, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}
}

func nToOneFixture(t *testing.T) (Bundle, Mapping, config.LLMConfig, config.ASSStyleConfig) {
	t.Helper()
	bundle := Bundle{Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "When our memory pathways get disrupted,", Plain: "When our memory pathways\nget disrupted,"},
			{ID: 2, Start: 4000, End: 7000, Display: "it can lead to things like conflation.", Plain: "it can lead to things like conflation."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1200, End: 6800, Display: "当我们的记忆通道被扰乱时 会导致混淆之类的情况", Plain: "当我们的记忆通道被扰乱时\n会导致混淆之类的情况"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{Version: Version, SourceFingerprint: digest,
		Groups: []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}}}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md
	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	style := config.ASSStyleConfig{Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Go", PrimarySize: 40, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Go", SecondarySize: 30, SecondaryColor: "&H00FFFFFF",
		Outline: 1, OutlineColor: "&H00000000", BackColor: "&H00000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12}
	return bundle, mapping, cfg, style
}

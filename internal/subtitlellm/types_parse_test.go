package subtitlellm

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const prototypeSources = "../../experiments/llm-subtitle-merge/results/dark-matter-s02e07"

func TestLoadSourcesMatchesPrototypeBundle(t *testing.T) {
	bundle, err := LoadSources(
		filepath.Join(prototypeSources, "en.source.srt"),
		filepath.Join(prototypeSources, "zh.source.srt"),
	)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	if len(bundle.EN) != 710 || len(bundle.ZH) != 722 {
		t.Fatalf("cue counts = %d/%d, want 710/722", len(bundle.EN), len(bundle.ZH))
	}

	data, err := os.ReadFile(filepath.Join(prototypeSources, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		EN []Cue `json:"en"`
		ZH []Cue `json:"zh"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	compareCues(t, "en", bundle.EN, cleanedReference(reference.EN))
	compareCues(t, "zh", bundle.ZH, cleanedReference(reference.ZH))
}

// cleanedReference applies the load-time text cleanup to prototype cues: one
// half-width space between words and "- " before a speaker line. The flat
// display text is compared with whitespace collapsed, because long two-speaker
// cues keep their line breaks there.
func cleanedReference(cues []Cue) []Cue {
	out := make([]Cue, len(cues))
	for i, cue := range cues {
		lines := strings.Split(cue.Plain, "\n")
		for j, line := range lines {
			lines[j] = cleanLine(line)
		}
		cue.Plain = strings.Join(lines, "\n")
		cue.Display = strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
		cue.Changes = nil
		out[i] = cue
	}
	return out
}

func compareCues(t *testing.T, lang string, got, want []Cue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s cue count = %d, want %d", lang, len(got), len(want))
	}
	for i := range want {
		cue := got[i]
		cue.Display = strings.Join(strings.Fields(cue.Display), " ")
		cue.Changes = nil
		gotJSON, _ := json.Marshal(cue)
		wantJSON, _ := json.Marshal(want[i])
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("%s cue %d mismatch:\n got %s\nwant %s", lang, i+1, gotJSON, wantJSON)
		}
	}
}

func TestLoadSourcesParsesSRTNatives(t *testing.T) {
	dir := t.TempDir()
	enPath := filepath.Join(dir, "en.srt")
	zhPath := filepath.Join(dir, "zh.srt")
	writeFile(t, enPath, "1\n00:00:01,000 --> 00:00:03,000\n{\\an8}- Hello\n- Goodbye\n\n"+
		"2\n00:00:04,000 --> 00:00:05,000\n<i>Plain</i> &amp; <font color=\"red\">text</font>\n")
	writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
	bundle, err := LoadSources(enPath, zhPath)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	first := bundle.EN[0]
	if first.Alignment != 8 || !first.Top {
		t.Fatalf("native alignment = %d top=%v, want 8/true", first.Alignment, first.Top)
	}
	if !first.MultiSpeaker || first.Display != "- Hello - Goodbye" {
		t.Fatalf("multi speaker=%v display=%q", first.MultiSpeaker, first.Display)
	}
	if !contains(first.Changes, "markup_removed_or_newline_decoded") || !contains(first.Changes, "whitespace_flattened") {
		t.Fatalf("changes = %v", first.Changes)
	}
	second := bundle.EN[1]
	if second.Plain != "Plain & text" || second.Display != "Plain & text" {
		t.Fatalf("plain=%q display=%q", second.Plain, second.Display)
	}
}

func TestLoadSourcesParsesASSNativeStyleAndPosition(t *testing.T) {
	dir := t.TempDir()
	enPath := filepath.Join(dir, "en.ass")
	zhPath := filepath.Join(dir, "zh.srt")
	writeFile(t, enPath, `[Script Info]
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Top, Arial, 20, &H00FFFFFF, &H000000FF, &H00000000, &H00000000, 0, 0, 0, 0, 100, 100, 0, 0, 1, 2, 2, 8, 10, 10, 10, 1
Style: Default, Arial, 20, &H00FFFFFF, &H000000FF, &H00000000, &H00000000, 0, 0, 0, 0, 100, 100, 0, 0, 1, 2, 2, 2, 10, 10, 10, 1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Top,,0,0,0,,Styled top
Dialogue: 0,0:00:04.00,0:00:05.00,Default,,0,0,0,,{\an8}Inline top
Dialogue: 0,0:00:05.00,0:00:06.00,Default,,0,0,0,,{\pos(960,100)}Positioned top
Comment: 0,0:00:06.00,0:00:07.00,Default,,0,0,0,,ignored
`)
	writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
	bundle, err := LoadSources(enPath, zhPath)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	if len(bundle.EN) != 3 {
		t.Fatalf("cue count = %d, want 3 (comments ignored)", len(bundle.EN))
	}
	if bundle.EN[0].Alignment != 8 || !bundle.EN[0].Top || bundle.EN[0].Style != "Top" {
		t.Fatalf("style cue = %+v", bundle.EN[0])
	}
	if bundle.EN[1].Alignment != 8 || !bundle.EN[1].Top {
		t.Fatalf("inline an cue = %+v", bundle.EN[1])
	}
	positioned := bundle.EN[2]
	if positioned.Position == nil || positioned.Position[0] != 960 || positioned.Position[1] != 100 {
		t.Fatalf("position = %v", positioned.Position)
	}
	if !positioned.Top {
		t.Fatal("low position should be top")
	}
}

func TestLoadSourcesMatchesPythonWhitespaceAndTags(t *testing.T) {
	dir := t.TempDir()
	enPath := filepath.Join(dir, "en.srt")
	zhPath := filepath.Join(dir, "zh.srt")
	// U+3000 before the dash still marks a speaker line and is cleaned away,
	// and SRT tags are stripped case-insensitively.
	writeFile(t, enPath, "1\n00:00:01,000 --> 00:00:03,000\n　- Hello\n　- Goodbye <I>x</I>\n\n"+
		"2\n00:00:04,000 --> 00:00:05,000\nfirst line\n\nsecond line\n")
	writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
	bundle, err := LoadSources(enPath, zhPath)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	if !bundle.EN[0].MultiSpeaker {
		t.Fatalf("U+3000 speaker lines not detected: %q", bundle.EN[0].Plain)
	}
	if bundle.EN[0].Plain != "- Hello\n- Goodbye x" {
		t.Fatalf("plain = %q", bundle.EN[0].Plain)
	}
	// A blank line inside cue text is preserved, as pysubs2 does.
	if bundle.EN[1].Plain != "first line\n\nsecond line" {
		t.Fatalf("internal blank line lost: %q", bundle.EN[1].Plain)
	}
}

func TestLoadSourcesRejectsInvalidTime(t *testing.T) {
	dir := t.TempDir()
	enPath := filepath.Join(dir, "en.srt")
	zhPath := filepath.Join(dir, "zh.srt")
	writeFile(t, enPath, "1\n00:00:03,000 --> 00:00:01,000\nbad\n")
	writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
	if _, err := LoadSources(enPath, zhPath); err == nil {
		t.Fatal("expected invalid time error")
	}
}

func TestLoadSourcesRejectsUnsupportedFile(t *testing.T) {
	dir := t.TempDir()
	enPath := filepath.Join(dir, "en.srt")
	zhPath := filepath.Join(dir, "zh.srt")
	writeFile(t, enPath, "not a subtitle\n")
	writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
	if _, err := LoadSources(enPath, zhPath); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestBundleDigestMatchesPrototype(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, OriginalStart: 1000, OriginalEnd: 3000, Raw: "Hello", Plain: "Hello", Display: "Hello", Style: "Default", Alignment: 2},
		{ID: 2, Start: 3000, End: 5000, OriginalStart: 3000, OriginalEnd: 5000, Raw: "World", Plain: "World", Display: "World", Style: "Default", Alignment: 2},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, OriginalStart: 1000, OriginalEnd: 3000, Raw: "你好", Plain: "你好", Display: "你好", Style: "Default", Alignment: 2},
		{ID: 2, Start: 3000, End: 5000, OriginalStart: 3000, OriginalEnd: 5000, Raw: "世界", Plain: "世界", Display: "世界", Style: "Default", Alignment: 2},
	}
	bundle := Bundle{Version: Version, EN: en, ZH: zh}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "10ad9b90780eb14d7a1f0db35f35acdbf6d9fd4aa9b36be57f4c1c1e331ad3bf" {
		t.Fatalf("bundle digest = %s", digest)
	}
	bundle.Fingerprint = digest
	if _, err := ensureBundleFingerprint(bundle); err != nil {
		t.Fatalf("verified bundle rejected: %v", err)
	}
	bundle.EN[0].Display = "changed"
	if _, err := ensureBundleFingerprint(bundle); err == nil {
		t.Fatal("modified bundle accepted")
	}
}

func TestMappingDigestMatchesPrototype(t *testing.T) {
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: "10ad9b90780eb14d7a1f0db35f35acdbf6d9fd4aa9b36be57f4c1c1e331ad3bf",
		Parameters: MappingParameters{
			WindowMS: 210000, OverlapMS: 15000, MaxDistanceMS: 8000,
			Model: "test-model", Endpoint: "http://127.0.0.1:1/v1",
			ResponseFormat: "json_object", SchemaEnforcement: schemaEnforcement,
		},
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
			{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0},
		},
	}
	digest, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "12adb529a2945bade36a09d0415f79feeb0ae1a6881d30b538081d9762a86699" {
		t.Fatalf("mapping digest = %s", digest)
	}
	mapping.Groups[1] = Group{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginRestored}
	digest, err = mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "7b2a7aad529954ba44c5d7910bacb37c79d38e2386ebfbee19f35a04822e0d1a" {
		t.Fatalf("restored mapping digest = %s", digest)
	}
}

func TestPythonFloatRepr(t *testing.T) {
	cases := map[float64]string{
		23.6: "23.6", 1: "1.0", 0.0001: "0.0001", 1e15: "1000000000000000.0",
		1e16: "1e+16", 1e-5: "1e-05", 123456789012345.6: "123456789012345.6",
		2.25: "2.25", 2.35: "2.35", 0: "0.0", math.Copysign(0, -1): "-0.0",
		-12.5: "-12.5",
	}
	for value, want := range cases {
		if got := pythonFloatRepr(value); got != want {
			t.Errorf("pythonFloatRepr(%v) = %q, want %q", value, got, want)
		}
	}
}

func TestGroupJSONPreservesOwnerWindowPresence(t *testing.T) {
	model := Group{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, OwnerWindow: 0}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"owner_window":0`) {
		t.Fatalf("model group JSON = %s", data)
	}
	restored := Group{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginRestored}
	data, err = json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "owner_window") {
		t.Fatalf("restored group JSON = %s", data)
	}
	var decoded Group
	if err := json.Unmarshal([]byte(`{"en_ids":[1],"zh_ids":[],"layer":"bottom","origin":"model","owner_window":3}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OwnerWindow != 3 || !decoded.ownerWindowSet {
		t.Fatalf("decoded owner window = %d set=%v", decoded.OwnerWindow, decoded.ownerWindowSet)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCleanLine(t *testing.T) {
	for in, want := range map[string]string{
		"-  Fire.":         "- Fire.",
		"-Fire.":           "- Fire.",
		"－开火":              "- 开火",
		"–Yes":             "- Yes",
		"— 好":              "- 好",
		"　- Hello":         "- Hello",
		"a   b　c ":         "a b c",
		"well-known  word": "well-known word",
		"--":               "--",
		"——好":              "——好",
		"-5 degrees":       "-5 degrees",
		"-":                "-",
		"":                 "",
	} {
		if got := cleanLine(in); got != want {
			t.Errorf("cleanLine(%q) = %q, want %q", in, got, want)
		}
	}
}

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

// darkMatterFixture returns bundle, mapping, cfg, and style for Dark Matter S02E07.
func darkMatterFixture(t *testing.T) (Bundle, Mapping, config.LLMConfig, config.ASSStyleConfig) {
	t.Helper()
	bundle, err := LoadSources(
		filepath.Join(prototypeSources, "en.source.srt"),
		filepath.Join(prototypeSources, "zh.source.srt"),
	)
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}

	rawMapping, err := os.ReadFile(filepath.Join(prototypeSources, "alignment", "mapping.json"))
	if err != nil {
		t.Fatalf("Read mapping: %v", err)
	}
	var mapping Mapping
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		t.Fatalf("Decode mapping: %v", err)
	}

	cfg := config.LLMConfig{
		BaseURL:        "https://example.com/v1",
		TimeoutSeconds: 30,
		Workers:        1,
		ResponseFormat: config.ResponseFormatJSONObject,
		EnChars:        65,
		ZhChars:        20,
		EnGapMS:        400,
		ZhGapMS:        500,
		SpanMS:         5500,
		FillGapMS:      500,
		Model:          "group/flash-models",
		WindowSeconds:  210,
		OverlapSeconds: 15,
	}

	// Traceable style matching native ASS delivery:
	// Base: PrimarySize 52, SecondarySize 36, MarginV 32, MarginLeft 80, MarginRight 80
	// Adapts on 3840x1920 (scale 1.125) to: 59, 41, MarginV -32, MarginLeft 90, MarginRight 90.
	style := config.ASSStyleConfig{
		Enabled:         true,
		PlayResX:        1920,
		PlayResY:        1080,
		PrimaryFont:     "PingFang SC",
		PrimarySize:     52,
		PrimaryColor:    "&H08EFECEA",
		SecondaryFont:   "Helvetica Neue",
		SecondarySize:   36,
		SecondaryColor:  "&H087BC0E5",
		Bold:            false,
		Outline:         1.4,
		OutlineColor:    "&H30141414",
		Shadow:          1.5,
		BackColor:       "&H80000000",
		MarginV:         32,
		MarginLeft:      80,
		MarginRight:     80,
		Alignment:       2,
		BorderStyle:     1,
		WrapStyle:       "0",
		ScaleX:          100,
		Spacing:         0,
		LineHeightRatio: 1.23,
		BlockPadding:    16,
		CollisionGap:    12,
	}

	return bundle, mapping, cfg, style
}

// 1. 缺字/次序：从新ASS复制件删除一条事件或改一个汉字，验证必须失败；原ASS每个语言源身份、正文及语言内顺序完整。
func TestCrossRegression_1_MissingOrCorruptedText(t *testing.T) {
	bundle, mapping, cfg, style := darkMatterFixture(t)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1920, nil)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}
	assContent := string(assBytes)
	dialogues, err := ParseSerializedASSWithPositions(assContent)
	if err != nil {
		t.Fatalf("ParseSerializedASSWithPositions: %v", err)
	}

	// Positive assertion on genuine output:
	v, err := verifyEpisode(bundle, mapping, cfg, events)
	if err != nil {
		t.Fatalf("verifyEpisode error: %v", err)
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("positive verifyEpisode failed: %v", failures)
	}
	if failures := VerifySerializedDialogueMatchAudits(dialogues, events); len(failures) != 0 {
		t.Fatalf("positive VerifySerializedDialogueMatchAudits failed: %v", failures)
	}

	// Negative assertion 1a: delete one Dialogue event from serialized ASS copy
	{
		lines := strings.Split(assContent, "\n")
		var tamperedLines []string
		deleted := false
		for _, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") && !deleted {
				deleted = true
				continue // skip one dialogue line
			}
			tamperedLines = append(tamperedLines, l)
		}
		tamperedASS := strings.Join(tamperedLines, "\n")
		tamperedDialogues, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatalf("Parse tampered ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDialogues, events)
		if len(failures) == 0 {
			t.Fatal("negative 1a failed: deleting a Dialogue event from ASS was not detected")
		}
		t.Logf("Negative 1a properly detected deletion: %s", failures[0])
	}

	// Negative assertion 1b: alter one Chinese character in serialized ASS copy
	{
		// Find a dialogue line with Chinese text and mutate one character
		lines := strings.Split(assContent, "\n")
		mutated := false
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") && strings.Contains(l, "扫描") {
				lines[i] = strings.Replace(l, "扫描", "检视", 1)
				mutated = true
				break
			}
		}
		if !mutated {
			t.Fatal("fixture did not contain target string 扫描")
		}
		tamperedASS := strings.Join(lines, "\n")
		tamperedDialogues, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatalf("Parse tampered ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDialogues, events)
		if len(failures) == 0 {
			t.Fatal("negative 1b failed: altering a Chinese character in ASS was not detected")
		}
		t.Logf("Negative 1b properly detected character corruption: %s", failures[0])
	}

	// Negative assertion 1d (R1): delete language style separator \N{\rEng} from serialized ASS copy
	// Characters and timing remain identical; verifier must catch the lost language boundary.
	{
		lines := strings.Split(assContent, "\n")
		mutated := false
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") && strings.Contains(l, `\N{\rEng}`) {
				lines[i] = strings.Replace(l, `\N{\rEng}`, "", 1)
				mutated = true
				break
			}
		}
		if !mutated {
			t.Fatal("fixture did not contain target string \\N{\\rEng}")
		}
		tamperedASS := strings.Join(lines, "\n")
		tamperedDialogues, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatalf("Parse tampered ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDialogues, events)
		if len(failures) == 0 {
			t.Fatal("negative 1d (R1) failed: deleting language separator \\N{\\rEng} was NOT detected by VerifySerializedDialogueMatchAudits")
		}
		t.Logf("Negative 1d properly detected separator deletion: %s", failures[0])
	}

	// Negative assertion 1e (R1): delete top bilingual separator \N{\rTopEng}
	{
		lines := strings.Split(assContent, "\n")
		mutated := false
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") && strings.Contains(l, `\N{\rTopEng}`) {
				lines[i] = strings.Replace(l, `\N{\rTopEng}`, "", 1)
				mutated = true
				break
			}
		}
		if !mutated {
			t.Fatal("fixture did not contain target string \\N{\\rTopEng}")
		}
		tamperedASS := strings.Join(lines, "\n")
		tamperedDialogues, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatalf("Parse tampered ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDialogues, events)
		if len(failures) == 0 {
			t.Fatal("negative 1e (R1) failed: deleting top bilingual separator \\N{\\rTopEng} was NOT detected")
		}
		t.Logf("Negative 1e properly detected top bilingual separator deletion: %s", failures[0])
	}

	// Negative assertion 1f (R2): remove space between two English words in serialized ASS copy
	{
		lines := strings.Split(assContent, "\n")
		mutated := false
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") && strings.Contains(l, "I need") {
				lines[i] = strings.Replace(l, "I need", "Ineed", 1)
				mutated = true
				break
			}
		}
		if !mutated {
			t.Fatal("fixture did not contain target string 'I need'")
		}
		tamperedASS := strings.Join(lines, "\n")
		tamperedDialogues, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatalf("Parse tampered ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDialogues, events)
		if len(failures) == 0 {
			t.Fatal("negative 1f (R2) failed: removing space between English words was NOT detected by VerifySerializedDialogueMatchAudits")
		}
		t.Logf("Negative 1f properly detected missing English word space: %s", failures[0])
	}

	// Negative assertion 1c: alter one Chinese character in audit events
	{
		corruptedEvents := make([]renderedEvent, len(events))
		copy(corruptedEvents, events)
		corruptedEvents[0].zhText = strings.Replace(corruptedEvents[0].zhText, "嘿", "哈", 1)
		vCorrupt, err := verifyEpisode(bundle, mapping, cfg, corruptedEvents)
		if err != nil {
			t.Fatalf("verifyEpisode corrupted: %v", err)
		}
		if len(vCorrupt.TextMismatches) == 0 {
			t.Fatal("negative 1c failed: altering Chinese character in audit was not detected by verifyEpisode")
		}
		t.Logf("Negative 1c properly detected text mismatch: %s", vCorrupt.TextMismatches[0])
	}
}

// TestCrossRegression_R1_LanguageCoverage verifies that single-sided EN,
// single-sided ZH, and top bilingual outputs pass validation, and deleting
// their language separator fails.
func TestCrossRegression_R1_LanguageCoverage(t *testing.T) {
	cfg := review939Config()
	style := fixtureStyle()

	// 1. Single-sided English
	{
		en := []Cue{review939Cue(1, 1000, 3000, "English only line.", false)}
		zh := []Cue{}
		groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
		bundle, mapping := review939Inputs(t, en, zh, groups)
		assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
		if err != nil {
			t.Fatalf("render single-side EN: %v", err)
		}
		dials, err := ParseSerializedASSWithPositions(string(assBytes))
		if err != nil {
			t.Fatalf("parse single-side EN: %v", err)
		}
		if failures := VerifySerializedDialogueMatchAudits(dials, events); len(failures) != 0 {
			t.Fatalf("single-side EN positive check failed: %v", failures)
		}
		if dials[0].ZHText != "" || dials[0].ENText == "" {
			t.Fatalf("expected empty ZH and non-empty EN, got ZH=%q EN=%q", dials[0].ZHText, dials[0].ENText)
		}
	}

	// 2. Single-sided Chinese
	{
		en := []Cue{}
		zh := []Cue{review939Cue(1, 1000, 3000, "仅中文对白。", false)}
		groups := []Group{{ENIDs: []int{}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
		bundle, mapping := review939Inputs(t, en, zh, groups)
		assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
		if err != nil {
			t.Fatalf("render single-side ZH: %v", err)
		}
		dials, err := ParseSerializedASSWithPositions(string(assBytes))
		if err != nil {
			t.Fatalf("parse single-side ZH: %v", err)
		}
		if failures := VerifySerializedDialogueMatchAudits(dials, events); len(failures) != 0 {
			t.Fatalf("single-side ZH positive check failed: %v", failures)
		}
		if dials[0].ZHText == "" || dials[0].ENText != "" {
			t.Fatalf("expected non-empty ZH and empty EN, got ZH=%q EN=%q", dials[0].ZHText, dials[0].ENText)
		}
	}

	// 3. Top bilingual
	{
		en := []Cue{{ID: 1, Start: 1000, End: 3000, Plain: "Top English.", Display: "Top English.", Top: true, Alignment: 8}}
		zh := []Cue{{ID: 1, Start: 1000, End: 3000, Plain: "顶部中文。", Display: "顶部中文。", Top: true, Alignment: 8}}
		groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Kind: KindDialogue, Origin: OriginModel}}
		bundle, mapping := review939Inputs(t, en, zh, groups)
		assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
		if err != nil {
			t.Fatalf("render top bilingual: %v", err)
		}
		dials, err := ParseSerializedASSWithPositions(string(assBytes))
		if err != nil {
			t.Fatalf("parse top bilingual: %v", err)
		}
		if failures := VerifySerializedDialogueMatchAudits(dials, events); len(failures) != 0 {
			t.Fatalf("top bilingual positive check failed: %v", failures)
		}
		if dials[0].ZHText == "" || dials[0].ENText == "" {
			t.Fatalf("expected non-empty ZH and EN for top bilingual, got ZH=%q EN=%q", dials[0].ZHText, dials[0].ENText)
		}
		// Negative: deleting \N{\rTopEng} fails
		tampered := strings.Replace(string(assBytes), `\N{\rTopEng}`, "", 1)
		tamperedDials, err := ParseSerializedASSWithPositions(tampered)
		if err != nil {
			t.Fatal(err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDials, events)
		if len(failures) == 0 {
			t.Fatal("negative top bilingual separator deletion failed: was not detected")
		}
		t.Logf("Negative top bilingual detected separator deletion: %s", failures[0])
	}
}

// TestCrossRegression_R2_VisibleTextAndSpacing verifies that normal \N breaks
// and legal \h hard-spaces pass visible text verification, while deleting
// spaces between English words fails.
func TestCrossRegression_R2_VisibleTextAndSpacing(t *testing.T) {
	cfg := review939Config()
	style := fixtureStyle()

	en := []Cue{
		review939Cue(1, 1000, 3000, "Two roads diverged in yellow wood.", false),
	}
	zh := []Cue{
		review939Cue(1, 1000, 3000, "黄色的树林里分出两条路。", false),
	}
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
	bundle, mapping := review939Inputs(t, en, zh, groups)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	originalASS := string(assBytes)

	// 1. Positive check: original ASS passes
	dials, err := ParseSerializedASSWithPositions(originalASS)
	if err != nil {
		t.Fatalf("parse original: %v", err)
	}
	if failures := VerifySerializedDialogueMatchAudits(dials, events); len(failures) != 0 {
		t.Fatalf("original ASS visible text failed: %v", failures)
	}

	// 2. Positive check: legal \h (hard space) expression passes
	{
		hSpaceASS := strings.Replace(originalASS, "Two roads", `Two\hroads`, 1)
		hDials, err := ParseSerializedASSWithPositions(hSpaceASS)
		if err != nil {
			t.Fatalf("parse \\h ASS: %v", err)
		}
		if failures := VerifySerializedDialogueMatchAudits(hDials, events); len(failures) != 0 {
			t.Fatalf("legal \\h hard space should pass, but failed: %v", failures)
		}
	}

	// 3. Positive check: normal \N line break in English passes
	{
		nlASS := strings.Replace(originalASS, "Two roads diverged", `Two roads\Ndiverged`, 1)
		// Corresponding audit event has \n newline
		nlEvents := make([]renderedEvent, len(events))
		copy(nlEvents, events)
		nlEvents[0].enText = strings.Replace(nlEvents[0].enText, "Two roads diverged", "Two roads\ndiverged", 1)

		nlDials, err := ParseSerializedASSWithPositions(nlASS)
		if err != nil {
			t.Fatalf("parse \\N ASS: %v", err)
		}
		if failures := VerifySerializedDialogueMatchAudits(nlDials, nlEvents); len(failures) != 0 {
			t.Fatalf("normal \\N break should pass, but failed: %v", failures)
		}
	}

	// 4. Negative check: deleting space between words fails
	{
		noSpaceASS := strings.Replace(originalASS, "Two roads", "Tworoads", 1)
		badDials, err := ParseSerializedASSWithPositions(noSpaceASS)
		if err != nil {
			t.Fatalf("parse no-space ASS: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(badDials, events)
		if len(failures) == 0 {
			t.Fatal("negative R2 failed: deleting space between English words was not detected")
		}
		t.Logf("Negative R2 properly detected deleted English word space: %s", failures[0])
	}
}

// 2. 时间/身份：复制件使显示越出对应源/英语锚定窗口，或用错误fingerprint导入旧mapping，必须被相应公开入口/验证识别。
func TestCrossRegression_2_WindowOrFingerprintViolation(t *testing.T) {
	bundle, mapping, cfg, style := darkMatterFixture(t)
	_, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1920, nil)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}

	// Positive assertion: baseline interval envelopes match
	v, err := verifyEpisode(bundle, mapping, cfg, events)
	if err != nil {
		t.Fatalf("verifyEpisode error: %v", err)
	}
	if len(v.IntervalMismatches) != 0 {
		t.Fatalf("positive interval check had mismatches: %v", v.IntervalMismatches)
	}

	// Negative assertion 2a: tamper an event time to extend 5000ms past its window
	{
		tamperedEvents := make([]renderedEvent, len(events))
		copy(tamperedEvents, events)
		tamperedEvents[0].assEnd += 5000
		vTampered, err := verifyEpisode(bundle, mapping, cfg, tamperedEvents)
		if err != nil {
			t.Fatalf("verifyEpisode: %v", err)
		}
		if len(vTampered.IntervalMismatches) == 0 {
			t.Fatal("negative 2a failed: extending event window by 5000ms was not detected")
		}
		t.Logf("Negative 2a properly detected interval window mismatch: %s", vTampered.IntervalMismatches[0])
	}

	// Negative assertion 2b: import mapping with wrong SourceFingerprint
	{
		tamperedMapping := mapping
		tamperedMapping.SourceFingerprint = "0000000000000000000000000000000000000000000000000000000000000000"
		tmpDir := t.TempDir()
		mappingData, _ := json.Marshal(tamperedMapping)
		tamperedPath := filepath.Join(tmpDir, "mapping.json")
		_ = os.WriteFile(tamperedPath, mappingData, 0644)

		importCfg := cfg
		importCfg.MappingPath = tamperedPath
		_, _, err := NewEngine().Align(context.Background(), bundle, importCfg)
		if err == nil {
			t.Fatal("negative 2b failed: importing mapping with wrong source fingerprint should fail, but succeeded")
		}
		if !strings.Contains(err.Error(), "different source bundle") {
			t.Fatalf("negative 2b error should mention fingerprint mismatch, got: %v", err)
		}
		t.Logf("Negative 2b properly rejected bad fingerprint: %v", err)
	}
}

// 3. 实际ASS与audit：复制件改变一条Dialogue起止或位置，必须与审计不一致；正例读取实际序列化事件，不能只相信JSON或固定true。
func TestCrossRegression_3_PositionOrTimeInconsistency(t *testing.T) {
	bundle, mapping, cfg, style := darkMatterFixture(t)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1920, nil)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}
	assContent := string(assBytes)
	dialogues, err := ParseSerializedASSWithPositions(assContent)
	if err != nil {
		t.Fatalf("ParseSerializedASSWithPositions: %v", err)
	}

	// Positive check: all parsed dialogues match audit events exactly
	if failures := VerifySerializedDialogueMatchAudits(dialogues, events); len(failures) != 0 {
		t.Fatalf("positive VerifySerializedDialogueMatchAudits failed: %v", failures)
	}

	// Negative assertion 3a: tamper start/end time of one line in serialized ASS
	{
		lines := strings.Split(assContent, "\n")
		tampered := false
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue: 0,0:00:23.56,0:00:25.94,") {
				lines[i] = strings.Replace(l, "0:00:25.94", "0:00:29.00", 1)
				tampered = true
				break
			}
		}
		if !tampered {
			t.Fatal("could not find target line to tamper time")
		}
		tamperedASS := strings.Join(lines, "\n")
		dials, err := ParseSerializedASSWithPositions(tamperedASS)
		if err != nil {
			t.Fatal(err)
		}
		failures := VerifySerializedDialogueMatchAudits(dials, events)
		if len(failures) == 0 {
			t.Fatal("negative 3a failed: tampering time in serialized ASS was not detected")
		}
		t.Logf("Negative 3a properly detected time mismatch: %s", failures[0])
	}

	// Negative assertion 3b (R3): tamper real ASS with illegal coordinate syntax \pos(x,y,z)
	{
		lines := strings.Split(assContent, "\n")
		targetLine := -1
		for i, l := range lines {
			if strings.HasPrefix(l, "Dialogue:") {
				targetLine = i
				break
			}
		}
		if targetLine == -1 {
			t.Fatal("fixture had no Dialogue lines")
		}
		lines[targetLine] = strings.Replace(lines[targetLine], ",Default,,0,0,0,,", `,Default,,0,0,0,,{\pos(960,500,100)}`, 1)
		tamperedASS := strings.Join(lines, "\n")
		_, err := ParseSerializedASSWithPositions(tamperedASS)
		if err == nil {
			t.Fatal("negative 3b (R3) failed: illegal 3-arg coordinate syntax \\pos(960,500,100) in real ASS was not detected as an error")
		}
		t.Logf("Negative 3b properly rejected illegal pos syntax: %v", err)
	}

	// Negative assertion 3c: tamper alignment \an
	{
		dialsCopy := make([]SerializedDialogue, len(dialogues))
		copy(dialsCopy, dialogues)
		dialsCopy[0].Alignment = 8 // switch bottom 2 to top 8
		failures := VerifySerializedDialogueMatchAudits(dialsCopy, events)
		if len(failures) == 0 {
			t.Fatal("negative 3c failed: tampering alignment was not detected")
		}
		t.Logf("Negative 3c properly detected alignment mismatch: %s", failures[0])
	}
}

// TestCrossRegression_R3_CoordinateVerification tests that:
// 1. Changing valid 2-arg coordinates to noticeably different values fails verification.
// 2. Corrupting coordinate syntax into illegal 3-arg or non-numeric forms fails parsing.
// 3. Similar words in plain visible text (e.g. "positive", "superposition") are not mistaken for tags.
func TestCrossRegression_R3_CoordinateVerification(t *testing.T) {
	cfg := review939Config()
	style := fixtureStyle()

	pos := [2]float64{960, 200}
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "Explicit pos line.", Display: "Explicit pos line.", Alignment: 2, Position: &pos},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "明确坐标行。", Display: "明确坐标行。", Alignment: 2, Position: &pos},
	}
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
	bundle, mapping := review939Inputs(t, en, zh, groups)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assContent := string(assBytes)

	// Positive check: parses and verifies with HasPos=true
	dialogues, err := ParseSerializedASSWithPositions(assContent)
	if err != nil {
		t.Fatalf("parse positive: %v", err)
	}
	if len(dialogues) != 1 || !dialogues[0].HasPos {
		t.Fatalf("expected 1 dialogue with HasPos=true, got %+v", dialogues)
	}
	if failures := VerifySerializedDialogueMatchAudits(dialogues, events); len(failures) != 0 {
		t.Fatalf("positive verify failed: %v", failures)
	}

	// Negative 1: mutate valid 2-arg coordinates to noticeably different coordinates
	{
		tampered := strings.Replace(assContent, `\pos(960,200)`, `\pos(960,500)`, 1)
		tamperedDials, err := ParseSerializedASSWithPositions(tampered)
		if err != nil {
			t.Fatalf("parse tampered 2-arg coords: %v", err)
		}
		failures := VerifySerializedDialogueMatchAudits(tamperedDials, events)
		if len(failures) == 0 {
			t.Fatal("negative R3-1 failed: mutating valid 2-arg coordinate to (960, 500) was not detected")
		}
		t.Logf("Negative R3-1 properly detected coordinate mutation: %s", failures[0])
	}

	// Negative 2: corrupt into illegal 3-arg syntax
	{
		tampered := strings.Replace(assContent, `\pos(960,200)`, `\pos(960,200,100)`, 1)
		_, err := ParseSerializedASSWithPositions(tampered)
		if err == nil {
			t.Fatal("negative R3-2 failed: illegal 3-arg coordinate syntax was not rejected")
		}
		t.Logf("Negative R3-2 properly rejected 3-arg syntax: %v", err)
	}

	// Negative 3: corrupt into illegal non-numeric syntax
	{
		tampered := strings.Replace(assContent, `\pos(960,200)`, `\pos(abc,def)`, 1)
		_, err := ParseSerializedASSWithPositions(tampered)
		if err == nil {
			t.Fatal("negative R3-3 failed: illegal non-numeric coordinate syntax was not rejected")
		}
		t.Logf("Negative R3-3 properly rejected non-numeric syntax: %v", err)
	}

	// Positive 4: visible text containing "positive" and "superposition" is not mistaken for tags
	{
		plainEn := []Cue{
			review939Cue(1, 1000, 3000, "A superposition state with positive energy.", false),
		}
		plainZh := []Cue{
			review939Cue(1, 1000, 3000, "具有正能量的叠加状态。", false),
		}
		groupsPlain := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
		bundleP, mappingP := review939Inputs(t, plainEn, plainZh, groupsPlain)
		assBytesP, eventsP, err := renderWithAudits(bundleP, mappingP, cfg, style, 1920, 1080, nil)
		if err != nil {
			t.Fatalf("render plain text: %v", err)
		}
		dialsP, err := ParseSerializedASSWithPositions(string(assBytesP))
		if err != nil {
			t.Fatalf("parse plain text: %v", err)
		}
		if dialsP[0].HasPos {
			t.Fatalf("plain text with 'positive' was incorrectly parsed as having \\pos tag: %+v", dialsP[0])
		}
		if failures := VerifySerializedDialogueMatchAudits(dialsP, eventsP); len(failures) != 0 {
			t.Fatalf("verify plain text failed: %v", failures)
		}
	}
}

// TestCrossRegression_R4_PositionTagPresenceAndReference tests that:
// 1. VerifySerializedDialogueMatchAudits has an evidence gap (cannot detect deleted \pos because renderedEvent lacks hasPos).
// 2. VerifyDeliveredASSAgainstReference reads actual delivered ASS and detects deleted \pos tag.
// 3. Normal delivered ASS without \pos passes verification.
// 4. Injecting unexpected \pos into normal output is detected.
func TestCrossRegression_R4_PositionTagPresenceAndReference(t *testing.T) {
	cfg := review939Config()
	style := fixtureStyle()
	tmpDir := t.TempDir()

	pos := [2]float64{960, 200}
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "Positioned cue.", Display: "Positioned cue.", Alignment: 2, Position: &pos},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "有位置的字幕。", Display: "有位置的字幕。", Alignment: 2, Position: &pos},
	}
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
	bundle, mapping := review939Inputs(t, en, zh, groups)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assPath := filepath.Join(tmpDir, "test-r4-delivered.ass")
	if err := os.WriteFile(assPath, assBytes, 0644); err != nil {
		t.Fatalf("write ass file: %v", err)
	}

	// 1. Positive check on actual delivered file
	failures, err := VerifyDeliveredASSAgainstReference(assPath, bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("VerifyDeliveredASSAgainstReference: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("positive delivered ASS verify failed: %v", failures)
	}

	// 2. Prove evidence gap in VerifySerializedDialogueMatchAudits:
	// Deleting \pos tag completely bypasses VerifySerializedDialogueMatchAudits
	tamperedBytes := []byte(strings.Replace(string(assBytes), `\pos(960,200)`, "", 1))
	tamperedDials, err := ParseSerializedASSWithPositions(string(tamperedBytes))
	if err != nil {
		t.Fatalf("parse tampered: %v", err)
	}
	gapFailures := VerifySerializedDialogueMatchAudits(tamperedDials, events)
	if len(gapFailures) != 0 {
		t.Fatalf("expected evidence gap where audit verifier cannot detect missing \\pos tag, but got: %v", gapFailures)
	}
	t.Log("Demonstrated evidence gap: VerifySerializedDialogueMatchAudits alone passed despite missing \\pos tag")

	// 3. Negative check with reference: deleting \pos tag is detected
	tamperedPath := filepath.Join(tmpDir, "test-r4-tampered.ass")
	if err := os.WriteFile(tamperedPath, tamperedBytes, 0644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}
	refFailures, err := VerifyDeliveredASSAgainstReference(tamperedPath, bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("VerifyDeliveredASSAgainstReference on tampered: %v", err)
	}
	if len(refFailures) == 0 {
		t.Fatal("negative R4 failed: deleting \\pos tag was not detected by VerifyDeliveredASSAgainstReference")
	}
	t.Logf("Negative R4 properly detected missing \\pos tag: %s", refFailures[0])

	// 4. Positive check: normal output without \pos passes reference check
	normalEn := []Cue{review939Cue(1, 1000, 3000, "Normal line.", false)}
	normalZh := []Cue{review939Cue(1, 1000, 3000, "普通对白。", false)}
	groupsNormal := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel}}
	bundleN, mappingN := review939Inputs(t, normalEn, normalZh, groupsNormal)
	normalBytes, _, err := renderWithAudits(bundleN, mappingN, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatalf("render normal: %v", err)
	}
	normalPath := filepath.Join(tmpDir, "test-r4-normal.ass")
	if err := os.WriteFile(normalPath, normalBytes, 0644); err != nil {
		t.Fatalf("write normal: %v", err)
	}
	normalFailures, err := VerifyDeliveredASSAgainstReference(normalPath, bundleN, mappingN, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("verify normal: %v", err)
	}
	if len(normalFailures) != 0 {
		t.Fatalf("normal ASS without pos failed reference verification: %v", normalFailures)
	}

	// 5. Negative check: injecting unexpected \pos into normal output is detected
	injectedBytes := []byte(strings.Replace(string(normalBytes), ",Default,,0,0,0,,", `,Default,,0,0,0,,{\pos(960,100)}`, 1))
	injectedPath := filepath.Join(tmpDir, "test-r4-injected.ass")
	if err := os.WriteFile(injectedPath, injectedBytes, 0644); err != nil {
		t.Fatalf("write injected: %v", err)
	}
	injectedFailures, err := VerifyDeliveredASSAgainstReference(injectedPath, bundleN, mappingN, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("verify injected: %v", err)
	}
	if len(injectedFailures) == 0 {
		t.Fatal("negative R4 failed: injecting unexpected \\pos was not detected")
	}
	t.Logf("Negative R4 properly detected unexpected \\pos tag: %s", injectedFailures[0])
}

// 4. 可见行与未知分类：报告同时发生的全部事件的top/bottom可见行数、所有单边/保持/超过两行组及原因，不只统计每条Dialogue的换行；缺kind的旧映射不得虚构分类或据此迁移。
func TestCrossRegression_4_ConcurrentVisibleLinesAndMissingKind(t *testing.T) {
	bundle, mapping, cfg, style := darkMatterFixture(t)
	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1920, nil)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}
	assContent := string(assBytes)
	dialogues, err := ParseSerializedASSWithPositions(assContent)
	if err != nil {
		t.Fatalf("ParseSerializedASSWithPositions: %v", err)
	}

	// Positive assertion on real Dark Matter output:
	report := AnalyzeConcurrentVisibleLines(dialogues, events, mapping)
	t.Logf("Real Dark Matter Analysis: %s", report.Summary)
	t.Logf("One-sided groups count: %d", len(report.OneSidedGroups))
	t.Logf("Hold groups count: %d", len(report.HoldGroups))
	t.Logf("Max Top lines: %d, Max Bottom lines: %d", report.MaxTopLines, report.MaxBottomLines)

	// Verify that all 712 groups in mapping have no unexpected kind (old mapping compatibility)
	if len(report.UnexpectedKindGroups) != 0 {
		t.Fatalf("found unexpected kind in groups: %v", report.UnexpectedKindGroups)
	}
	for i, g := range mapping.Groups {
		if g.Kind != "" {
			t.Fatalf("group %d has non-empty kind %q; old mapping must have empty kind", i, g.Kind)
		}
	}

	// Negative assertion 4a: simultaneous overlapping lines detection catches >2 lines when simulated
	{
		// Simulate two simultaneous bottom events of 2 lines each at 1000-2000ms
		simDialogues := []SerializedDialogue{
			{Start: 1000, End: 2000, Alignment: 2, ZHLines: 1, ENLines: 1, RawText: "line1\\N{\\rEng}line2"},
			{Start: 1000, End: 2000, Alignment: 2, ZHLines: 1, ENLines: 1, RawText: "line3\\N{\\rEng}line4"},
		}
		simReport := AnalyzeConcurrentVisibleLines(simDialogues, nil, Mapping{})
		if simReport.MaxBottomLines != 4 {
			t.Fatalf("negative 4a failed: expected 4 simultaneous bottom lines, got %d", simReport.MaxBottomLines)
		}
		if len(simReport.OverTwoLinesSpans) == 0 {
			t.Fatal("negative 4a failed: overlapping events resulting in 4 lines were not reported in OverTwoLinesSpans")
		}
		t.Logf("Negative 4a properly caught concurrent overlap of %d lines across %d events",
			simReport.MaxBottomLines, len(simDialogues))
	}

	// Negative assertion 4b: flag fabricated kind
	{
		tamperedMapping := mapping
		tamperedMapping.Groups = make([]Group, len(mapping.Groups))
		copy(tamperedMapping.Groups, mapping.Groups)
		tamperedMapping.Groups[0].Kind = "fabricated_alien_kind"
		badReport := AnalyzeConcurrentVisibleLines(dialogues, events, tamperedMapping)
		if len(badReport.UnexpectedKindGroups) == 0 {
			t.Fatal("negative 4b failed: fabricated kind was not detected")
		}
		t.Logf("Negative 4b properly caught unexpected kind in group %d", badReport.UnexpectedKindGroups[0])
	}
}

// 5. 格式/测宽兼容：复用已有SRT/ASS实际parser的行为测试与缺省字段回归；有效原分行、真实双人标记、未知style/缺字体时的保守回退。
func TestCrossRegression_5_FormatAndFontFallback(t *testing.T) {
	bundle, _, _, _ := darkMatterFixture(t)

	// 5a. Source cue integrity and counts
	if len(bundle.EN) != 710 || len(bundle.ZH) != 722 {
		t.Fatalf("expected 710 EN and 722 ZH cues, got %d and %d", len(bundle.EN), len(bundle.ZH))
	}
	if bundle.Fingerprint != "14ec9eb49624d94352152b8fa0cc0b6bacc81228c66919d9e6d4731f20018bd9" {
		t.Fatalf("expected fingerprint 14ec9eb4..., got %s", bundle.Fingerprint)
	}

	// 5b. Genuine multi-speaker evidence preserved
	enCue23 := bundle.EN[22] // id 23
	if enCue23.ID != 23 {
		t.Fatalf("cue 22 index expected ID 23, got %d", enCue23.ID)
	}
	if !enCue23.MultiSpeaker {
		t.Fatalf("EN cue 23 should have multi_speaker=true, got false")
	}
	if !strings.Contains(enCue23.Plain, "- What?\n- I'm sorry. It was my fault.") {
		t.Fatalf("EN cue 23 plain text mismatch: %q", enCue23.Plain)
	}

	zhCue24 := bundle.ZH[23] // id 24
	if zhCue24.ID != 24 {
		t.Fatalf("cue 23 index expected ID 24, got %d", zhCue24.ID)
	}
	if !zhCue24.MultiSpeaker {
		t.Fatalf("ZH cue 24 should have multi_speaker=true, got false")
	}
	if !strings.Contains(zhCue24.Plain, "- 怎么了？\n- 对不起 这都怪我") {
		t.Fatalf("ZH cue 24 plain text mismatch: %q", zhCue24.Plain)
	}

	// 5c. Negative check: parser rejects invalid time format
	{
		invalidSRT := "1\n99:99:99,999 --> 00:00:01,000\nHello\n\n"
		tmpFile := filepath.Join(t.TempDir(), "invalid.srt")
		_ = os.WriteFile(tmpFile, []byte(invalidSRT), 0644)
		validSRT := filepath.Join(prototypeSources, "zh.source.srt")
		if _, err := LoadSources(tmpFile, validSRT); err == nil {
			t.Fatal("negative 5c failed: LoadSources should reject invalid time in SRT, but succeeded")
		} else {
			t.Logf("Negative 5c properly rejected invalid SRT time: %v", err)
		}
	}

	// 5d. Missing font fallback: conservative fallback preserves full text without crash or cutting
	{
		w, ok := MeasureTextWidth("测试文字", "NonExistentFont12345", 52, 100, 0, false, "")
		if ok {
			t.Fatalf("MeasureTextWidth should return false for nonexistent font, got width %f", w)
		}
		t.Logf("Negative 5d properly returned false for nonexistent font")
	}
}

// TestCrossRegression_ReplayRecordedResponsesHTTP verifies the offline replay of the 19 recorded responses
// over an internal HTTP server through Engine.Align.
func TestCrossRegression_ReplayRecordedResponsesHTTP(t *testing.T) {
	bundle, wantMapping, cfg, _ := darkMatterFixture(t)
	requestsDir := filepath.Join(prototypeSources, "alignment", "requests")

	replayedMapping, audit, err := ReplayRecordedResponsesHTTP(context.Background(), bundle, cfg, requestsDir)
	if err != nil {
		t.Fatalf("ReplayRecordedResponsesHTTP failed: %v", err)
	}

	if len(replayedMapping.Groups) != len(wantMapping.Groups) {
		t.Fatalf("replayed groups count %d != want %d", len(replayedMapping.Groups), len(wantMapping.Groups))
	}
	if audit.Provenance == nil || audit.Provenance.Source != "recorded-response-replay" {
		t.Fatalf("audit provenance source mismatch: %+v", audit.Provenance)
	}
	t.Logf("Replayed 19 windows successfully: %d groups, source: %s, coverage: %+v",
		len(replayedMapping.Groups), audit.Provenance.Source, audit.Coverage)
}

// TestCrossRegression_GenerateAndVerifyArtifacts tests the full pipeline execution on Dark Matter S02E07,
// ensuring that all independent regression artifacts are produced and strictly verified.
func TestCrossRegression_GenerateAndVerifyArtifacts(t *testing.T) {
	dir := prototypeSources
	outDir := os.Getenv("FUSIONN_REGRESSION_OUT_DIR")
	if outDir == "" {
		outDir = filepath.Join(t.TempDir(), "artifacts")
	}
	result, err := RunDarkMatterCrossRegressionToDir(dir, outDir)
	if err != nil {
		t.Fatalf("RunDarkMatterCrossRegression failed: %v", err)
	}

	if !result.Report.AllChecksPassed {
		t.Fatalf("Regression validation had failures: %v", result.Report.Failures)
	}

	// Verify all artifact files were created on disk and are non-empty
	for _, p := range []string{
		result.ASSPath,
		result.ImportedAuditPath,
		result.RecomputedMappingPath,
		result.RecomputedAuditPath,
		result.ValidationReportPath,
		result.ComparisonSummaryPath,
		result.MarkdownSummaryPath,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("artifact %s not created: %v", p, err)
		}
		if info.Size() == 0 {
			t.Fatalf("artifact %s is empty", p)
		}
	}

	// Verify old mapping import vs recomputed mapping match 100%
	if !result.Comparison.GroupsMatch {
		t.Fatalf("imported vs recomputed groups mismatch: %v", result.Comparison.Differences)
	}

	t.Logf("Cross regression verified and artifacts emitted successfully: Total dialogues = %d, Top = %d, Bottom = %d, Max top lines = %d, Max bottom lines = %d",
		result.Report.RenderStats.TotalDialogues,
		result.Report.RenderStats.TopDialogues,
		result.Report.RenderStats.BottomDialogues,
		result.Report.RenderStats.MaxTopLines,
		result.Report.RenderStats.MaxBottomLines,
	)
}

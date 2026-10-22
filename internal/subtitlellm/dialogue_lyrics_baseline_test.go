package subtitlellm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// --- Baseline Old-Red Repro Tests (proving 93def95 failures) ---

// TestBaselineOldRed_Group48CounterpartJoin verifies that 93def95 fails to join the
// short English counterpart of a native two-speaker cue, instead splitting it into N:1 slices.
func TestBaselineOldRed_Group48CounterpartJoin(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 199867, End: 200950, Plain: "Oh, yeah?", Display: "Oh, yeah?"},
			{ID: 2, Start: 200951, End: 201994, Plain: "Yep.", Display: "Yep."},
		},
		ZH: []Cue{
			{ID: 1, Start: 199867, End: 201994, Plain: "- 是吗？\n- 是", Display: "- 是吗？   - 是", MultiSpeaker: true},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
	style := renderTestStyle()

	result, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected exactly 1 joined dialogue event for counterpart join, got %d: %v", len(dialogues), dialogues)
	}
	d := dialogues[0]
	if !strings.Contains(d.text, "Oh, yeah?") || !strings.Contains(d.text, "Yep.") {
		t.Fatalf("expected joined English containing both phrases, got %q", d.text)
	}
	if !strings.Contains(d.text, "是吗？") || !strings.Contains(d.text, "是") {
		t.Fatalf("expected intact Chinese dialogue, got %q", d.text)
	}
}

// TestBaselineOldRed_SilentNightMixedCue verifies that 93def95 fails to separate
// explicit lyrics from spoken dialogue in a mixed cue (EN96 / ZH83).
func TestBaselineOldRed_SilentNightMixedCue(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{
				ID:           1,
				Start:        416083,
				End:          419919,
				Raw:          `- {\i1}♪ Silent night ♪{\i0}\N- Yeah, thank you.`,
				Plain:        "- ♪ Silent night ♪\n- Yeah, thank you.",
				Display:      "- ♪ Silent night ♪   - Yeah, thank you.",
				MultiSpeaker: true,
			},
		},
		ZH: []Cue{
			{
				ID:      1,
				Start:   418752,
				End:     419962,
				Plain:   "好 谢谢",
				Display: "好 谢谢",
			},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 1},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
	style := renderTestStyle()

	result, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	var lyricEvent, spokenEvent *dialogue
	for i := range dialogues {
		d := &dialogues[i]
		if strings.Contains(d.text, "Silent night") {
			lyricEvent = d
		}
		if strings.Contains(d.text, "好 谢谢") {
			spokenEvent = d
		}
	}

	if lyricEvent == nil {
		t.Fatalf("expected lyric event with Silent night, got none in %v", dialogues)
	}
	if spokenEvent == nil {
		t.Fatalf("expected spoken event with 好 谢谢, got none in %v", dialogues)
	}
	if lyricEvent == spokenEvent {
		t.Fatalf("expected lyric and spoken parts to be separated into distinct events, but got merged: %q", lyricEvent.text)
	}
	// Lyric should be at the top
	if lyricEvent.an < 7 && !strings.Contains(lyricEvent.style, "Top") && !strings.Contains(lyricEvent.text, `\an8`) {
		t.Fatalf("expected lyric event at top, got style %q an %d text %q", lyricEvent.style, lyricEvent.an, lyricEvent.text)
	}
	// Spoken event should not start before 418750
	if spokenEvent.start < 418750 {
		t.Fatalf("expected spoken Chinese event not before 418750ms, got %dms", spokenEvent.start)
	}
}

// TestBaselineOldRed_BilingualLyricsTop verifies that 93def95 fails to place bilingual lyrics at the top.
func TestBaselineOldRed_BilingualLyricsTop(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1046760, End: 1050300, Plain: "♪ I hear a wolf ♪", Display: "♪ I hear a wolf ♪"},
		},
		ZH: []Cue{
			{ID: 1, Start: 1046760, End: 1050300, Plain: "我听到野狼", Display: "我听到野狼"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 2},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
	style := renderTestStyle()

	result, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 dialogue event, got %d", len(dialogues))
	}
	d := dialogues[0]
	if d.an < 7 && !strings.Contains(d.style, "Top") && !strings.Contains(d.text, `\an8`) {
		t.Fatalf("expected bilingual lyric at top, got style %q an %d text %q", d.style, d.an, d.text)
	}
}

// --- Detailed Regression and Boundary Suites ---

// TestDialogue_SymmetricLanguageCounterpartJoin verifies language symmetry:
// single native English two-speaker cue allows Chinese counterpart short join.
func TestDialogue_SymmetricLanguageCounterpartJoin(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 3000, Plain: "- Yes?\n- No.", Display: "- Yes?   - No.", MultiSpeaker: true},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 1900, Plain: "是吗？", Display: "是吗？"},
			{ID: 2, Start: 2000, End: 3000, Plain: "不是", Display: "不是"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 joined dialogue event for symmetric Chinese counterpart join, got %d: %v", len(dialogues), dialogues)
	}
	d := dialogues[0]
	if !strings.Contains(d.text, "是吗？") || !strings.Contains(d.text, "不是") {
		t.Fatalf("expected joined Chinese text, got %q", d.text)
	}
	if !strings.Contains(d.text, "Yes?") || !strings.Contains(d.text, "No.") {
		t.Fatalf("expected intact English dialogue, got %q", d.text)
	}
}

// TestDialogue_G26Real0131OverCapChineseKeepsThreeLines verifies that G26 (01:31)
// joins the English counterpart into 1 line while keeping Chinese native 2 lines (total 3 lines).
func TestDialogue_G26Real0131OverCapChineseKeepsThreeLines(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 91258, End: 92884, Plain: "The men who killed\nmy friends and family...", Display: "The men who killed my friends and family..."},
			{ID: 2, Start: 92885, End: 94052, Plain: "Please stop.", Display: "Please stop."},
		},
		ZH: []Cue{
			{ID: 1, Start: 91759, End: 94052, Plain: "- 杀死我的朋友和家人的人…\n- 不要！", Display: "- 杀死我的朋友和家人的人…   - 不要！", MultiSpeaker: true},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 joined event for G26, got %d: %v", len(dialogues), dialogues)
	}
	d := dialogues[0]
	// Must contain both English parts joined
	if !strings.Contains(d.text, "The men who killed") || !strings.Contains(d.text, "Please stop.") {
		t.Fatalf("expected joined English, got %q", d.text)
	}
	// Must keep both Chinese lines intact without time-splitting
	if !strings.Contains(d.text, "- 杀死我的朋友和家人的人…") || !strings.Contains(d.text, "- 不要！") {
		t.Fatalf("expected intact Chinese 2-speaker lines, got %q", d.text)
	}
	// Total lines should be 3 (Chinese 2 lines + English 1 line)
	lines := strings.Split(d.text, `\N`)
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 lines (2 Chinese + 1 English), got %d lines: %v", len(lines), lines)
	}
}

// TestDialogue_Negative_OverCapCounterpartDoesNotJoin verifies that if the counterpart
// exceeds the configured cap, short join is refused.
func TestDialogue_Negative_OverCapCounterpartDoesNotJoin(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 2000, Plain: "This is a very long English sentence that is already quite long indeed.", Display: "This is a very long English sentence that is already quite long indeed."},
			{ID: 2, Start: 2100, End: 3000, Plain: "And this second English sentence makes the total length far exceed sixty-five characters.", Display: "And this second English sentence makes the total length far exceed sixty-five characters."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 3000, Plain: "- 对白甲\n- 对白乙", Display: "- 对白甲   - 对白乙", MultiSpeaker: true},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	// Must NOT be joined into 1 event; should remain separate N:1 slices
	if len(dialogues) == 1 {
		t.Fatalf("expected over-cap English counterpart not to join into 1 event, but got 1: %v", dialogues[0].text)
	}
}

// TestDialogue_Negative_OverGapCounterpartDoesNotJoin verifies that counterpart gap > limit rejects join.
func TestDialogue_Negative_OverGapCounterpartDoesNotJoin(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 2000, Plain: "First phrase.", Display: "First phrase."},
			// Gap is 2500 - 2000 = 500ms > EnGapMS (400ms)
			{ID: 2, Start: 2500, End: 3500, Plain: "Second phrase.", Display: "Second phrase."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 3500, Plain: "- 话一\n- 话二", Display: "- 话一   - 话二", MultiSpeaker: true},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) == 1 {
		t.Fatalf("expected over-gap English counterpart not to join, but got 1 event: %v", dialogues[0].text)
	}
}

// TestDialogue_Negative_MultipleMultiSpeakerCuesNotConcatenated verifies that multiple
// MultiSpeaker cues on the same side are NEVER concatenated.
func TestDialogue_Negative_MultipleMultiSpeakerCuesNotConcatenated(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 2000, Plain: "- Person A\n- Person B", Display: "- Person A   - Person B", MultiSpeaker: true},
			{ID: 2, Start: 2100, End: 3000, Plain: "- Person C\n- Person D", Display: "- Person C   - Person D", MultiSpeaker: true},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 3000, Plain: "中文一", Display: "中文一"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) == 1 {
		t.Fatalf("multiple MultiSpeaker cues must never be concatenated together into 1 event, got: %v", dialogues[0].text)
	}
}

// TestLyrics_OpenEndedMusicMarkerTop verifies that a cue with only an opening ♪ and no closing
// symbol is still correctly recognized as explicit lyrics and placed at the top.
func TestLyrics_OpenEndedMusicMarkerTop(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 3000, Plain: "♪ sing Alleluia...", Display: "♪ sing Alleluia..."},
		},
		ZH: []Cue{},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 event, got %d", len(dialogues))
	}
	d := dialogues[0]
	if d.an < 7 && !strings.Contains(d.style, "Top") && !strings.Contains(d.text, `\an8`) {
		t.Fatalf("open-ended lyric must be placed at top, got style %q an %d text %q", d.style, d.an, d.text)
	}
}

// TestLyrics_ItalicsWithoutMusicSymbolStaysBottom verifies that italics alone (without ♪)
// does not cause a subtitle to be classified as a lyric or moved to top.
func TestLyrics_ItalicsWithoutMusicSymbolStaysBottom(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 3000, Raw: `{\i1}Previously on The Terminal List...{\i0}`, Plain: "Previously on The Terminal List...", Display: "Previously on The Terminal List..."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 3000, Plain: "前情提要…", Display: "前情提要…"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 event, got %d", len(dialogues))
	}
	d := dialogues[0]
	if d.an >= 7 || strings.Contains(d.style, "Top") || strings.Contains(d.text, `\an8`) {
		t.Fatalf("ordinary italicized speech must stay at bottom, got style %q an %d text %q", d.style, d.an, d.text)
	}
}

// TestLyrics_MixedCueMissingIndependentTimingFallsBack verifies that if a mixed cue
// lacks reliable independent timing for the spoken part, it conservatively preserves
// content rather than arbitrarily guessing a cut point.
func TestLyrics_MixedCueMissingIndependentTimingFallsBack(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{
				ID:           1,
				Start:        1000,
				End:          4000,
				Plain:        "- ♪ Singing line ♪\n- Spoken line.",
				Display:      "- ♪ Singing line ♪   - Spoken line.",
				MultiSpeaker: true,
			},
		},
		ZH: []Cue{
			{
				ID:      1,
				Start:   1000, // Starts at same time as English; no independent later cut point
				End:     4000,
				Plain:   "翻译",
				Display: "翻译",
			},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	// Without independent timing evidence, content is conserved as 1 event
	if len(dialogues) != 1 {
		t.Fatalf("expected fallback to 1 event without independent timing evidence, got %d", len(dialogues))
	}
	d := dialogues[0]
	if !strings.Contains(d.text, "Singing line") || !strings.Contains(d.text, "Spoken line") {
		t.Fatalf("expected content preserved, got %q", d.text)
	}
}

// TestLyrics_TopOccupiedFallbackToBottom verifies that when the top area is occupied
// by native on-screen text, a concurrent lyric safely falls back to bottom.
func TestLyrics_TopOccupiedFallbackToBottom(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			// Group 1: on-screen text with native top
			{ID: 1, Start: 1000, End: 3000, Plain: "LOCATION: SAN DIEGO", Display: "LOCATION: SAN DIEGO", Top: true, Alignment: 8},
			// Group 2: concurrent lyric
			{ID: 2, Start: 1200, End: 2800, Plain: "♪ Background music ♪", Display: "♪ Background music ♪"},
		},
		ZH: []Cue{},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: digest,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
			{ENIDs: []int{2}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"},
		},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	result, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(result))
	var ostEvent, lyricEvent *dialogue
	for i := range dialogues {
		d := &dialogues[i]
		if strings.Contains(d.text, "SAN DIEGO") {
			ostEvent = d
		}
		if strings.Contains(d.text, "Background music") {
			lyricEvent = d
		}
	}
	if ostEvent == nil || lyricEvent == nil {
		t.Fatalf("expected both events, got %v", dialogues)
	}
	// OST must remain at top
	if ostEvent.an < 7 && !strings.Contains(ostEvent.style, "Top") {
		t.Fatalf("expected OST at top, got style %q an %d", ostEvent.style, ostEvent.an)
	}
	// Lyric must have fallen back to bottom
	if lyricEvent.an >= 7 || strings.Contains(lyricEvent.style, "Top") || strings.Contains(lyricEvent.text, `\an8`) {
		t.Fatalf("expected lyric to fall back to bottom due to occupied top, got style %q an %d text %q", lyricEvent.style, lyricEvent.an, lyricEvent.text)
	}
}

// TestProbeRenderE01 verifies rendering of full E01 recorded input.
func TestProbeRenderE01(t *testing.T) {
	dir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-alignment-combined"
	rawBundle, err := os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Skipf("skipping E01 probe test (fixture not present): %v", err)
	}
	var bundle Bundle
	if err := jsonUnmarshal(rawBundle, &bundle); err != nil {
		t.Fatal(err)
	}
	rawMapping, err := os.ReadFile(filepath.Join(dir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mapping Mapping
	if err := jsonUnmarshal(rawMapping, &mapping); err != nil {
		t.Fatal(err)
	}
	var words WordTimings
	if rawWords, err := os.ReadFile(filepath.Join(dir, "word-timings.json")); err == nil {
		_ = jsonUnmarshal(rawWords, &words)
	}
	rawStyle, err := os.ReadFile(filepath.Join(dir, "delivered-style.json"))
	if err != nil {
		t.Fatal(err)
	}
	var style config.ASSStyleConfig
	if err := jsonUnmarshal(rawStyle, &style); err != nil {
		t.Fatal(err)
	}

	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}

	assBytes, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1632, words)
	if err != nil {
		t.Fatalf("renderWithAudits failed: %v", err)
	}

	v, err := verifyEpisode(bundle, mapping, cfg, events)
	if err != nil {
		t.Fatalf("verifyEpisode error: %v", err)
	}
	failures := v.Verify()
	if len(failures) > 0 {
		for _, f := range failures {
			t.Errorf("validation failure: %s", f)
		}
	}

	t.Logf("Total events: %d (EN covered: %d/%d, ZH covered: %d/%d)", len(events), v.ENCovered, v.ENSourceCues, v.ZHCovered, v.ZHSourceCues)

	// Inspect key groups
	for _, e := range events {
		if e.group == 46 || e.group == 90 || e.group == 24 || e.group == 142 || (e.group >= 91 && e.group <= 97) || (e.group >= 228 && e.group <= 233) {
			t.Logf("G%d [%d-%d] top=%v strategy=%s zh=%q en=%q", e.group, e.assStart, e.assEnd, e.alignment >= 7, e.strategy, e.zhText, e.enText)
		}
	}

	destDir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-dialogue-lyrics"
	if _, err := os.Stat(destDir); err == nil {
		_ = os.WriteFile(filepath.Join(destDir, "E01.zh20-bilingual-layout.ass"), assBytes, 0644)
	}
}

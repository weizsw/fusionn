package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// makeTestBundle builds a valid Bundle with fingerprint.
func makeTestBundle(t *testing.T, en, zh []Cue) Bundle {
	t.Helper()
	b := Bundle{
		Version: Version,
		EN:      en,
		ZH:      zh,
	}
	digest, err := bundleDigest(b)
	if err != nil {
		t.Fatalf("bundleDigest: %v", err)
	}
	b.Fingerprint = digest
	return b
}

func testStyle() config.ASSStyleConfig {
	return renderTestStyle()
}

func testLLMConfig() config.LLMConfig {
	return config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
}

// Test S02E05 42:04.94–42:06.65 target
// Show the lyric independently at top, the spoken All right line below paired with 好了 里斯有那张牌.
// Chinese may hold; English Reece got that one must only appear at the next English cue at 42:06.65.
func TestSemanticRecheck_S02E05_Target4204_LyricTop_SpokenBelow(t *testing.T) {
	en := []Cue{
		{
			ID:           1,
			Start:        2524940,
			End:          2526649,
			Plain:        "- ♪ But we were good at having fun...\n- All right, all right, all right.",
			Display:      "- ♪ But we were good at having fun...\n- All right, all right, all right.",
			MultiSpeaker: true,
			Alignment:    2,
		},
		{
			ID:        2,
			Start:     2526650,
			End:       2527859,
			Plain:     "Reece got that one.",
			Display:   "Reece got that one.",
			Alignment: 2,
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     2525023,
			End:       2527901,
			Plain:     "好了 里斯有那张牌",
			Display:   "好了 里斯有那张牌",
			Alignment: 2,
		},
	}
	bundle := makeTestBundle(t, en, zh)

	// Mock recheck decision
	recheck := SemanticRecheck{
		ENIDs: []int{1, 2},
		ZHIDs: []int{1},
		Relations: []SemanticRelation{
			{EN: []string{"e0"}, ZH: nil},
			{EN: []string{"e1", "e2"}, ZH: []string{"z0"}},
		},
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{recheck},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(assBytes))
	if len(dialogues) < 3 {
		t.Fatalf("expected at least 3 events (1 top lyric, 2 bottom spoken), got %d: %v", len(dialogues), dialogues)
	}

	// 1. Verify Top lyric event
	foundTopLyric := false
	for _, d := range dialogues {
		if strings.Contains(d.style, "Top") && strings.Contains(d.text, "But we were good at having fun") {
			foundTopLyric = true
			if d.start != 2524940 || (d.end != 2526640 && d.end != 2526650) {
				t.Errorf("top lyric time [%d, %d] != [2524940, 2526640/2526650]", d.start, d.end)
			}
			if strings.Contains(d.text, "好了") || strings.Contains(d.text, "里斯") {
				t.Errorf("top lyric should be independent without spoken Chinese: %q", d.text)
			}
		}
	}
	if !foundTopLyric {
		t.Errorf("top lyric event not found in: %s", assBytes)
	}

	// 2. Verify bottom events:
	// 42:04.94 - 42:06.65: All right line below paired with 好了 里斯有那张牌
	// 42:06.65 - 42:07.86: Reece got that one paired with 好了 里斯有那张牌 (Chinese holds)
	foundFirstSpoken := false
	foundSecondSpoken := false
	for _, d := range dialogues {
		if !strings.Contains(d.style, "Top") {
			if strings.Contains(d.text, "All right") {
				foundFirstSpoken = true
				if !strings.Contains(d.text, "好了 里斯有那张牌") {
					t.Errorf("first spoken missing Chinese: %q", d.text)
				}
				if d.start != 2524940 || (d.end != 2526640 && d.end != 2526650) {
					t.Errorf("first spoken time [%d, %d] != [2524940, 2526640/2526650]", d.start, d.end)
				}
			}
			if strings.Contains(d.text, "Reece got that one") {
				foundSecondSpoken = true
				if !strings.Contains(d.text, "好了 里斯有那张牌") {
					t.Errorf("second spoken missing held Chinese: %q", d.text)
				}
				if (d.start != 2526640 && d.start != 2526650) || (d.end != 2527850 && d.end != 2527860) {
					t.Errorf("second spoken time [%d, %d] != [2526650, 2527860]", d.start, d.end)
				}
			}
		}
	}
	if !foundFirstSpoken {
		t.Errorf("first spoken event not found")
	}
	if !foundSecondSpoken {
		t.Errorf("second spoken event not found")
	}
}

// Test S02E05 41:56.35
// Related 41:56.35 Chinese translations belong to the lyric, not Boom/No.
func TestSemanticRecheck_S02E05_Target4156_ChineseLyricBelongsToSong(t *testing.T) {
	en := []Cue{
		{
			ID:           1,
			Start:        2516348,
			End:          2519434,
			Plain:        "- ♪ We were always on the run...\n- Boom.",
			Display:      "- ♪ We were always on the run...\n- Boom.",
			MultiSpeaker: true,
			Alignment:    2,
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     2516598,
			End:       2518767,
			Plain:     "我们总在四处奔波",
			Display:   "我们总在四处奔波",
			Alignment: 2,
		},
	}
	bundle := makeTestBundle(t, en, zh)

	// Model determines that Chinese translates the lyric, Boom is untranslated
	recheck := SemanticRecheck{
		ENIDs: []int{1},
		ZHIDs: []int{1},
		Relations: []SemanticRelation{
			{EN: []string{"e0"}, ZH: []string{"z0"}},
			{EN: []string{"e1"}, ZH: nil},
		},
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{recheck},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(assBytes))
	foundLyricWithZH := false
	foundBoomWithoutZH := false
	for _, d := range dialogues {
		if strings.Contains(d.style, "Top") {
			if strings.Contains(d.text, "我们总在四处奔波") && strings.Contains(d.text, "We were always on the run") {
				foundLyricWithZH = true
			}
		} else {
			if strings.Contains(d.text, "Boom") {
				foundBoomWithoutZH = true
				if strings.Contains(d.text, "奔波") {
					t.Errorf("Boom should NOT be paired with Chinese lyric translation: %q", d.text)
				}
			}
		}
	}

	if !foundLyricWithZH {
		t.Errorf("Top lyric with Chinese translation not found in: %s", assBytes)
	}
	if !foundBoomWithoutZH {
		t.Errorf("Boom spoken dialogue below not found in: %s", assBytes)
	}
}

// Test S02E05 42:08.86
// Inverted source row order: line 1 is spoken, line 2 is lyric.
func TestSemanticRecheck_S02E05_Target4208_InvertedSourceRowOrder(t *testing.T) {
	en := []Cue{
		{
			ID:           1,
			Start:        2528860,
			End:          2531362,
			Plain:        "- What we got next? Queen of hearts...\n- ♪ We got stoned along the seawall...",
			Display:      "- What we got next? Queen of hearts...\n- ♪ We got stoned along the seawall...",
			MultiSpeaker: true,
			Alignment:    2,
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     2528902,
			End:       2531112,
			Plain:     "接下来出什么？红桃Q",
			Display:   "接下来出什么？红桃Q",
			Alignment: 2,
		},
	}
	bundle := makeTestBundle(t, en, zh)

	// Line 0 is spoken ("What we got next?"), line 1 is lyric ("We got stoned...")
	// Chinese translates line 0
	recheck := SemanticRecheck{
		ENIDs: []int{1},
		ZHIDs: []int{1},
		Relations: []SemanticRelation{
			{EN: []string{"e0"}, ZH: []string{"z0"}},
			{EN: []string{"e1"}, ZH: nil},
		},
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{recheck},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(assBytes))
	foundTopLyric := false
	foundBottomDialogue := false
	for _, d := range dialogues {
		if strings.Contains(d.style, "Top") {
			if strings.Contains(d.text, "We got stoned along the seawall") {
				foundTopLyric = true
			}
		} else {
			if strings.Contains(d.text, "接下来出什么？红桃Q") && strings.Contains(d.text, "Queen of hearts") {
				foundBottomDialogue = true
			}
		}
	}

	if !foundTopLyric {
		t.Errorf("top lyric not found in: %s", assBytes)
	}
	if !foundBottomDialogue {
		t.Errorf("bottom spoken dialogue not found in: %s", assBytes)
	}
}

// Test S02E06 11:35.61
// Separate Harper's untranslated question (top, English only) from I need you to get her out of D.C.
// paired below with 我需要你带她离开华盛顿. Next English turn and 你可以请假吗？ should pair starting 11:37.61.
func TestSemanticRecheck_S02E06_Target1135_HarperUntranslatedTop(t *testing.T) {
	en := []Cue{
		{
			ID:           1,
			Start:        695612,
			End:          697613,
			Plain:        "- Harper, just a real quick question.\n- I need you to get her out of D.C.",
			Display:      "- Harper, just a real quick question.\n- I need you to get her out of D.C.",
			MultiSpeaker: true,
			Alignment:    2,
		},
		{
			ID:        2,
			Start:     697614,
			End:       699116,
			Plain:     "You good to take some leave?",
			Display:   "You good to take some leave?",
			Alignment: 2,
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     695862,
			End:       698865,
			Plain:     "我需要你带她离开华盛顿\n你可以请假吗？",
			Display:   "我需要你带她离开华盛顿\n你可以请假吗？",
			Alignment: 2,
		},
	}
	bundle := makeTestBundle(t, en, zh)

	// Relations:
	// e0 ("Harper...") -> untranslated question (top)
	// e1 ("I need you...") -> z0 ("我需要你带她离开华盛顿")
	// e2 ("You good...") -> z1 ("你可以请假吗？")
	recheck := SemanticRecheck{
		ENIDs: []int{1, 2},
		ZHIDs: []int{1},
		Relations: []SemanticRelation{
			{EN: []string{"e0"}, ZH: nil},
			{EN: []string{"e1"}, ZH: []string{"z0"}},
			{EN: []string{"e2"}, ZH: []string{"z1"}},
		},
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{recheck},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(assBytes))

	foundHarperTop := false
	foundFirstTurnBottom := false
	foundSecondTurnBottom := false

	for _, d := range dialogues {
		if strings.Contains(d.style, "Top") {
			if strings.Contains(d.text, "Harper, just a real quick question") {
				foundHarperTop = true
				if d.start != 695610 || d.end != 697610 {
					t.Errorf("Harper top time [%d, %d] != [695610, 697610]", d.start, d.end)
				}
			}
		} else {
			if strings.Contains(d.text, "I need you to get her out of D.C.") {
				foundFirstTurnBottom = true
				if !strings.Contains(d.text, "我需要你带她离开华盛顿") {
					t.Errorf("first turn missing Chinese translation: %q", d.text)
				}
				if d.start != 695610 || d.end != 697610 {
					t.Errorf("first turn time [%d, %d] != [695610, 697610]", d.start, d.end)
				}
			}
			if strings.Contains(d.text, "You good to take some leave?") {
				foundSecondTurnBottom = true
				if !strings.Contains(d.text, "你可以请假吗？") {
					t.Errorf("second turn missing Chinese translation: %q", d.text)
				}
				if d.start != 697610 {
					t.Errorf("second turn start %d != 697610 (11:37.61)", d.start)
				}
			}
		}
	}

	if !foundHarperTop {
		t.Errorf("Harper untranslated question at top not found in: %s", assBytes)
	}
	if !foundFirstTurnBottom {
		t.Errorf("first turn paired below not found in: %s", assBytes)
	}
	if !foundSecondTurnBottom {
		t.Errorf("second turn starting 11:37.61 not found in: %s", assBytes)
	}
}

// Test S02E06 359
// Reversed translation order
func TestSemanticRecheck_S02E06_Target359_ReversedTranslationOrder(t *testing.T) {
	en := []Cue{
		{
			ID:           1,
			Start:        1508299,
			End:          1510842,
			Plain:        "- keep me from going lunatic\n- Hey, buddy.",
			Display:      "- keep me from going lunatic\n- Hey, buddy.",
			MultiSpeaker: true,
			Alignment:    2,
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     1508674,
			End:       1509591,
			Plain:     "嘿 伙计",
			Display:   "嘿 伙计",
			Alignment: 2,
		},
		{
			ID:        2,
			Start:     1509592,
			End:       1510967,
			Plain:     "不要让我疯掉！",
			Display:   "不要让我疯掉！",
			Alignment: 2,
		},
	}
	bundle := makeTestBundle(t, en, zh)

	// Relations:
	// e0 (lyric) translates z1 ("不要让我疯掉！")
	// e1 (spoken) translates z0 ("嘿 伙计")
	recheck := SemanticRecheck{
		ENIDs: []int{1},
		ZHIDs: []int{1, 2},
		Relations: []SemanticRelation{
			{EN: []string{"e0"}, ZH: []string{"z1"}},
			{EN: []string{"e1"}, ZH: []string{"z0"}},
		},
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{recheck},
	}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dialogues := parseDialogues(t, string(assBytes))
	foundLyric := false
	foundSpoken := false
	for _, d := range dialogues {
		if strings.Contains(d.text, "不要让我疯掉！") {
			foundLyric = true
			if !strings.Contains(d.text, "keep me from going lunatic") {
				t.Errorf("lyric mismatch: %q", d.text)
			}
			// Bound strictly ends at native cue end
			if d.end != 1510840 {
				t.Errorf("lyric boundary end %d != 1510840", d.end)
			}
		}
		if strings.Contains(d.text, "嘿 伙计") {
			foundSpoken = true
			if !strings.Contains(d.text, "Hey, buddy.") {
				t.Errorf("spoken mismatch: %q", d.text)
			}
			// Bound strictly starts at native cue start
			if d.start != 1508300 && d.start != 1508290 {
				t.Errorf("spoken boundary start %d != 1508300", d.start)
			}
		}
	}
	if !foundLyric || !foundSpoken {
		t.Errorf("reversed translation events not properly paired: %s", assBytes)
	}
}

// Test Negative Cases
func TestSemanticRecheck_NegativeCases(t *testing.T) {
	// 1. Natural wrap (multiline without dashes or song symbols)
	wrapEN := []Cue{{ID: 1, Plain: "First line of speech.\nSecond line of same sentence."}}
	wrapZH := []Cue{{ID: 1, Plain: "第一句台词\n同一句话的第二句"}}
	gWrap := Group{ENIDs: []int{1}, ZHIDs: []int{1}}
	enIdx, zhIdx, _ := cueIndex(makeTestBundle(t, wrapEN, wrapZH))
	if isAmbiguousGroup(gWrap, enIdx, zhIdx) {
		t.Errorf("natural wrap should NOT be ambiguous")
	}

	// 2. Symmetric Diamonds exchange (both 2 dashes)
	symEN := []Cue{{ID: 1, Plain: "- Diamonds.\n- Diamonds? Diamonds. Sorry, I'm...", MultiSpeaker: true}}
	symZH := []Cue{{ID: 1, Plain: "- 方块\n- 方块 不好意思 我没牌可出", MultiSpeaker: true}}
	gSym := Group{ENIDs: []int{1}, ZHIDs: []int{1}}
	enSym, zhSym, _ := cueIndex(makeTestBundle(t, symEN, symZH))
	if isAmbiguousGroup(gSym, enSym, zhSym) {
		t.Errorf("symmetric multi-speaker dialogue should NOT be ambiguous")
	}

	// 3. Simple lyric cue without spoken row (Chinese has no music symbol)
	songEN := []Cue{{ID: 1, Plain: "♪ We got drunk and rolled a car... ♪"}}
	songZH := []Cue{{ID: 1, Plain: "我们喝醉了把车翻了"}}
	gSong := Group{ENIDs: []int{1}, ZHIDs: []int{1}}
	enSong, zhSong, _ := cueIndex(makeTestBundle(t, songEN, songZH))
	if isAmbiguousGroup(gSong, enSong, zhSong) {
		t.Errorf("simple lyric cue without spoken row should NOT be ambiguous")
	}

	// 4. Lone dash
	loneEN := []Cue{{ID: 1, Plain: "- Look, you know Landry."}}
	loneZH := []Cue{{ID: 1, Plain: "听着 你了解兰德里"}}
	gLone := Group{ENIDs: []int{1}, ZHIDs: []int{1}}
	enLone, zhLone, _ := cueIndex(makeTestBundle(t, loneEN, loneZH))
	if isAmbiguousGroup(gLone, enLone, zhLone) {
		t.Errorf("lone dash on single line should NOT be ambiguous")
	}
}

// Test validation rejection and fallback when model returns invalid or incomplete response.
func TestSemanticRecheck_InvalidModelAnswer_RejectionAndFallback(t *testing.T) {
	byKey := map[string]int{"en=1;zh=1": 0}
	enInfos := map[string][]recheckLineInfo{
		"en=1;zh=1": {
			{Ref: "e0", Text: "- Line 1"},
			{Ref: "e1", Text: "- Line 2"},
		},
	}
	zhInfos := map[string][]recheckLineInfo{
		"en=1;zh=1": {
			{Ref: "z0", Text: "行一"},
		},
	}

	// 1. Incomplete relations: e1 missing
	incompleteJSON := `{"rechecks":[{"group":"en=1;zh=1","relations":[{"en":["e0"],"zh":["z0"]}]}]}`
	if _, err := validateSemanticRecheckResponse(json.RawMessage(incompleteJSON), byKey, enInfos, zhInfos); err == nil {
		t.Errorf("expected error for incomplete relations missing e1")
	}

	// 2. Hallucinated ref: e99
	hallucinatedJSON := `{"rechecks":[{"group":"en=1;zh=1","relations":[{"en":["e0","e99"],"zh":["z0"]},{"en":["e1"],"zh":[]}]}]}`
	if _, err := validateSemanticRecheckResponse(json.RawMessage(hallucinatedJSON), byKey, enInfos, zhInfos); err == nil {
		t.Errorf("expected error for hallucinated ref e99")
	}

	// 3. Duplicate ref: e0 repeated
	dupJSON := `{"rechecks":[{"group":"en=1;zh=1","relations":[{"en":["e0"],"zh":["z0"]},{"en":["e0","e1"],"zh":[]}]}]}`
	if _, err := validateSemanticRecheckResponse(json.RawMessage(dupJSON), byKey, enInfos, zhInfos); err == nil {
		t.Errorf("expected error for duplicate ref e0")
	}
}

// Test live mock server with cache replay
func TestSemanticRecheck_MockServer_CacheReplay(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- ♪ Song line ♪\n- Spoken line", MultiSpeaker: true},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "歌曲译文"},
	}
	bundle := makeTestBundle(t, en, zh)

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		resp := `{"rechecks":[{"group":"en=1;zh=1","relations":[{"en":["e0"],"zh":["z0"]},{"en":["e1"],"zh":[]}]}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "mock",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": resp}},
			},
		})
	}))
	defer server.Close()

	cfg := testLLMConfig()
	cfg.BaseURL = server.URL
	cfg.Model = "mock"
	cfg.ResponseFormat = "json_object"
	cacheDir := t.TempDir()

	engine := NewEngine()

	// First run: calls server and caches
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0}}
	rechecks1, audit1, req1, hits1 := engine.recheckAmbiguousGroups(context.Background(), bundle, groups, cfg, cacheDir)
	if req1 != 1 || hits1 != 0 {
		t.Errorf("run 1: want req=1 hits=0, got req=%d hits=%d", req1, hits1)
	}
	if len(rechecks1) != 1 || audit1.Applied != 1 {
		t.Errorf("run 1: expected 1 applied recheck, got %d", audit1.Applied)
	}

	// Second run: served from cache
	rechecks2, audit2, req2, hits2 := engine.recheckAmbiguousGroups(context.Background(), bundle, groups, cfg, cacheDir)
	if req2 != 0 || hits2 != 1 {
		t.Errorf("run 2: want req=0 hits=1, got req=%d hits=%d", req2, hits2)
	}
	if len(rechecks2) != 1 || audit2.Applied != 1 {
		t.Errorf("run 2: expected 1 applied recheck from cache, got %d", audit2.Applied)
	}
	if requests != 1 {
		t.Errorf("server should have been called exactly once, called %d times", requests)
	}
}

// Test direct mapping import rejects invalid semantic rechecks
func TestSemanticRecheck_DirectMappingImport_RejectsInvalid(t *testing.T) {
	bundle := makeTestBundle(t, []Cue{{ID: 1, Start: 1000, End: 2000, Plain: "Hello"}}, []Cue{{ID: 1, Start: 1000, End: 2000, Plain: "你好"}})
	dir := t.TempDir()
	sourcesPath := filepath.Join(dir, "sources.json")
	srcData, _ := json.Marshal(bundle)
	_ = os.WriteFile(sourcesPath, srcData, 0644)

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups:            []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0}},
		SemanticRechecks: []SemanticRecheck{
			{
				ENIDs: []int{1},
				ZHIDs: []int{1},
				Relations: []SemanticRelation{
					{EN: []string{"e99"}, ZH: []string{"z0"}}, // e99 invalid
				},
			},
		},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md

	mappingPath := filepath.Join(dir, "mapping.json")
	mapData, _ := json.Marshal(mapping)
	_ = os.WriteFile(mappingPath, mapData, 0644)

	cfg := config.LLMConfig{MappingPath: mappingPath}
	_, _, err := importMapping(bundle, cfg)
	if err == nil {
		t.Errorf("importMapping should fail when semantic recheck contains invalid refs")
	}
}

// Test partial overlap on the same layer safely falls back to former screen plan
func TestSemanticRecheck_PartialOverlap_FallbackToFormerScreenPlan(t *testing.T) {
	// Two blocks on the same layer with unsupported partial overlap
	b1 := block{start: 1000, end: 2500, top: false}
	b2 := block{start: 2000, end: 3500, top: false}
	merged := coalesceOverlappingBlocks([]block{b1, b2})
	if merged != nil {
		t.Fatalf("expected coalesceOverlappingBlocks to return nil on unsupported partial overlap, got %v", merged)
	}

	// Normal render fallback check: ambiguous group with 2 dashed speaker lines
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- Speaker A\n- Speaker B", Display: "- Speaker A\n- Speaker B", MultiSpeaker: true},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- 说话人甲\n- 说话人乙", Display: "- 说话人甲\n- 说话人乙", MultiSpeaker: true},
	}
	bundle := makeTestBundle(t, en, zh)

	// An invalid or unsupported recheck plan falls back, preserving explicit speaker rows
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dialogues := parseDialogues(t, string(assBytes))
	if len(dialogues) == 0 {
		t.Fatal("expected rendered dialogue lines")
	}
	hasSpeakerA := false
	hasSpeakerB := false
	for _, d := range dialogues {
		if strings.Contains(d.text, "Speaker A") {
			hasSpeakerA = true
		}
		if strings.Contains(d.text, "Speaker B") {
			hasSpeakerB = true
		}
	}
	if !hasSpeakerA || !hasSpeakerB {
		t.Errorf("fallback screen plan did not preserve explicit speaker rows: %s", assBytes)
	}
}

// Test invalid relations cannot fabricate a match and fall back preserving speaker rows
func TestSemanticRecheck_InvalidRelations_FallbackPreservesSpeakerRows(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 2000, End: 4000, Plain: "- Question?\n- Answer.", Display: "- Question?\n- Answer.", MultiSpeaker: true},
	}
	zh := []Cue{
		{ID: 1, Start: 2000, End: 4000, Plain: "- 问题？\n- 回答。", Display: "- 问题？\n- 回答。", MultiSpeaker: true},
	}
	bundle := makeTestBundle(t, en, zh)

	// Mapping has no valid semantic rechecks for this ambiguous group
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: nil, // no valid recheck matches
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dialogues := parseDialogues(t, string(assBytes))
	foundQuestion := false
	foundAnswer := false
	for _, d := range dialogues {
		if strings.Contains(d.text, "Question?") {
			foundQuestion = true
		}
		if strings.Contains(d.text, "Answer.") {
			foundAnswer = true
		}
	}
	if !foundQuestion || !foundAnswer {
		t.Errorf("ambiguous fallback did not preserve explicit speaker rows: %s", assBytes)
	}
}

// Test HTTP failures safely fallback and do not crash
func TestSemanticRecheck_HTTPFailure_SafeFallback(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- ♪ Song line ♪\n- Spoken line", Display: "- ♪ Song line ♪ - Spoken line", MultiSpeaker: true},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "歌曲译文", Display: "歌曲译文"},
	}
	bundle := makeTestBundle(t, en, zh)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testLLMConfig()
	cfg.BaseURL = server.URL
	cfg.Model = "mock"
	cfg.Retries = 0

	engine := NewEngine()
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0}}
	rechecks, audit, requests, hits := engine.recheckAmbiguousGroups(context.Background(), bundle, groups, cfg, t.TempDir())
	if len(rechecks) != 1 || !rechecks[0].Uncertain {
		t.Fatalf("expected 1 uncertain recheck on HTTP failure, got %+v", rechecks)
	}
	if len(audit.Failures) == 0 {
		t.Errorf("expected audit failures on HTTP failure, got none")
	}
	if requests != 1 || hits != 0 {
		t.Errorf("want req=1 hits=0, got req=%d hits=%d", requests, hits)
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups:            groups,
		SemanticRechecks:  rechecks,
	}
	assBytes, err := Render(bundle, mapping, cfg, testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dialogues := parseDialogues(t, string(assBytes))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 dialogue event, got %d: %s", len(dialogues), assBytes)
	}
	if !strings.Contains(dialogues[0].text, "歌曲译文") {
		t.Errorf("rendered event missing Chinese: %s", dialogues[0].text)
	}
	wantSubstr := "- ♪ Song line ♪\\N- Spoken line"
	t.Logf("serialized fallback event text: %s", dialogues[0].text)
	if !strings.Contains(dialogues[0].text, wantSubstr) {
		t.Errorf("rendered event %q does not contain both original EN rows separated by \\N", dialogues[0].text)
	}
}

// Test imported plans preserve source text and timing
func TestSemanticRecheck_ImportedPlan_PreservesSourceTextAndTiming(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- Speaker 1\n- Speaker 2", Display: "- Speaker 1\n- Speaker 2", MultiSpeaker: true},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "统一译文", Display: "统一译文"},
	}
	bundle := makeTestBundle(t, en, zh)
	dir := t.TempDir()

	sourcesPath := filepath.Join(dir, "sources.json")
	srcData, _ := json.Marshal(bundle)
	_ = os.WriteFile(sourcesPath, srcData, 0644)

	recheck := SemanticRecheck{
		ENIDs: []int{1},
		ZHIDs: []int{1},
		Relations: []SemanticRelation{
			{EN: []string{"e0", "e1"}, ZH: []string{"z0"}},
		},
	}
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Parameters:        MappingParameters{SchemaEnforcement: schemaEnforcement},
		Groups:            []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0}},
		SemanticRechecks:  []SemanticRecheck{recheck},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md

	mappingPath := filepath.Join(dir, "mapping.json")
	mapData, _ := json.Marshal(mapping)
	_ = os.WriteFile(mappingPath, mapData, 0644)

	cfg := config.LLMConfig{MappingPath: mappingPath}
	importedMapping, _, err := importMapping(bundle, cfg)
	if err != nil {
		t.Fatalf("importMapping failed: %v", err)
	}
	if len(importedMapping.SemanticRechecks) != 1 {
		t.Fatalf("imported mapping lost semantic recheck: %+v", importedMapping)
	}

	assBytes, err := Render(bundle, importedMapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render with imported mapping failed: %v", err)
	}
	dialogues := parseDialogues(t, string(assBytes))
	if len(dialogues) != 1 {
		t.Fatalf("expected 1 rendered event, got %d", len(dialogues))
	}
	if dialogues[0].start != 1000 || dialogues[0].end != 3000 {
		t.Errorf("dialogue timing [%d, %d] != [1000, 3000]", dialogues[0].start, dialogues[0].end)
	}
	if !strings.Contains(dialogues[0].text, "Speaker 1") || !strings.Contains(dialogues[0].text, "统一译文") {
		t.Errorf("dialogue missing source text: %q", dialogues[0].text)
	}
}

// Test uncertain fallback with multiple EN and ZH cues sweeps active source cues,
// preserves explicit rows with literal ASS \N, renders English-only intervals,
// and ensures a later ZH phrase does not appear before its start.
func TestSemanticRecheck_Uncertain_MultipleCues_SweepsActiveSourceCues(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 2000, Plain: "First EN clause.", Display: "First EN clause."},
		{ID: 2, Start: 2000, End: 4000, Plain: "- Speaker A\n- Speaker B", Display: "- Speaker A - Speaker B", MultiSpeaker: true},
		{ID: 3, Start: 4000, End: 5000, Plain: "Third EN clause.", Display: "Third EN clause."},
	}
	zh := []Cue{
		{ID: 1, Start: 1500, End: 3000, Plain: "- 说话人甲\n- 说话人乙", Display: "- 说话人甲 - 说话人乙", MultiSpeaker: true},
		{ID: 2, Start: 3000, End: 4500, Plain: "稍后的中文句子。", Display: "稍后的中文句子。"},
	}
	bundle := makeTestBundle(t, en, zh)

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue", OwnerWindow: 0},
		},
		SemanticRechecks: []SemanticRecheck{
			{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Uncertain: true},
		},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md

	assBytes, err := Render(bundle, mapping, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dialogues := parseDialogues(t, string(assBytes))
	t.Logf("rendered %d dialogues:", len(dialogues))
	for i, d := range dialogues {
		t.Logf("  [%d] %d..%d: %s", i, d.start, d.end, d.text)
	}

	if len(dialogues) == 0 {
		t.Fatal("expected rendered dialogue events, got 0")
	}

	// 1. First event is English-only [1000..1500] and has "First EN clause." with no Chinese.
	if dialogues[0].start != 1000 || dialogues[0].end != 1500 {
		t.Errorf("event 0 timing [%d, %d] != [1000, 1500]", dialogues[0].start, dialogues[0].end)
	}
	if strings.Contains(dialogues[0].text, "说话人") || strings.Contains(dialogues[0].text, "稍后的中文句子") {
		t.Errorf("event 0 unexpectedly contains Chinese: %s", dialogues[0].text)
	}

	// 2. Later ZH phrase ("稍后的中文句子") only appears starting at 3000ms.
	for _, d := range dialogues {
		if d.end <= 3000 && strings.Contains(d.text, "稍后的中文句子") {
			t.Errorf("later ZH phrase appeared prematurely at %d..%d: %s", d.start, d.end, d.text)
		}
	}

	// 3. Explicit EN rows (- Speaker A\N- Speaker B) and ZH rows (- 说话人甲\N- 说话人乙) are preserved with literal \N.
	hasExplicitEN := false
	hasExplicitZH := false
	for _, d := range dialogues {
		if strings.Contains(d.text, "Speaker A\\N- Speaker B") {
			hasExplicitEN = true
		}
		if strings.Contains(d.text, "说话人甲\\N- 说话人乙") {
			hasExplicitZH = true
		}
	}
	if !hasExplicitEN {
		t.Errorf("rendered dialogues missing explicit English rows joined by literal \\N")
	}
	if !hasExplicitZH {
		t.Errorf("rendered dialogues missing explicit Chinese rows joined by literal \\N")
	}

	// 4. Last event is English-only [4500..5000].
	last := dialogues[len(dialogues)-1]
	if last.start != 4500 || last.end != 5000 {
		t.Errorf("last event timing [%d, %d] != [4500, 5000]", last.start, last.end)
	}
	if strings.Contains(last.text, "稍后的中文句子") {
		t.Errorf("last event unexpectedly contains Chinese: %s", last.text)
	}
}

func TestSemanticRecheck_ASSCentisecondQuantization(t *testing.T) {
	// Blocks whose start/end differ in raw milliseconds but round to the same ASS centisecond:
	// 1004ms and 996ms both round to ASS centisecond 100 (1000ms / 0:01.00).
	// 1996ms and 2004ms both round to ASS centisecond 200 (2000ms / 0:02.00).
	textA := "Text A"
	textB := "Text B"
	b1 := block{start: 1004, end: 1996, top: false, zhText: &textA}
	b2 := block{start: 996, end: 2004, top: false, zhText: &textB}
	merged := coalesceOverlappingBlocks([]block{b1, b2})
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged block for ASS centisecond identical windows, got %d", len(merged))
	}
	if merged[0].zhText == nil {
		t.Fatal("expected non-nil merged zhText")
	}
	if *merged[0].zhText != "Text B\nText A" {
		t.Fatalf("expected merged text 'Text B\\nText A', got %q", *merged[0].zhText)
	}
}

// Target 1 regression: accepted Chinese-only relation referencing multiple distinct Chinese cues
// preserves each cue's own time window and rows.
func TestSemanticRecheck_ChineseOnlyUsesOwnCues(t *testing.T) {
	en := []Cue{{ID: 1, Start: 1000, End: 3000, Plain: "- ♪ Sing ♪\n- Speak!", Display: "- ♪ Sing ♪\n- Speak!", MultiSpeaker: true, Alignment: 2}}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "说！", Display: "说！", Alignment: 2},
		{ID: 2, Start: 4000, End: 6000, Plain: "还有呢？", Display: "还有呢？", Alignment: 2},
		{ID: 3, Start: 6500, End: 8000, Plain: "好了。", Display: "好了。", Alignment: 2},
	}
	b := makeTestBundle(t, en, zh)
	g := Group{ENIDs: []int{1}, ZHIDs: []int{1, 2, 3}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"}
	m := Mapping{
		Version:           Version,
		SourceFingerprint: b.Fingerprint,
		Groups:            []Group{g},
		SemanticRechecks: []SemanticRecheck{
			{
				ENIDs: g.ENIDs,
				ZHIDs: g.ZHIDs,
				Relations: []SemanticRelation{
					{EN: []string{"e0"}},
					{EN: []string{"e1"}, ZH: []string{"z0"}},
					{ZH: []string{"z1", "z2"}},
				},
			},
		},
	}
	m.Fingerprint, _ = mappingDigest(m)
	o, err := Render(b, m, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range parseDialogues(t, string(o)) {
		if strings.Contains(d.text, "还有呢？") {
			if d.start != 4000 || d.end != 6000 {
				t.Errorf("source [4000,6000], event = %+v", d)
			}
			if d.style != "Default" {
				t.Errorf("expected native layer style 'Default', got %q: %+v", d.style, d)
			}
		}
		if strings.Contains(d.text, "好了。") {
			found = true
			if d.start != 6500 || d.end != 8000 {
				t.Errorf("source [6500,8000], event = %+v", d)
			}
			if d.style != "Default" {
				t.Errorf("expected native layer style 'Default', got %q: %+v", d.style, d)
			}
		}
	}
	if !found {
		t.Fatal("missing Chinese dialogue event '好了。'")
	}
}

// Target 2 regression: accepted recheck keeps native timing boundaries intact
// without unintended reading-duration extension (e.g. 1000..1600 does not extend to 2200).
func TestSemanticRecheck_ShortRecheckNativeTime(t *testing.T) {
	en := []Cue{{ID: 1, Start: 1000, End: 1600, Plain: "- ♪ Sing ♪\n- Speak!", Display: "- ♪ Sing ♪\n- Speak!", MultiSpeaker: true, Alignment: 2}}
	zh := []Cue{{ID: 1, Start: 1000, End: 1600, Plain: "说！", Display: "说！", Alignment: 2}}
	b := makeTestBundle(t, en, zh)
	g := Group{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"}
	m := Mapping{
		Version:           Version,
		SourceFingerprint: b.Fingerprint,
		Groups:            []Group{g},
		SemanticRechecks: []SemanticRecheck{
			{
				ENIDs: g.ENIDs,
				ZHIDs: g.ZHIDs,
				Relations: []SemanticRelation{
					{EN: []string{"e0"}},
					{EN: []string{"e1"}, ZH: []string{"z0"}},
				},
			},
		},
	}
	m.Fingerprint, _ = mappingDigest(m)
	o, err := Render(b, m, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range parseDialogues(t, string(o)) {
		if d.end != 1600 {
			t.Errorf("source ends 1600, event = %+v", d)
		}
	}
}

func TestSemanticRecheck_BatchKeys(t *testing.T) {
	keys := map[string]int{"en=1;zh=1": 0, "en=2;zh=2": 1}
	eInfos := map[string][]recheckLineInfo{
		"en=1;zh=1": {{Ref: "e0"}},
		"en=2;zh=2": {{Ref: "e0"}},
	}
	zInfos := map[string][]recheckLineInfo{
		"en=1;zh=1": {{Ref: "z0"}},
		"en=2;zh=2": {{Ref: "z0"}},
	}

	cases := map[string][]string{
		"missing":   {"en=1;zh=1"},
		"unknown":   {"en=1;zh=1", "en=2;zh=2", "en=3;zh=3"},
		"duplicate": {"en=1;zh=1", "en=1;zh=1", "en=2;zh=2"},
		"empty":     {},
		"valid":     {"en=2;zh=2", "en=1;zh=1"},
	}

	for name, list := range cases {
		t.Run(name, func(t *testing.T) {
			var items []any
			for _, k := range list {
				items = append(items, map[string]any{
					"group": k,
					"relations": []SemanticRelation{
						{EN: []string{"e0"}, ZH: []string{"z0"}},
					},
				})
			}
			b, _ := json.Marshal(map[string]any{"rechecks": items})
			rechecks, err := validateSemanticRecheckResponse(b, keys, eInfos, zInfos)
			if name == "valid" {
				if err != nil {
					t.Fatalf("valid batch rejected: %v", err)
				}
				if len(rechecks) != 2 {
					t.Fatalf("expected 2 rechecks, got %d", len(rechecks))
				}
			} else {
				if err == nil {
					t.Fatalf("%s batch was unexpectedly accepted", name)
				}
			}
		})
	}
}

func TestSemanticRecheck_OneSidedTemporalOverlapLayer(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "- Cough.\n- Speak.", Display: "- Cough. - Speak.", MultiSpeaker: true, Alignment: 2},
		{ID: 2, Start: 5000, End: 6500, Plain: "Later!", Display: "Later!", Alignment: 2},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "说！", Display: "说！", Alignment: 2},
	}
	b := makeTestBundle(t, en, zh)
	g := Group{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"}
	m := Mapping{
		Version:           Version,
		SourceFingerprint: b.Fingerprint,
		Groups:            []Group{g},
		SemanticRechecks: []SemanticRecheck{
			{
				ENIDs: g.ENIDs,
				ZHIDs: g.ZHIDs,
				Relations: []SemanticRelation{
					{EN: []string{"e0", "e2"}},
					{EN: []string{"e1"}, ZH: []string{"z0"}},
				},
			},
		},
	}
	m.Fingerprint, _ = mappingDigest(m)
	o, err := Render(b, m, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}

	seenCough := false
	seenLater := false
	for _, d := range parseDialogues(t, string(o)) {
		if strings.Contains(d.text, "Cough.") {
			seenCough = true
			if d.style != "TopEng" {
				t.Errorf("concurrent voice expected TopEng, got %+v", d)
			}
		}
		if strings.Contains(d.text, "Later!") {
			seenLater = true
			if d.start != 5000 || d.end != 6500 || d.style != "Eng" {
				t.Errorf("sequential voice expected Eng at 5000..6500, got %+v", d)
			}
		}
	}
	if !seenCough || !seenLater {
		t.Errorf("expected both voices, saw seenCough=%v seenLater=%v", seenCough, seenLater)
	}
}

func TestSemanticRecheck_SingleCueSpeakerBreak_UncertainFallback(t *testing.T) {
	en := []Cue{{
		ID:           1,
		Start:        1000,
		End:          3500,
		Plain:        "Are we there?\n- Soon.",
		Display:      "Are we there? - Soon.",
		MultiSpeaker: false,
		Alignment:    2,
	}}
	zh := []Cue{{
		ID:           1,
		Start:        1000,
		End:          3500,
		Plain:        "- 到了吗？\n- 快了。",
		Display:      "- 到了吗？ - 快了。",
		MultiSpeaker: true,
		Alignment:    2,
	}}
	b := makeTestBundle(t, en, zh)
	g := Group{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: "dialogue"}
	e, z, _ := cueIndex(b)
	if !isAmbiguousGroup(g, e, z) {
		t.Fatal("fixture is not an ambiguous candidate")
	}
	m := Mapping{
		Version:           Version,
		SourceFingerprint: b.Fingerprint,
		Groups:            []Group{g},
		SemanticRechecks: []SemanticRecheck{
			{ENIDs: g.ENIDs, ZHIDs: g.ZHIDs, Uncertain: true},
		},
	}
	m.Fingerprint, _ = mappingDigest(m)
	o, err := Render(b, m, testLLMConfig(), testStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	dialogues := parseDialogues(t, string(o))
	if len(dialogues) != 1 || !strings.Contains(dialogues[0].text, `Are we there?\N- Soon.`) {
		t.Fatalf("uncertain English speaker break lost: %+v", dialogues)
	}
}

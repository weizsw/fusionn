package subtitlellm

import (
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

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

func TestReview939ShiftedMixedSpeechKeepsValidDuration(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 1000, 4000, "- ♪ Sail away ♪\n- Stay here.", true)},
		[]Cue{review939Cue(1, 4500, 6000, "留在这里", false)},
		[]Group{review939Group([]int{1}, []int{1})})
	ass, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("legitimate shifted tracks must use conservative fallback, not fail the whole render: %v", err)
	}
	if !strings.Contains(string(ass), "Stay here.") || !strings.Contains(string(ass), "留在这里") {
		t.Fatalf("source text lost: %s", ass)
	}
}

func TestReview939SymmetricShiftedMixedSpeechKeepsValidDuration(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 4500, 6000, "Stay here.", false)},
		[]Cue{review939Cue(1, 1000, 4000, "- ♪ 歌声远去 ♪\n- 留在这里", true)},
		[]Group{review939Group([]int{1}, []int{1})})
	ass, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("symmetric shifted tracks must use conservative fallback, not fail: %v", err)
	}
	if !strings.Contains(string(ass), "Stay here.") || !strings.Contains(string(ass), "留在这里") {
		t.Fatalf("source text lost: %s", ass)
	}
}

func TestReview939MixedSpeechZeroAtFinalPrecisionFallsBack(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 1000, 4000, "- ♪ Sail away ♪\n- Stay here.", true)},
		[]Cue{review939Cue(1, 3996, 5200, "留在这里", false)},
		[]Group{review939Group([]int{1}, []int{1})})
	_, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("a cut that vanishes at ASS precision must fall back without failing: %v", err)
	}
}

func TestReview939SymmetricMixedSpeechZeroAtFinalPrecisionFallsBack(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 3996, 5200, "Stay here.", false)},
		[]Cue{review939Cue(1, 1000, 4000, "- ♪ 歌声远去 ♪\n- 留在这里", true)},
		[]Group{review939Group([]int{1}, []int{1})})
	_, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("symmetric cut vanishing at ASS precision must fall back: %v", err)
	}
}

func TestReview939WrappedOpenLyricNotInventedSpeech(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 1000, 5000, "♪ Sail into the stars\nand never come back", false)},
		[]Cue{review939Cue(1, 1500, 5000, "扬帆驶向群星不再归来", false)},
		[]Group{review939Group([]int{1}, []int{1})})
	_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if (strings.Contains(e.enText, "never come back") || strings.Contains(e.zhText, "不再归来")) && e.alignment < 7 {
			t.Fatalf("an unmarked wrapped continuation and its lyric translation became spoken text: %+v", e)
		}
	}
}

func TestReview939SymmetricWrappedOpenLyricNotInventedSpeech(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 1500, 5000, "Sail into the stars and never return", false)},
		[]Cue{review939Cue(1, 1000, 5000, "♪ 驶向群星\n永不回头", false)},
		[]Group{review939Group([]int{1}, []int{1})})
	_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if (strings.Contains(e.enText, "never return") || strings.Contains(e.zhText, "永不回头")) && e.alignment < 7 {
			t.Fatalf("symmetric unmarked wrapped continuation became spoken text: %+v", e)
		}
	}
}

func TestReview939LyricMoveDoesNotKeepBottomAbsolutePosition(t *testing.T) {
	cue := review939Cue(1, 1000, 4000, "♪ Sail away ♪", false)
	pos := [2]float64{960, 900}
	cue.Position = &pos
	b, m := review939Inputs(t, []Cue{cue}, nil, []Group{review939Group([]int{1}, nil)})
	ass, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("moving the lyric must remain renderable: %v", err)
	}
	ds := parseDialogues(t, string(ass))
	if len(ds) != 1 {
		t.Fatalf("expected one complete lyric, got %v", ds)
	}
	if !strings.Contains(ds[0].style, "Top") {
		t.Fatalf("expected lyric at top, got %+v", ds[0])
	}
	if ds[0].hasPos && ds[0].posY >= 540 {
		t.Fatalf("top style still carries bottom absolute position: %+v", ds[0])
	}
}

func TestReview939SymmetricLyricMoveDoesNotKeepBottomAbsolutePosition(t *testing.T) {
	cue := review939Cue(1, 1000, 4000, "♪ 歌唱 ♪", false)
	pos := [2]float64{960, 900}
	cue.Position = &pos
	b, m := review939Inputs(t, nil, []Cue{cue}, []Group{review939Group(nil, []int{1})})
	ass, _, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatalf("moving symmetric lyric must remain renderable: %v", err)
	}
	ds := parseDialogues(t, string(ass))
	if len(ds) != 1 {
		t.Fatalf("expected one complete lyric, got %v", ds)
	}
	if !strings.Contains(ds[0].style, "Top") {
		t.Fatalf("expected lyric at top, got %+v", ds[0])
	}
	if ds[0].hasPos && ds[0].posY >= 540 {
		t.Fatalf("top style still carries bottom absolute position: %+v", ds[0])
	}
}

func TestReview939MixedSpeechCounterpartStillGetsTemporalPlan(t *testing.T) {
	b, m := review939Inputs(t,
		[]Cue{review939Cue(1, 1000, 6000, "- ♪ Sail away ♪\n- First preparations. Final preparations.", true)},
		[]Cue{review939Cue(1, 2000, 6000, "准备出发的第一步\n准备出发的第二步", false)},
		[]Group{review939Group([]int{1}, []int{1})})
	cfg := review939Config()
	cfg.ZhChars = 8
	_, events, err := renderWithAudits(b, m, cfg, renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var phrases []string
	for _, e := range events {
		if e.zhText != "" {
			if strings.Contains(e.zhText, "\n") {
				t.Errorf("ordinary over-cap speech kept both rows despite 4s budget: %+v", e)
			}
			phrases = append(phrases, e.zhText)
		}
	}
	if len(phrases) < 2 {
		t.Fatalf("ordinary counterpart must retain its normal split planning: %q", phrases)
	}
}

func TestReview939JoinedCounterpartKeepsProjectedNativeBoundary(t *testing.T) {
	es := []Cue{
		review939Cue(1, 1000, 3500, strings.Repeat("An ordinary lead sentence. ", 4), false),
		review939Cue(2, 3500, 5000, "First reply.", false),
		review939Cue(3, 5000, 7000, "Second reply.", false),
	}
	zs := []Cue{
		review939Cue(1, 1000, 4000, "开始的陈述", false),
		review939Cue(2, 4000, 7000, "- 好\n- 不", true),
	}
	b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1, 2, 3}, []int{1, 2})})
	_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		for _, id := range e.zhIDs {
			if id == 2 && e.assStart < 4000 {
				t.Fatalf("joining original EN cues widened projected range and revealed next Chinese source early: %+v", e)
			}
		}
	}
}

func TestReview939SymmetricJoinedCounterpartKeepsProjectedNativeBoundary(t *testing.T) {
	es := []Cue{
		review939Cue(1, 1000, 4000, "Beginning statement that takes some time to read.", false),
		review939Cue(2, 4000, 7000, "- Yes\n- No", true),
	}
	zs := []Cue{
		review939Cue(1, 1000, 3500, "第一句较长引导陈述用于占满前段时窗", false),
		review939Cue(2, 3500, 5000, "好啊", false),
		review939Cue(3, 5000, 7000, "不好", false),
	}
	b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1, 2}, []int{1, 2, 3})})
	_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		for _, id := range e.enIDs {
			if id == 2 && e.assStart < 4000 {
				t.Fatalf("symmetric joining widened projected range and revealed next English source early: %+v", e)
			}
		}
	}
}

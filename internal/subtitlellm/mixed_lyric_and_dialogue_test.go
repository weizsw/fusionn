package subtitlellm

import (
	"strings"
	"testing"
)

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
	for _, tt := range []struct {
		name    string
		zhStart int // native start of the second Chinese cue
		want    int // where the second Chinese cue may first appear
	}{
		// 500ms from the English boundary at 3500: the switch snaps to it.
		{name: "snaps_within_500ms", zhStart: 4000, want: 3500},
		// 750ms from the English boundary and not checked: it stays native.
		{name: "unchecked_750ms_stays_native", zhStart: 4250, want: 4250},
	} {
		t.Run(tt.name, func(t *testing.T) {
			zs := []Cue{
				review939Cue(1, 1000, tt.zhStart, "开始的陈述", false),
				review939Cue(2, tt.zhStart, 7000, "- 好\n- 不", true),
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1, 2, 3}, []int{1, 2})})
			_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			first := -1
			for _, e := range events {
				for _, id := range e.zhIDs {
					if id == 2 && (first < 0 || e.assStart < first) {
						first = e.assStart
					}
				}
			}
			if first != tt.want {
				t.Fatalf("second Chinese cue first appears at %d, want %d (joined English cues must not widen it): %+v", first, tt.want, events)
			}
		})
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

// A lyric line need not have a dash. The renderer already accepts the following
// dash-prefixed line as speech; later planning must retain that role subset.
func TestReview5ccMixedRoleSyncMustNotReimportParent(t *testing.T) {
	for _, symmetric := range []bool{false, true} {
		name := "english_mixed"
		if symmetric {
			name = "chinese_mixed"
		}
		t.Run(name, func(t *testing.T) {
			es := []Cue{review939Cue(1, 1000, 6000, "♪ Sail into distant shining stars ♪\n- First preparations and final preparations.", false)}
			zs := []Cue{review939Cue(1, 2000, 6000, "准备出发的第一步\n准备出发的第二步", false)}
			if symmetric {
				es = []Cue{review939Cue(1, 2000, 6000, "First preparations made ready.\nFinal preparations made ready.", false)}
				zs = []Cue{review939Cue(1, 1000, 6000, "♪ 歌声穿过远方明亮的星空 ♪\n- 留在这里并准备好迎接下一步", false)}
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1}, []int{1})})
			cfg := review939Config()
			cfg.ZhChars = 8
			ass, events, err := renderWithAudits(b, m, cfg, renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			var sawLyric, sawSpeech bool
			for _, e := range events {
				if e.alignment >= 7 && (hasMusicSymbol(e.enText) || hasMusicSymbol(e.zhText)) {
					sawLyric = true
				}
				if e.alignment < 7 {
					if hasMusicSymbol(e.enText) || hasMusicSymbol(e.zhText) {
						t.Errorf("source-based synchronous split reimported the original lyric into a derived speech block: %+v", e)
					}
					if strings.Contains(e.enText, "preparations") || strings.Contains(e.zhText, "留在这里") {
						sawSpeech = true
					}
				}
			}
			if !sawLyric || !sawSpeech {
				t.Fatalf("missing lyric or spoken role; lyric=%v speech=%v\n%s", sawLyric, sawSpeech, ass)
			}
		})
	}
}

// Removing one lyric row leaves two ordinary source speech rows per language.
// The approved 2+2 preference still applies even when both texts fit the caps.
// A derived ordinary 2+2 speech pair whose lines fit side by side stays one
// event, like any other under-cap 2+2 pair.
func TestReview5ccDerivedTwoPlusTwoStaysTogetherUnderCap(t *testing.T) {
	for _, symmetric := range []bool{false, true} {
		name := "english_mixed"
		if symmetric {
			name = "chinese_mixed"
		}
		t.Run(name, func(t *testing.T) {
			es := []Cue{review939Cue(1, 1000, 6000, "- ♪ Sail away ♪\n- First step now.\nFinal step now.", true)}
			zs := []Cue{review939Cue(1, 2000, 6000, "第一步\n第二步", false)}
			if symmetric {
				es = []Cue{review939Cue(1, 2000, 6000, "First step now.\nFinal step now.", false)}
				zs = []Cue{review939Cue(1, 1000, 6000, "- ♪ 歌声远去 ♪\n- 第一步\n第二步", true)}
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1}, []int{1})})
			_, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			var speech []renderedEvent
			for _, e := range events {
				if e.alignment < 7 {
					speech = append(speech, e)
				}
			}
			if len(speech) != 1 {
				t.Fatalf("derived ordinary 2+2 speech under the caps should stay one event: %+v", speech)
			}
			for _, e := range speech {
				if hasMusicSymbol(e.enText) || hasMusicSymbol(e.zhText) {
					t.Errorf("parent lyric reimported: %+v", e)
				}
			}
		})
	}
}

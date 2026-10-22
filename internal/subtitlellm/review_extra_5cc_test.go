package subtitlellm

import (
	"strings"
	"testing"
)

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
func TestReview5ccDerivedTwoPlusTwoRetainsSyncPreference(t *testing.T) {
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
			if len(speech) != 2 {
				t.Fatalf("derived ordinary 2+2 speech should sync into two readable fragments before under-cap merging: %+v", speech)
			}
			for _, e := range speech {
				if e.assEnd-e.assStart < 1200 {
					t.Errorf("sync floor violated: %+v", e)
				}
				if hasMusicSymbol(e.enText) || hasMusicSymbol(e.zhText) {
					t.Errorf("parent lyric reimported: %+v", e)
				}
			}
		})
	}
}

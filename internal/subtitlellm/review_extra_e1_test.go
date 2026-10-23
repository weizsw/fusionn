package subtitlellm

import (
	"strings"
	"testing"
)

// Text overrides are also produced by ordinary temporal projection; they do not
// by themselves prove that a source's two speakers were separated into roles.
func TestReviewE1TemporalOverrideRetainsRealDialogueCap(t *testing.T) {
	for _, symmetric := range []bool{false, true} {
		name := "held_chinese_dialogue"
		if symmetric {
			name = "held_english_dialogue"
		}
		t.Run(name, func(t *testing.T) {
			parsed := func(plain string) Cue {
				return buildCue(1, rawCue{start: 1000, end: 5000, text: plain}, styleInfo{alignment: 2}, 1920, 1080)
			}
			es := []Cue{parsed("First ordinary line explains what should happen next.\nSecond ordinary line continues the same explanation.")}
			zs := []Cue{parsed("- 我觉得这个计划非常不错\n- 这的确是个很好的主意")}
			cfg := review939Config()
			dialogueSource := zs[0]
			dialogueCap := cfg.ZhChars
			if symmetric {
				es = []Cue{parsed("- We need to leave before dark.\n- Yes, let's go while we can.")}
				zs = []Cue{parsed("第一段普通台词说明这件事\n第二段普通台词继续说明这件事")}
				cfg.EnChars = 50
				dialogueSource = es[0]
				dialogueCap = cfg.EnChars
			}
			if !dialogueSource.MultiSpeaker || len(sourceLines(dialogueSource.Plain)) != 2 || len(sourceLines(dialogueSource.Display)) != 1 || countedLength(dialogueSource.Display) <= dialogueCap {
				t.Fatalf("fixture must be a real parsed two-speaker source, flattened by normalization but over the configured display cap: %+v cap=%d", dialogueSource, dialogueCap)
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1}, []int{1})})
			prior := fontWidthMeasurerOverride
			fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { return 200, true }
			t.Cleanup(func() { fontWidthMeasurerOverride = prior })
			ass, events, err := renderWithAudits(b, m, cfg, renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 2 {
				t.Fatalf("ordinary side should split while the true dialogue holds; events=%+v\n%s", events, ass)
			}
			for _, e := range events {
				dialogue := e.zhText
				if symmetric {
					dialogue = e.enText
				}
				if len(sourceLines(dialogue)) != 2 {
					t.Errorf("ordinary temporal text override erased real MultiSpeaker identity and allowed over-cap flattening: %q event=%+v", dialogue, e)
				}
				if !strings.HasPrefix(strings.TrimSpace(dialogue), "-") {
					t.Errorf("speaker marker lost: %q", dialogue)
				}
			}
		})
	}
}

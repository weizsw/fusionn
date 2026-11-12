package subtitlellm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReview6fOneSidedThreeRowsRemainRenderable(t *testing.T) {
	for _, english := range []bool{true, false} {
		name := "english_only"
		if !english {
			name = "chinese_only"
		}
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("a valid one-sided multi-line source must not panic the episode render: %v", r)
				}
			}()
			plain := "First ordinary subtitle sentence that fills a source line.\nSecond ordinary subtitle sentence that fills a source line.\nThird ordinary subtitle sentence that fills a source line."
			if !english {
				plain = "这是一段很长的画面文字第一行\n这是一段很长的画面文字第二行\n这是一段很长的画面文字第三行"
			}
			cue := buildCue(1, rawCue{start: 1000, end: 2200, text: plain}, styleInfo{alignment: 2}, 1920, 1080)
			if len(sourceLines(cue.Plain)) != 3 {
				t.Fatalf("fixture must retain three native rows: %+v", cue)
			}
			var es, zs []Cue
			var enIDs, zhIDs []int
			if english {
				es = []Cue{cue}
				enIDs = []int{1}
			} else {
				zs = []Cue{cue}
				zhIDs = []int{1}
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group(enIDs, zhIDs)})
			ass, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatalf("one-sided content must render: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("the 1.2s source cannot be split into two 0.8s fragments or be omitted: %+v", events)
			}
			for _, e := range events {
				if english && e.zhText != "" {
					t.Errorf("invented Chinese: %+v", e)
				}
				if !english && e.enText != "" {
					t.Errorf("invented English: %+v", e)
				}
				got := e.zhText
				if english {
					got = e.enText
				}
				if compactText(got, false) != compactText(cue.Display, false) {
					t.Errorf("one-sided source text changed: want=%q got=%q", cue.Display, got)
				}
				if e.assStart != 1000 || e.assEnd != 2200 {
					t.Errorf("one-sided source window changed: %+v", e)
				}
			}
			t.Logf("rendered %d events without dropping the one-sided source; ASS bytes=%d", len(events), len(ass))
		})
	}
}

func TestReview6fSeparatedSpeechKeepsLateOrdinaryMerge(t *testing.T) {
	for _, symmetric := range []bool{false, true} {
		name := "english_mixed"
		if symmetric {
			name = "chinese_mixed"
		}
		t.Run(name, func(t *testing.T) {
			parsed := func(start, end int, plain string) Cue {
				return buildCue(1, rawCue{start: start, end: end, text: plain}, styleInfo{alignment: 2}, 1920, 1080)
			}
			es := []Cue{parsed(1000, 2700, "- ♪ Sail away ♪\n- Stay here while I check the other room.")}
			zs := []Cue{parsed(1500, 2700, "留在这里等我仔细检查隔壁\n房间里面的情况以后再行动")}
			counterpart := zs[0]
			cap := 20
			if symmetric {
				es = []Cue{parsed(1500, 2700, "Please stay where you are while I carefully\ncheck the other room and make sure the way is clear.")}
				zs = []Cue{parsed(1000, 2700, "- ♪ 歌声继续 ♪\n- 先留在这里等我检查隔壁房间")}
				counterpart = es[0]
				cap = 65
			}
			mixed := es[0]
			if symmetric {
				mixed = zs[0]
			}
			_, speech, ok := cueIsMixedLyric(mixed)
			if !ok || !mixed.MultiSpeaker || len(speech) != 1 || cueSpeakerLines(speech) >= 2 || counterpart.MultiSpeaker || len(sourceLines(counterpart.Plain)) != 2 || utf8.RuneCountInString(counterpart.Display) <= cap {
				t.Fatalf("fixture must distinguish a multi-source parent from one ordinary speaker, with over-cap two-line translation: mixed=%+v counterpart=%+v speech=%+v", mixed, counterpart, speech)
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1}, []int{1})})
			prior := fontWidthMeasurerOverride
			calls := 0
			fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { calls++; return 200, true }
			t.Cleanup(func() { fontWidthMeasurerOverride = prior })
			ass, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			var bottom []renderedEvent
			for _, e := range events {
				if e.alignment < 7 {
					bottom = append(bottom, e)
				}
			}
			if len(bottom) != 1 {
				t.Fatalf("one 1.2s spoken window cannot be time-split under the 0.8s floor: %+v\n%s", bottom, ass)
			}
			e := bottom[0]
			if e.assStart != 1500 || e.assEnd != 2700 {
				t.Errorf("spoken source envelope changed: %+v", e)
			}
			if strings.ContainsAny(e.enText+e.zhText, "♪♫") {
				t.Errorf("lyrics resurrected in speech: %+v", e)
			}
			gotCounterpart, gotSpeech := e.zhText, e.enText
			if symmetric {
				gotCounterpart, gotSpeech = e.enText, e.zhText
			}
			if compactText(gotCounterpart, false) != compactText(counterpart.Display, false) || compactText(gotSpeech, false) != compactText(strings.Join(speech, " "), false) {
				t.Errorf("separated role or full translation text changed: %+v", e)
			}
			if len(sourceLines(e.enText)) != 1 || len(sourceLines(e.zhText)) != 1 || calls == 0 {
				t.Errorf("separated one-speaker speech inherited the mixed parent's MultiSpeaker cap and skipped the approved width-safe ordinary merge: widthCalls=%d event=%+v\n%s", calls, e, ass)
			}
		})
	}
}

func TestReview6fDialogueRoleOrCounterpartRetainsProtection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		symmetric  bool
		roleMulti  bool
		countMulti bool
	}{
		{name: "english_mixed_two_speaker_role", symmetric: false, roleMulti: true, countMulti: false},
		{name: "english_mixed_two_speaker_counterpart", symmetric: false, roleMulti: false, countMulti: true},
		{name: "chinese_mixed_two_speaker_role", symmetric: true, roleMulti: true, countMulti: false},
		{name: "chinese_mixed_two_speaker_counterpart", symmetric: true, roleMulti: false, countMulti: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := func(start, end int, plain string) Cue {
				return buildCue(1, rawCue{start: start, end: end, text: plain}, styleInfo{alignment: 2}, 1920, 1080)
			}
			var es, zs []Cue
			if !tc.symmetric {
				enPlain := "- ♪ Sail away ♪\n- Stay here while I check the other room."
				if tc.roleMulti {
					enPlain = "- ♪ Sail away ♪\n- Stay here while I carefully check the other room.\n- No, I am coming along with you right now."
				}
				zhPlain := "留在这里等我仔细检查隔壁\n房间里面的情况以后再行动"
				if tc.countMulti {
					zhPlain = "- 留在这里等我仔细检查隔壁房间\n- 我跟你一起过去确认情况安全"
				}
				es = []Cue{parsed(1000, 2700, enPlain)}
				zs = []Cue{parsed(1500, 2700, zhPlain)}
			} else {
				enPlain := "Please stay where you are while I carefully\ncheck the other room and make sure the way is clear."
				if tc.countMulti {
					enPlain = "- Please stay where you are while I check the way.\n- No, I am coming along with you right now."
				}
				zhPlain := "- ♪ 歌声继续 ♪\n- 先留在这里等我检查隔壁房间"
				if tc.roleMulti {
					zhPlain = "- ♪ 歌声继续 ♪\n- 先留在这里等我仔细检查隔壁房间\n- 我不放心要跟你一起去确认安全"
				}
				es = []Cue{parsed(1500, 2700, enPlain)}
				zs = []Cue{parsed(1000, 2700, zhPlain)}
			}
			b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1}, []int{1})})
			prior := fontWidthMeasurerOverride
			fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { return 200, true }
			t.Cleanup(func() { fontWidthMeasurerOverride = prior })
			ass, events, err := renderWithAudits(b, m, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			var bottom []renderedEvent
			for _, e := range events {
				if e.alignment < 7 {
					bottom = append(bottom, e)
				}
			}
			if len(bottom) != 1 {
				t.Fatalf("expected 1 bottom event, got %d: %+v\n%s", len(bottom), bottom, ass)
			}
			e := bottom[0]
			if tc.countMulti {
				counterpartText := e.zhText
				if tc.symmetric {
					counterpartText = e.enText
				}
				if len(sourceLines(counterpartText)) != 2 {
					t.Errorf("genuinely multi-speaker counterpart should retain dialogue protection and not merge: %q event=%+v", counterpartText, e)
				}
			}
			if tc.roleMulti {
				roleText := e.enText
				if tc.symmetric {
					roleText = e.zhText
				}
				if len(sourceLines(roleText)) != 2 {
					t.Errorf("actual two-speaker role should retain dialogue protection and not merge: %q event=%+v", roleText, e)
				}
			}
		})
	}
}

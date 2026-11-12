package subtitlellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 歌词字幕 (CONTEXT.md): one predicate decides which cue is a lyric, and
// pairing, layout and one-sided placement all use it.

// timingPairs reports whether one group holds both cue IDs.
func timingPairs(groups []Group, enID, zhID int) bool {
	for _, g := range groups {
		if hasCueID(g.ENIDs, enID) && hasCueID(g.ZHIDs, zhID) {
			return true
		}
	}
	return false
}

// hasCueID reports whether ids contains id.
func hasCueID(ids []int, id int) bool {
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}

// A restored lyric cue carries the music symbol on one source line only. It is
// still a lyric, so timing must not pair it with a Chinese dialogue cue the
// model never paired. Ordinary restored dialogue in the same failed window must
// still pair, or the test would pass by disabling timing pairing altogether.
func TestLyricCueIsNotPairedByTiming(t *testing.T) {
	en := []Cue{
		review939Cue(1, 1000, 4000, "♪ Silent night, holy night\nAll is calm, all is bright", false),
		review939Cue(2, 6000, 9000, "I am checking the other room now.", false),
	}
	zh := []Cue{
		review939Cue(1, 1100, 3900, "平安夜，圣善夜", false),
		review939Cue(2, 6100, 8900, "我正在检查另一个房间", false),
	}
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "window unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKey = "test-key"

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align: %v", err)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("the fixture must restore one failed window, got %v", audit.FailedWindows)
	}

	if timingPairs(mapping.Groups, 1, 1) {
		t.Errorf("the lyric cue was timing-paired with a dialogue cue: %+v", mapping.Groups)
	}
	if hasCueID(audit.TimingPaired["en"], 1) {
		t.Errorf("audit reports the lyric cue as timing-paired: %v", audit.TimingPaired)
	}

	if !timingPairs(mapping.Groups, 2, 2) {
		t.Errorf("ordinary restored dialogue was not timing-paired: %+v", mapping.Groups)
	}
	if !hasCueID(audit.TimingPaired["en"], 2) {
		t.Errorf("audit is missing the ordinary dialogue pairing: %v", audit.TimingPaired)
	}
}

// A one-sided cue with a music symbol belongs on the Top row, unless it is a
// mixed lyric and dialogue cue: that one keeps the dialogue path and only goes
// Top when its source position says Top.
func TestOneSidedLyricPlacementUsesOnePredicate(t *testing.T) {
	cases := []struct {
		name      string
		plain     string
		sourceTop bool
		wantTop   bool
	}{
		{
			name:    "bracketed music description is a lyric",
			plain:   "[♪ upbeat music ♪]",
			wantTop: true,
		},
		{
			name:    "lyric spread over two source lines is a lyric",
			plain:   "♪ Silent night, holy night\nAll is calm, all is bright",
			wantTop: true,
		},
		{
			name:    "mixed lyric and dialogue keeps the dialogue path",
			plain:   "- ♪ Silent night ♪\n- Yeah, thank you.",
			wantTop: false,
		},
		{
			name:      "mixed lyric and dialogue with a top source position stays Top",
			plain:     "- ♪ Silent night ♪\n- Yeah, thank you.",
			sourceTop: true,
			wantTop:   true,
		},
		{
			name:    "ordinary dialogue keeps the dialogue path",
			plain:   "Yeah, thank you.",
			wantTop: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cue := review939Cue(1, 1000, 2700, tc.plain, false)
			cue.Top = tc.sourceTop
			bundle, mapping := review939Inputs(t, []Cue{cue}, nil, []Group{review939Group([]int{1}, nil)})

			ass, events, err := renderWithAudits(bundle, mapping, review939Config(), renderTestStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("a one-sided source cue must render one event: %+v", events)
			}
			event := events[0]
			if gotTop := event.alignment >= 7; gotTop != tc.wantTop {
				t.Errorf("Top placement = %v (alignment %d), want %v; event=%+v\n%s", gotTop, event.alignment, tc.wantTop, event, ass)
			}
			if compactText(event.enText, false) != compactText(cue.Display, false) {
				t.Errorf("one-sided source text changed: want %q got %q\n%s", cue.Display, event.enText, ass)
			}
		})
	}
}

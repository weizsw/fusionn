package subtitlellm

import (
	"strings"
	"testing"
)

// Reproduce the user's E01 00:13.347--00:19.310 report with the five original
// cues (IDs mechanically rebased). Flag changes below are diagnostic controls,
// not a proposed product fix: real multi-speaker cues must remain protected.
func TestReviewE01MixedGroupDoesNotHoldDistinctChineseSources(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cap            int
		disableMarkers bool
	}{
		{"actual_zh20", 20, false},
		{"lowering_cap_does_not_fix", 10, false},
		{"diagnostic_only_flags_cleared", 20, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			en := []Cue{
				{ID: 1, Start: 13347, End: 16015, OriginalStart: 13347, OriginalEnd: 16015, Plain: "Twelve SEALs lost\nand no one in command even issues", Display: "Twelve SEALs lost and no one in command even issues", Alignment: 2},
				{ID: 2, Start: 16016, End: 17434, OriginalStart: 16016, OriginalEnd: 17434, Plain: "- a statement?\n-  Fire.", Display: "- a statement?   -  Fire.", MultiSpeaker: true, Alignment: 2},
				{ID: 3, Start: 17435, End: 19310, OriginalStart: 17435, OriginalEnd: 19310, Plain: "What are they hiding?", Display: "What are they hiding?", Alignment: 2},
			}
			zh := []Cue{
				{ID: 1, Start: 13347, End: 16766, OriginalStart: 13347, OriginalEnd: 16766, Plain: "12名海豹突击队队员丧生\n而指挥部连一个声明都没有？", Display: "12名海豹突击队队员丧生 而指挥部连一个声明都没有？", Alignment: 2},
				{ID: 2, Start: 16767, End: 19310, OriginalStart: 16767, OriginalEnd: 19310, Plain: "- 开火\n- 他们在隐瞒些什么？", Display: "- 开火   - 他们在隐瞒些什么？", MultiSpeaker: true, Alignment: 2},
			}
			if tc.disableMarkers {
				for i := range en {
					en[i].MultiSpeaker = false
				}
				for i := range zh {
					zh[i].MultiSpeaker = false
				}
			}
			bundle := fixtureBundle(en, zh)
			mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
			cfg := fixtureConfig()
			cfg.ZhChars = tc.cap
			_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 3840, 1632, nil)
			if err != nil {
				t.Fatal(err)
			}
			firstEN2, lastEN2 := 999999, 0
			for _, e := range events {
				t.Logf("%d--%d en=%v zh=%v strategy=%s text=%q", e.assStart, e.assEnd, e.enIDs, e.zhIDs, e.strategy, e.zhText)
				x := compactText(e.zhText, false)
				if strings.Contains(x, "丧生") && strings.Contains(x, "隐瞒") {
					t.Errorf("separate non-overlapping Chinese source events are held together across the English slices at %d--%d: %q", e.assStart, e.assEnd, e.zhText)
				}
				for _, id := range e.zhIDs {
					if id == 1 && (e.assStart < 13350 || e.assEnd > 16770) {
						t.Errorf("ZH1 leaked outside its 13350--16770 window: event %d--%d text=%q", e.assStart, e.assEnd, e.zhText)
					}
					if id == 2 && (e.assStart < 16770 || e.assEnd > 19310) {
						t.Errorf("ZH2 leaked outside its 16770--19310 window: event %d--%d text=%q", e.assStart, e.assEnd, e.zhText)
					}
				}
				for _, id := range e.enIDs {
					if id == 2 {
						firstEN2 = min(firstEN2, e.assStart)
						lastEN2 = max(lastEN2, e.assEnd)
						if !strings.Contains(e.enText, "- a statement?") || !strings.Contains(e.enText, "-  Fire.") {
							t.Errorf("multi-speaker EN2 lost its internal speaker texts: %q", e.enText)
						}
					}
				}
			}
			if firstEN2 != 16020 || lastEN2 != 17440 {
				t.Errorf("EN2 display window mismatch: want 16020--17440, got %d--%d", firstEN2, lastEN2)
			}
		})
	}
}

// TestReview2408NToOneProtectionRequiresRealSplit verifies that in an N:1 group
// ordinary Chinese splits when over cap, preserves its complete text/order,
// while multi-speaker English preserves its two speakers and 4000--7000ms window.
func TestReview2408NToOneProtectionRequiresRealSplit(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "First ordinary English cue.", Plain: "First ordinary English cue.", Alignment: 2},
			{ID: 2, Start: 4000, End: 7000, Display: "- Yes.   - No.", Plain: "- Yes.\n- No.", MultiSpeaker: true, Alignment: 2},
		},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "第一段中文内容 第二段中文内容", Plain: "第一段中文内容\n第二段中文内容", Alignment: 2}},
	)
	cfg := fixtureConfig()
	cfg.ZhChars = 8
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	firstMulti, lastMulti := 999999, 0
	for _, e := range events {
		z := compactText(e.zhText, false)
		if strings.Contains(z, "第一段") && strings.Contains(z, "第二段") {
			t.Errorf("ordinary Chinese did not split despite being over cap: %q", e.zhText)
		}
		if len(parts) == 0 || parts[len(parts)-1] != z {
			parts = append(parts, z)
		}
		for _, id := range e.enIDs {
			if id == 2 {
				if !strings.Contains(e.enText, "- Yes.") || !strings.Contains(e.enText, "- No.") {
					t.Errorf("multi-speaker English was broken apart: %q", e.enText)
				}
				firstMulti = min(firstMulti, e.assStart)
				lastMulti = max(lastMulti, e.assEnd)
			}
		}
	}
	if strings.Join(parts, "") != "第一段中文内容第二段中文内容" {
		t.Errorf("wrong Chinese order/full text: %v", parts)
	}
	if firstMulti != 4000 || lastMulti != 7000 {
		t.Errorf("wrong multi-speaker window %d--%d", firstMulti, lastMulti)
	}
}

// TestReview2408OneToNProtectionRequiresRealSplit verifies that in a 1:N group
// ordinary English splits when over cap, preserves its complete text/order,
// while multi-speaker Chinese preserves its two speakers and 4000--7000ms window.
func TestReview2408OneToNProtectionRequiresRealSplit(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "First ordinary English part. Second ordinary English part.", Plain: "First ordinary English part.\nSecond ordinary English part.", Alignment: 2}},
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "第一段中文", Plain: "第一段中文", Alignment: 2},
			{ID: 2, Start: 4000, End: 7000, Display: "- 对   - 错", Plain: "- 对\n- 错", MultiSpeaker: true, Alignment: 2},
		},
	)
	cfg := fixtureConfig()
	cfg.EnChars = 30
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	firstMulti, lastMulti := 999999, 0
	for _, e := range events {
		x := compactText(e.enText, false)
		if strings.Contains(x, "Firstordinary") && strings.Contains(x, "Secondordinary") {
			t.Errorf("ordinary English did not split despite being over cap: %q", e.enText)
		}
		if len(parts) == 0 || parts[len(parts)-1] != x {
			parts = append(parts, x)
		}
		for _, id := range e.zhIDs {
			if id == 2 {
				if !strings.Contains(e.zhText, "- 对") || !strings.Contains(e.zhText, "- 错") {
					t.Errorf("multi-speaker Chinese was broken apart: %q", e.zhText)
				}
				firstMulti = min(firstMulti, e.assStart)
				lastMulti = max(lastMulti, e.assEnd)
			}
		}
	}
	if strings.Join(parts, "") != compactText("First ordinary English part. Second ordinary English part.", false) {
		t.Errorf("wrong English order/full text: %v", parts)
	}
	if firstMulti != 4000 || lastMulti != 7000 {
		t.Errorf("wrong multi-speaker window %d--%d", firstMulti, lastMulti)
	}
}

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
			for _, e := range events {
				t.Logf("%d--%d en=%v zh=%v strategy=%s text=%q", e.assStart, e.assEnd, e.enIDs, e.zhIDs, e.strategy, e.zhText)
				x := compactText(e.zhText, false)
				if strings.Contains(x, "丧生") && strings.Contains(x, "隐瞒") {
					t.Errorf("separate non-overlapping Chinese source events are held together across the English slices at %d--%d: %q", e.assStart, e.assEnd, e.zhText)
				}
			}
		})
	}
}

// TestMixedNToOneMultiSpeakerDoesNotLeakProtection guards that a multi-speaker
// cue in an N:1 group does not leak protection to the ordinary Chinese cue
// or cause an A-B-A-B replay defect.
func TestMixedNToOneMultiSpeakerDoesNotLeakProtection(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "First ordinary english cue.", Plain: "First ordinary english\nfirst part."},
			{ID: 2, Start: 4000, End: 7000, Display: "- Yes.   - No.", Plain: "- Yes.\n- No.", MultiSpeaker: true},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 7000, Display: "第一段中文内容 第二段中文内容", Plain: "第一段中文内容\n第二段中文内容"},
		},
	}
	d, _ := bundleDigest(bundle)
	bundle.Fingerprint = d
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: d,
		Groups:            []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md
	cfg := fixtureConfig()
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify EN 2 preserves its multi-speaker structure
	en2Found := false
	for _, e := range events {
		if strings.Contains(e.enText, "- Yes.") && strings.Contains(e.enText, "- No.") {
			en2Found = true
		}
	}
	if !en2Found {
		t.Fatal("multi-speaker English cue lost its speaker structure")
	}

	// Verify Chinese does not replay A-B-A-B
	var zhSeq []string
	for _, e := range events {
		zh := normText(e.zhText)
		if len(zhSeq) == 0 || zhSeq[len(zhSeq)-1] != zh {
			zhSeq = append(zhSeq, zh)
		}
	}
	for i := 0; i+1 < len(zhSeq); i++ {
		if strings.Contains(zhSeq[i], "第二段") && strings.Contains(zhSeq[i+1], "第一段") {
			t.Fatalf("Chinese replayed out of order: %v", zhSeq)
		}
	}
}

// TestMixedOneToNMultiSpeakerDoesNotLeakProtection guards that a multi-speaker
// cue in a 1:N group does not leak protection to the ordinary English cue.
func TestMixedOneToNMultiSpeakerDoesNotLeakProtection(t *testing.T) {
	bundle := Bundle{
		Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 7000, Display: "Ordinary english line spanning two chinese cues.", Plain: "Ordinary english line\nspanning two chinese cues."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "第一段中文", Plain: "第一段中文"},
			{ID: 2, Start: 4000, End: 7000, Display: "- 对   - 错", Plain: "- 对\n- 错", MultiSpeaker: true},
		},
	}
	d, _ := bundleDigest(bundle)
	bundle.Fingerprint = d
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: d,
		Groups:            []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}},
	}
	md, _ := mappingDigest(mapping)
	mapping.Fingerprint = md
	cfg := fixtureConfig()
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}

	zh2Found := false
	for _, e := range events {
		if strings.Contains(e.zhText, "- 对") && strings.Contains(e.zhText, "- 错") {
			zh2Found = true
		}
	}
	if !zh2Found {
		t.Fatal("multi-speaker Chinese cue lost its speaker structure")
	}
}

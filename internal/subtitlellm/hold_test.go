package subtitlellm

import (
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestParent33AdjacentOriginalRepetitionIsNotHold(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.EnChars = 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "No. No.", Plain: "No.\nNo."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "不要", Plain: "不要"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("probe requires 2 real source fragments, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("two adjacent No. source lines are valid, not a removable hold: %v", failures)
	}
}

func TestParent33RepeatedPrefixWithHeldChineseRemainsValid(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.EnChars, cfg.ZhChars = 8, 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "No. No. Run.", Plain: "No.\nNo. Run."}},
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, Display: "甲乙丙丁", Plain: "甲乙丙丁"},
			{ID: 2, Start: 1500, End: 5000, Display: "戊己庚辛", Plain: "戊己庚辛"},
		},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	// No order check confirmed a split, so both Chinese cues are held whole
	// while the English cue splits at its source lines "No." and "No. Run.".
	if len(events) != 2 || normText(events[0].enText) != "No." || normText(events[1].enText) != "No.Run." {
		t.Fatalf("probe must exercise source No./No.Run. with the Chinese held whole: %+v", events)
	}
	if failures := v.Verify(); len(failures) > 0 {
		t.Fatalf("valid split with a genuinely repeated source prefix rejected: %v", failures)
	}
}

// TestReview7cRejectsSuffixReplayAsHold ensures that an emission changing from a
// whole sentence to its trailing suffix is treated as an invalid replay/change,
// not as an acceptable hold.
func TestReview7cRejectsSuffixReplayAsHold(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		t.Run(lang, func(t *testing.T) {
			bundle := fixtureBundle(
				[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Alpha. Beta.", Plain: "Alpha.\nBeta."}},
				[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "甲乙 丙丁", Plain: "甲乙\n丙丁"}},
			)
			mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
			events := []renderedEvent{
				{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 3000, enText: "Alpha. Beta.", zhText: "甲乙 丙丁"},
				{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 3000, assEnd: 5000, enText: "Alpha. Beta.", zhText: "甲乙 丙丁"},
			}
			good, err := verifyEpisode(bundle, mapping, fixtureConfig(), events)
			if err != nil {
				t.Fatal(err)
			}
			if failures := good.Verify(); len(failures) > 0 {
				t.Fatalf("baseline whole-sentence freeze must pass: %v", failures)
			}
			if lang == "en" {
				events[1].enText = "Beta."
			} else {
				events[1].zhText = "丙丁"
			}
			bad, err := verifyEpisode(bundle, mapping, fixtureConfig(), events)
			if err != nil {
				t.Fatal(err)
			}
			if len(bad.OrderMismatches) == 0 {
				t.Fatalf("validator accepted whole sentence followed by its suffix as a hold in %s: %+v", lang, bad)
			}
		})
	}
}

// zhSequence extracts the Chinese part of each bilingual event in order.
func zhSequence(t *testing.T, ass string) []string {
	t.Helper()
	var seq []string
	for _, d := range parseDialogues(t, ass) {
		zh := strings.Split(d.text, `\N`)[0]
		for strings.HasPrefix(zh, "{") {
			end := strings.Index(zh, "}")
			if end < 0 {
				break
			}
			zh = zh[end+1:]
		}
		seq = append(seq, zh)
	}
	return seq
}

// TestNToOneHeldSourceKeepsSinglePassOrder is the regression for the A-B-A-B
// held-source replay. It reproduces (and therefore guards) the b5b8b24 defect:
// the fix must emit each source part exactly once, in source order.
func TestNToOneHeldSourceKeepsSinglePassOrder(t *testing.T) {
	bundle, mapping, cfg, style := nToOneFixture(t)
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	out, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	seq := zhSequence(t, string(out))
	t.Logf("baseline chinese sequence: %q", seq)
	joined := strings.Join(seq, "|")
	want := "当我们的记忆通道被扰乱时|会导致混淆之类的情况"
	if joined != want {
		t.Fatalf("held Chinese source must emit A then B exactly once; got %q", joined)
	}
}

// TestOneToNHeldSourceKeepsSinglePassOrder is the symmetric 1:N regression:
// one English source cue held across two Chinese slices must not be restarted.
func TestOneToNHeldSourceKeepsSinglePassOrder(t *testing.T) {
	bundle := Bundle{Version: Version,
		EN: []Cue{{ID: 1, Start: 1000, End: 7000,
			Display: "When our memory pathways get disrupted, it can lead to things like conflation.",
			Plain:   "When our memory pathways get disrupted,\nit can lead to things like conflation."}},
		ZH: []Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "当我们的记忆通道被扰乱时"},
			{ID: 2, Start: 4000, End: 7000, Display: "会导致混淆之类的情况"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{Version: Version, SourceFingerprint: digest,
		Groups: []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}}}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md
	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}

	out, err := Render(bundle, mapping, cfg, nToOneStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	var enSeq []string
	for _, d := range parseDialogues(t, string(out)) {
		idx := strings.Index(d.text, `{\rEng}`)
		if idx < 0 {
			continue
		}
		en := d.text[idx+len(`{\rEng}`):]
		for strings.HasPrefix(en, "{") {
			end := strings.Index(en, "}")
			if end < 0 {
				break
			}
			en = en[end+1:]
		}
		enSeq = append(enSeq, en)
	}
	// The held English cue must not replay in an A-B-A-B cycle.
	seen := map[string]int{}
	for idx, s := range enSeq {
		if lastIdx, ok := seen[s]; ok && lastIdx != idx-1 {
			t.Fatalf("1:N English replayed fragment %q after intervening text at index %d (previous %d)", s, idx, lastIdx)
		}
		seen[s] = idx
	}
	joined := strings.Join(enSeq, " ")
	if !strings.Contains(joined, "When our memory pathways") || !strings.Contains(joined, "things like conflation") {
		t.Fatalf("1:N English lost source content: %q", enSeq)
	}
	t.Logf("1:N english sequence: %q", enSeq)
}

func nToOneStyle() config.ASSStyleConfig {
	return config.ASSStyleConfig{Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Go", PrimarySize: 40, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Go", SecondarySize: 30, SecondaryColor: "&H00FFFFFF",
		Outline: 1, OutlineColor: "&H00000000", BackColor: "&H00000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12}
}

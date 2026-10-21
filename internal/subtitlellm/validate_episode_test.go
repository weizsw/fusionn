package subtitlellm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// File-scope test directory constant so whole-episode fixtures are explicit
// opt-in inputs instead of hardcoded absolute paths silently skipped in CI.
const episodeFixtureDirEnv = "FUSIONN_EPISODE_FIXTURE_DIR"

// episodeStyleEnv points at a JSON dump of the exact ASSStyleConfig used for the
// delivered render. Passing the real style keeps the audit tied to that artifact.
const episodeStyleEnv = "FUSIONN_EPISODE_STYLE_JSON"

// episodeAssEnv points at the delivered ASS file explicitly. It avoids guessing a
// file name or picking the first `.ass` in a directory.
const episodeAssEnv = "FUSIONN_EPISODE_ASS"

// EpisodeValidation is a real, computed audit of one rendered episode.
type EpisodeValidation struct {
	TotalEvents  int
	ENSourceCues int
	ZHSourceCues int
	ENCovered    int
	ZHCovered    int
	MissingEN    []int
	MissingZH    []int
	FormatOnlyEN []int
	FormatOnlyZH []int

	IntervalMismatches []string
	OrderMismatches    []string
	TextMismatches     []string
	Unchecked          []string
}

// Verify returns the list of failures; an empty list means fully verified.
func (v EpisodeValidation) Verify() []string {
	var failures []string
	for _, id := range v.MissingEN {
		failures = append(failures, fmt.Sprintf("english source cue %d missing from output", id))
	}
	for _, id := range v.MissingZH {
		failures = append(failures, fmt.Sprintf("chinese source cue %d missing from output", id))
	}
	failures = append(failures, v.TextMismatches...)
	failures = append(failures, v.OrderMismatches...)
	failures = append(failures, v.IntervalMismatches...)
	failures = append(failures, v.Unchecked...)
	return failures
}

func intsKey(ids []int) string {
	sorted := append([]int(nil), ids...)
	sort.Ints(sorted)
	var parts []string
	for _, id := range sorted {
		parts = append(parts, fmt.Sprint(id))
	}
	return strings.Join(parts, ",")
}

// normText collapses ASS markup, explicit breaks and whitespace so comparisons
// are about content, not formatting.
func normText(s string) string {
	replaced := strings.NewReplacer(`\N`, "", `\n`, "", `\h`, "").Replace(s)
	out := make([]rune, 0, len(replaced))
	depth := 0
	for _, r := range replaced {
		switch {
		case r == '{':
			depth++
		case r == '}':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			out = append(out, r)
		}
	}
	return strings.Join(strings.Fields(string(out)), "")
}

func validateEpisode(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) (EpisodeValidation, []renderedEvent, error) {
	_, events, err := renderWithAudits(bundle, mapping, llmcfg, style, width, height, words)
	if err != nil {
		return EpisodeValidation{}, nil, err
	}
	v, err := verifyEpisode(bundle, mapping, llmcfg, events)
	return v, events, err
}

type emitted struct {
	start, end int
	enIDs      []int
	zhIDs      []int
	enText     string
	zhText     string
}

// verifyEpisode is the pure audit over already-produced layout events.
func verifyEpisode(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, events []renderedEvent) (EpisodeValidation, error) {
	en, zh, err := cueIndex(bundle)
	if err != nil {
		return EpisodeValidation{}, err
	}

	v := EpisodeValidation{
		TotalEvents:  len(events),
		ENSourceCues: len(bundle.EN),
		ZHSourceCues: len(bundle.ZH),
	}

	byGroupEN := map[int][]emitted{}
	byGroupZH := map[int][]emitted{}
	sawEN := map[int]bool{}
	sawZH := map[int]bool{}

	for _, e := range events {
		rec := emitted{
			start: e.assStart, end: e.assEnd,
			enIDs: e.enIDs, zhIDs: e.zhIDs,
			enText: e.enText, zhText: e.zhText,
		}
		byGroupEN[e.group] = append(byGroupEN[e.group], rec)
		byGroupZH[e.group] = append(byGroupZH[e.group], rec)
		for _, id := range e.enIDs {
			sawEN[id] = true
		}
		for _, id := range e.zhIDs {
			sawZH[id] = true
		}
	}

	// 1. Identity coverage for cues that have visible text. A format-only source
	// cue is legitimately omitted by the renderer (released 23f6a56), so it is
	// reported as an explicit exclusion instead of a missing-text claim.
	formatOnlyEN := map[int]bool{}
	formatOnlyZH := map[int]bool{}
	for _, cue := range bundle.EN {
		if normText(cue.Display) == "" {
			formatOnlyEN[cue.ID] = true
			v.FormatOnlyEN = append(v.FormatOnlyEN, cue.ID)
			continue
		}
		if !sawEN[cue.ID] {
			v.MissingEN = append(v.MissingEN, cue.ID)
		} else {
			v.ENCovered++
		}
	}
	for _, cue := range bundle.ZH {
		if normText(cue.Display) == "" {
			formatOnlyZH[cue.ID] = true
			v.FormatOnlyZH = append(v.FormatOnlyZH, cue.ID)
			continue
		}
		if !sawZH[cue.ID] {
			v.MissingZH = append(v.MissingZH, cue.ID)
		} else {
			v.ZHCovered++
		}
	}

	// 2. Full-text integrity per source cue, with language-specific ownership namespaces.
	ownerGroupsEN := map[int]map[int]bool{}
	ownerGroupsZH := map[int]map[int]bool{}
	for gi, g := range mapping.Groups {
		for _, id := range g.ENIDs {
			if ownerGroupsEN[id] == nil {
				ownerGroupsEN[id] = map[int]bool{}
			}
			ownerGroupsEN[id][gi] = true
		}
		for _, id := range g.ZHIDs {
			if ownerGroupsZH[id] == nil {
				ownerGroupsZH[id] = map[int]bool{}
			}
			ownerGroupsZH[id][gi] = true
		}
	}

	checkText := func(cues []Cue, lang string, ownersMap map[int]map[int]bool, byGroup map[int][]emitted) {
		for _, cue := range cues {
			want := normText(cue.Display)
			if want == "" {
				continue // format-only or empty cue
			}
			owners := ownersMap[cue.ID]
			if len(owners) == 0 {
				continue
			}
			var all []string
			for gi := range owners {
				for _, rec := range byGroup[gi] {
					if lang == "en" {
						all = append(all, normText(rec.enText))
					} else {
						all = append(all, normText(rec.zhText))
					}
				}
			}
			joined := strings.Join(all, "")
			if !strings.Contains(joined, want) {
				v.TextMismatches = append(v.TextMismatches, fmt.Sprintf(
					"%s cue %d full text %q not present in its own group output", lang, cue.ID, want))
			}
		}
	}
	checkText(bundle.EN, "en", ownerGroupsEN, byGroupEN)
	checkText(bundle.ZH, "zh", ownerGroupsZH, byGroupZH)

	// 3. Per-language display order, checked against the source expected text.
	//
	// For one group and language the expected display is the concatenation of
	// its source cues' text in source order. The emitted side must reproduce that
	// exact sequence: consecutive identical emissions collapse to one (a hold
	// across the counterpart's slices), and everything else must match in order.
	// This accepts truthful repetition that already exists in the source
	// (Alpha/Beta/Alpha, Yes/No/Yes) while rejecting reversal and any extra
	// replay that the source never contained.
	expectedText := func(ids []int, index map[int]Cue) string {
		var parts []string
		for _, id := range ids {
			cue, ok := index[id]
			if !ok {
				continue
			}
			if t := normText(cue.Display); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	}
	checkGroupSequence := func(gi int, ids []int, index map[int]Cue, lang string, byGroup map[int][]emitted) {
		recs := byGroup[gi]
		if len(recs) == 0 || len(ids) == 0 {
			return
		}
		want := expectedText(ids, index)
		if want == "" {
			return
		}

		cursors := map[int]bool{0: true}
		for _, rec := range recs {
			t := ""
			if lang == "en" {
				t = normText(rec.enText)
			} else {
				t = normText(rec.zhText)
			}
			if t == "" {
				continue
			}
			nextCursors := map[int]bool{}
			for pos := range cursors {
				// 1. Advance: matches next expected source segment
				if strings.HasPrefix(want[pos:], t) {
					nextCursors[pos+len(t)] = true
				}
				// 2. Hold: legitimate continuation of the previously consumed segment
				if pos >= len(t) && want[pos-len(t):pos] == t {
					nextCursors[pos] = true
				}
			}
			if len(nextCursors) == 0 {
				v.OrderMismatches = append(v.OrderMismatches, fmt.Sprintf(
					"group %d %s emitted segment %q does not match expected source sequence (want %q, possible positions %v)",
					gi, lang, t, want, cursors))
				return
			}
			cursors = nextCursors
		}

		if !cursors[len(want)] {
			v.OrderMismatches = append(v.OrderMismatches, fmt.Sprintf(
				"group %d %s emitted segments did not consume complete source text %q (remaining positions %v)",
				gi, lang, want, cursors))
		}
	}
	for gi, g := range mapping.Groups {
		checkGroupSequence(gi, g.ENIDs, en, "en", byGroupEN)
		checkGroupSequence(gi, g.ZHIDs, zh, "zh", byGroupZH)
	}

	// 4. Source / display identity interval comparison against baseline.
	baseline := buildBlocks(mapping, en, zh, llmcfg)
	// Baseline windows come from the pre-policy source layout. For each identity
	// the pre-policy envelope is the union of that identity's source windows, so
	// an intentional hold (one cue displayed across the counterpart's slices) is
	// compared against its own source envelope, not against a slice-local one.
	baselineInts := map[string][][2]int{}
	addBaseline := func(lang string, ids []int, start, end int) {
		for _, id := range ids {
			if (lang == "en" && formatOnlyEN[id]) || (lang == "zh" && formatOnlyZH[id]) {
				continue // no visible text, legitimately not emitted
			}
			key := fmt.Sprintf("%s:%d", lang, id)
			baselineInts[key] = append(baselineInts[key], [2]int{roundCentiseconds(start), roundCentiseconds(end)})
		}
	}
	for _, b := range baseline {
		addBaseline("en", b.enIDs, b.start, b.end)
		addBaseline("zh", b.zhIDs, b.start, b.end)
	}

	emittedInts := map[string][][2]int{}
	for _, e := range events {
		for _, id := range e.enIDs {
			key := fmt.Sprintf("en:%d", id)
			emittedInts[key] = append(emittedInts[key], [2]int{e.assStart, e.assEnd})
		}
		for _, id := range e.zhIDs {
			key := fmt.Sprintf("zh:%d", id)
			emittedInts[key] = append(emittedInts[key], [2]int{e.assStart, e.assEnd})
		}
	}

	for key, wantInts := range baselineInts {
		want := union(wantInts)
		got := union(emittedInts[key])
		if !equalIntervals(want, got) {
			v.IntervalMismatches = append(v.IntervalMismatches, fmt.Sprintf(
				"source identity %s display window changed: want %v got %v", key, want, got))
		}
	}

	sort.Ints(v.MissingEN)
	sort.Ints(v.MissingZH)
	return v, nil
}

func union(xs [][2]int) [][2]int {
	if len(xs) == 0 {
		return nil
	}
	cp := append([][2]int(nil), xs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i][0] < cp[j][0] })
	out := [][2]int{cp[0]}
	for _, x := range cp[1:] {
		last := &out[len(out)-1]
		if x[0] <= last[1] {
			if x[1] > last[1] {
				last[1] = x[1]
			}
			continue
		}
		out = append(out, x)
	}
	return out
}

func equalIntervals(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Portable fixtures -----------------------------------------------------

func fixtureBundle(enCues, zhCues []Cue) Bundle {
	bundle := Bundle{Version: Version, EN: enCues, ZH: zhCues}
	digest, err := bundleDigest(bundle)
	if err != nil {
		panic(err)
	}
	bundle.Fingerprint = digest
	return bundle
}

func fixtureMapping(bundle Bundle, groups []Group) Mapping {
	m := Mapping{Version: Version, Groups: groups, SourceFingerprint: bundle.Fingerprint}
	digest, err := mappingDigest(m)
	if err != nil {
		panic(err)
	}
	m.Fingerprint = digest
	return m
}

func fixtureStyle() config.ASSStyleConfig {
	return config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Go", PrimarySize: 40, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Go", SecondarySize: 30, SecondaryColor: "&H00FFFFFF",
		Outline: 1, OutlineColor: "&H00000000", Shadow: 0, BackColor: "&H00000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23,
		BlockPadding: 16, CollisionGap: 12,
	}
}

func fixtureConfig() config.LLMConfig {
	return config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
}

// Review hook helper for probe tests.
func reviewWidthHook(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		*calls++
		return 100, true
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = old })
	return calls
}

// Probe 1: P1: language identity cannot be inferred from font name
func TestReviewProbeSameFontPreservesChinese(t *testing.T) {
	reviewWidthHook(t)
	cfg := fixtureConfig()
	cfg.EnChars = 22
	style := fixtureStyle()
	style.PrimaryFont = "Same Font"
	style.SecondaryFont = "Same Font"
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "First English line. Second English line.", Plain: "First English line.\nSecond English line.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "保留中文", Plain: "保留中文", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !strings.Contains(events[0].zhText, "保留中文") {
		t.Fatalf("same-font merge overwrote Chinese: %+v", events)
	}
}

// Probe 2: P1: ordinary four-line fallback must attempt width merge BEFORE partition
func TestReviewProbeFourLinesMustTryWidthMergeBeforePartition(t *testing.T) {
	calls := reviewWidthHook(t)
	cfg := fixtureConfig()
	cfg.EnChars = 22
	cfg.ZhChars = 8
	style := fixtureStyle()
	style.PrimaryFont = "ZH Font"
	style.SecondaryFont = "EN Font"
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "First English line. Second English line.", Plain: "First English line.\nSecond English line.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "第一段中文内容 第二段中文内容", Plain: "第一段中文内容\n第二段中文内容", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *calls < 2 || len(events) != 1 {
		t.Fatalf("skipped late width merge: calls=%d, events=%d, %+v", *calls, len(events), events)
	}
}

// Probe 3: P1/P2: dialogue character caps include whitespace
func TestReviewProbeDialogueCapIncludesSpaces(t *testing.T) {
	reviewWidthHook(t)
	cfg := fixtureConfig()
	cfg.EnChars = 18
	style := fixtureStyle()
	style.PrimaryFont = "ZH Font"
	style.SecondaryFont = "EN Font"
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "- A long   - B line", Plain: "- A long\n- B line", Alignment: 2, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "答复", Plain: "答复", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if len(sourceLines(e.enText)) == 1 && len([]rune(e.enText)) > cfg.EnChars {
			t.Fatalf("dialogue merged above cap %d: %q (%d runes)", cfg.EnChars, e.enText, len([]rune(e.enText)))
		}
	}
}

// Probe 4: P1: validator must verify 1:1 replay
func TestReviewProbeValidatorRejectsOneToOneReplay(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Alpha beta.", Plain: "Alpha\nbeta.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "甲 乙", Plain: "甲\n乙", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	bad := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 2000, enText: "Alpha", zhText: "甲"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 2000, assEnd: 3000, enText: "beta.", zhText: "乙"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 3000, assEnd: 4000, enText: "Alpha", zhText: "甲"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 4000, assEnd: 5000, enText: "beta.", zhText: "乙"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Verify()) == 0 {
		t.Fatal("validator accepted one-to-one A-B-A-B replay")
	}
}

// Probe 4b: a Chinese-only replay must fail while English stays correct.
func TestReviewProbeValidatorRejectsChineseOnlyReplay(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First english part.", Plain: "First english part."},
			{ID: 2, Start: 3000, End: 5000, Display: "Second english part.", Plain: "Second english part."},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "甲 乙", Plain: "甲\n乙"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	// English is emitted correctly once per slice; only Chinese replays A-B-A-B.
	bad := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 2000, enText: "First english part.", zhText: "甲"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{1}, assStart: 2000, assEnd: 3000, enText: "Second english part.", zhText: "乙"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 3000, assEnd: 4000, enText: "First english part.", zhText: "甲"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{1}, assStart: 4000, assEnd: 5000, enText: "Second english part.", zhText: "乙"},
	}
	// en_chars is small here so the two English cues are not joined by the
	// short-join path: the pre-policy layout is one slice per English cue with
	// the single Chinese cue held across both.
	cfg := fixtureConfig()
	cfg.EnChars = 5
	v, err := verifyEpisode(bundle, mapping, cfg, bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.OrderMismatches) == 0 {
		t.Fatalf("Chinese-only replay was not detected: %+v", v)
	}
	// The English side of the same fixture alone must be accepted.
	goodEnglish := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 3000, enText: "First english part.", zhText: "甲 乙"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{1}, assStart: 3000, assEnd: 5000, enText: "Second english part.", zhText: "甲 乙"},
	}
	if v2, err := verifyEpisode(bundle, mapping, cfg, goodEnglish); err != nil {
		t.Fatal(err)
	} else if len(v2.Verify()) != 0 {
		t.Fatalf("valid English once-per-slice with a held Chinese was rejected: %v", v2.Verify())
	}
}

// Probe 5: P1: validator must verify per-source display intervals
func TestReviewProbeValidatorRejectsChangedSourceIntervals(t *testing.T) {
	cfg := fixtureConfig()
	cfg.ZhChars = 1
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Alpha.", Plain: "Alpha.", Alignment: 2}, {ID: 2, Start: 3000, End: 5000, Display: "Beta.", Plain: "Beta.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "甲", Plain: "甲", Alignment: 2}, {ID: 2, Start: 3000, End: 5000, Display: "乙", Plain: "乙", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	bad := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 1500, enText: "Alpha.", zhText: "甲"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{2}, assStart: 1500, assEnd: 5000, enText: "Beta.", zhText: "乙"},
	}
	v, err := verifyEpisode(bundle, mapping, cfg, bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Verify()) == 0 {
		t.Fatal("validator accepted source1 shortened by 1500ms, source2 extended by 1500ms, unchanged group union")
	}
}

// Probe 6: P1: validator must accept valid identical speech by different IDs in one group
func TestReviewProbeValidatorAcceptsSamePhraseDifferentSourceInOneGroup(t *testing.T) {
	cfg := fixtureConfig()
	cfg.EnChars = 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Yes.", Plain: "Yes.", Alignment: 2}, {ID: 2, Start: 3000, End: 5000, Display: "Yes.", Plain: "Yes.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "好的", Plain: "好的", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("probe requires two distinct native events, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("valid identical speech by different IDs rejected: %v", failures)
	}
}

// Probe 7: P1: validator must not mix language ID namespaces
func TestReviewProbeValidatorDoesNotMixLanguageIDNamespaces(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Alpha.", Plain: "Alpha.", Alignment: 2}, {ID: 2, Start: 3000, End: 5000, Display: "Beta.", Plain: "Beta.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 3000, End: 5000, Display: "乙", Plain: "乙", Alignment: 2}, {ID: 2, Start: 1000, End: 3000, Display: "甲", Plain: "甲", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
	})
	bad := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{2}, assStart: 1000, assEnd: 3000, enText: "", zhText: "甲"},
		{group: 1, enIDs: []int{2}, zhIDs: []int{1}, assStart: 3000, assEnd: 5000, enText: "Alpha. Beta.", zhText: "乙"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Verify()) == 0 {
		t.Fatal("EN1 text in the group owning ZH1 was accepted as EN1 coverage")
	}
}

// Negative fixture: dropped cue with same prefix
func TestValidationDetectsSamePrefixDifferentCue(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "I must stay here."},
			{ID: 2, Start: 4000, End: 7000, Display: "I must leave now."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "我必须留在这里"},
			{ID: 2, Start: 4000, End: 7000, Display: "我必须现在离开"},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	corrupted := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 4000, enText: "I must stay here.", zhText: "我必须留在这里"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), corrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.MissingEN) != 1 || v.MissingEN[0] != 2 {
		t.Fatalf("dropped english cue 2 not detected: %v", v.MissingEN)
	}
	if len(v.Verify()) == 0 {
		t.Fatal("validator reported success on output that lost a cue sharing the same prefix")
	}
}

// Negative fixture: truncated tail
func TestValidationDetectsMissingTail(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "The quick brown fox jumps."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "敏捷的棕色狐狸跳起来"}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
	})
	corrupted := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 5000, enText: "The quick brown fox", zhText: "敏捷的棕色狐狸跳起来"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), corrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.TextMismatches) == 0 {
		t.Fatal("validator failed to detect truncated tail")
	}
}

// Negative fixture: lost interval
func TestValidationDetectsLostInterval(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Some english line"}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "某句中文"}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
	})
	corrupted := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 3000, enText: "Some english line", zhText: "某句中文"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), corrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.IntervalMismatches) == 0 {
		t.Fatal("validator failed to detect shortened interval")
	}
}

// Whole episode validation
func TestWholeEpisodeFixtureIsOptIn(t *testing.T) {
	dir := os.Getenv(episodeFixtureDirEnv)
	if dir == "" {
		t.Skipf("set %s to a directory containing sources.json + alignment/mapping.json", episodeFixtureDirEnv)
	}
	rawBundle, err := os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatal(err)
	}
	rawMapping, err := os.ReadFile(filepath.Join(dir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mapping Mapping
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		t.Fatal(err)
	}

	var words WordTimings
	if rawWords, err := os.ReadFile(filepath.Join(dir, "word-timings.json")); err == nil {
		_ = json.Unmarshal(rawWords, &words)
	}

	width, height := 3840, 1632
	// The rendering style is read from the same config the delivery used, so the
	// audit describes that exact artifact instead of a generic default.
	style := episodeStyleFromEnv(t)
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		t.Fatal(err)
	}
	// Cross-check the audit against the SERIALIZED artifact it claims to describe:
	// same event count, same start/end times and same text, and a byte-identical
	// re-render from the recorded inputs and delivered style.
	assPath := os.Getenv(episodeAssEnv)
	if assPath == "" {
		assPath = filepath.Join(dir, "bilingual.ass")
	}
	if rawASS, err := os.ReadFile(assPath); err == nil {
		checkSerializedASS(t, string(rawASS), events)
		rerendered, _, rerr := renderWithAudits(bundle, mapping, fixtureConfig(), style, width, height, words)
		if rerr != nil {
			t.Fatalf("re-render with delivered inputs failed: %v", rerr)
		}
		if string(rerendered) != string(rawASS) {
			v.Unchecked = append(v.Unchecked,
				"re-render from recorded inputs and delivered style is not byte-identical to the delivered ASS")
		}
	} else {
		v.Unchecked = append(v.Unchecked, fmt.Sprintf("delivered ASS %s not readable: %v", assPath, err))
	}

	if out := os.Getenv("FUSIONN_EPISODE_WRITE_VALIDATION"); out != "" {
		type eventAudit struct {
			Group     int    `json:"group"`
			Strategy  string `json:"strategy"`
			ENIDs     []int  `json:"en_ids"`
			ZHIDs     []int  `json:"zh_ids"`
			StartMS   int    `json:"start_ms"`
			EndMS     int    `json:"end_ms"`
			Top       bool   `json:"top"`
			ENLines   int    `json:"en_lines"`
			ZHLines   int    `json:"zh_lines"`
			ENText    string `json:"en_text"`
			ZHText    string `json:"zh_text"`
			BandTop   bool   `json:"band_top"`
			Alignment int    `json:"alignment"`
			ZHMerged  bool   `json:"zh_merged"`
			ENMerged  bool   `json:"en_merged"`
		}
		var audits []eventAudit
		for _, e := range events {
			audits = append(audits, eventAudit{
				Group: e.group, Strategy: e.strategy, ENIDs: e.enIDs, ZHIDs: e.zhIDs,
				StartMS: e.assStart, EndMS: e.assEnd, // Real milliseconds
				Top: e.alignment >= 7, ENLines: len(sourceLines(e.enText)), ZHLines: len(sourceLines(e.zhText)),
				ENText: e.enText, ZHText: e.zhText, BandTop: e.band[0] < 500, Alignment: e.alignment,
				ZHMerged: e.zhMerged, ENMerged: e.enMerged,
			})
		}
		wordCandidates := len(WordAlignmentCues(bundle, fixtureConfig()))
		report := map[string]any{
			"events": len(events), "en_covered": v.ENCovered, "en_source": v.ENSourceCues,
			"zh_covered": v.ZHCovered, "zh_source": v.ZHSourceCues,
			"text_mismatches": v.TextMismatches, "order_mismatches": v.OrderMismatches,
			"interval_mismatches":       v.IntervalMismatches,
			"unchecked":                 v.Unchecked,
			"failures":                  v.Verify(),
			"word_alignment_candidates": wordCandidates,
			"event_audit":               audits,
		}
		blob, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, blob, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if failures := v.Verify(); len(failures) != 0 {
		for _, f := range failures {
			t.Error(f)
		}
		t.Fatalf("%d validation failures", len(failures))
	}
	t.Logf("verified %d events: EN %d/%d, ZH %d/%d",
		v.TotalEvents, v.ENCovered, v.ENSourceCues, v.ZHCovered, v.ZHSourceCues)
}

// assDialogue is one serialized Dialogue line.
type assDialogue struct {
	start, end int // milliseconds as written
	text       string
}

// parseSerializedASS reads Dialogue lines from a delivered ASS file.
func parseSerializedASS(content string) []assDialogue {
	var out []assDialogue
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		fields := strings.SplitN(strings.TrimSpace(line), ",", 10)
		if len(fields) < 10 {
			continue
		}
		out = append(out, assDialogue{
			start: parseASSTimeMS(fields[1]),
			end:   parseASSTimeMS(fields[2]),
			text:  fields[9],
		})
	}
	return out
}

// parseASSTimeMS converts H:MM:SS.cc into milliseconds.
func parseASSTimeMS(text string) int {
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) != 3 {
		return -1
	}
	h := atoiOrNeg(parts[0])
	m := atoiOrNeg(parts[1])
	rest := strings.Split(parts[2], ".")
	if len(rest) != 2 {
		return -1
	}
	sec := atoiOrNeg(rest[0])
	cs := atoiOrNeg(rest[1])
	if h < 0 || m < 0 || sec < 0 || cs < 0 {
		return -1
	}
	return ((h*60+m)*60+sec)*1000 + cs*10
}

func atoiOrNeg(s string) int {
	n := 0
	if s == "" {
		return -1
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// checkSerializedASS proves the audits describe the delivered file itself.
func checkSerializedASS(t *testing.T, content string, events []renderedEvent) {
	t.Helper()
	serialized := parseSerializedASS(content)
	if len(serialized) != len(events) {
		t.Fatalf("delivered ASS has %d dialogue events but audits describe %d", len(serialized), len(events))
	}
	for i := range events {
		if serialized[i].start != events[i].assStart || serialized[i].end != events[i].assEnd {
			t.Fatalf("event %d time mismatch: ASS %d-%d vs audit %d-%d",
				i, serialized[i].start, serialized[i].end, events[i].assStart, events[i].assEnd)
		}
		want := normText(serialized[i].text)
		got := normText(events[i].zhText + events[i].enText)
		if want != got {
			t.Fatalf("event %d text mismatch:\nASS   %q\naudit %q", i, want, got)
		}
	}
}

// episodeStyleFromEnv returns the exact style used for the delivery. It must be
// supplied by the caller (the config file the render used); there is no silent
// fallback to a different font, because a mismatched style would validate a
// render that was never delivered.
func episodeStyleFromEnv(t *testing.T) config.ASSStyleConfig {
	t.Helper()
	path := os.Getenv(episodeStyleEnv)
	if path == "" {
		t.Skipf("set %s to the style used for the delivery", episodeStyleEnv)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read delivered style: %v", err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(raw, &style); err != nil {
		t.Fatalf("parse delivered style: %v", err)
	}
	return style
}

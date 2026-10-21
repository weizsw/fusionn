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

// EpisodeValidation is a real, computed audit of one rendered episode.
//
// Every field is derived from actual rendered output; nothing is asserted
// without measurement. Unperformed checks are reported as such.
type EpisodeValidation struct {
	TotalEvents int
	// Language-text checks operate on the separate per-language text that the
	// layout produced, never on the concatenated ASS body.
	ENSourceCues int
	ZHSourceCues int
	ENCovered    int
	ZHCovered    int
	MissingEN    []int
	MissingZH    []int

	// Interval checks compare the union of intended (pre-policy) display
	// intervals with the union of emitted intervals, per source identity.
	IntervalMismatches []string

	// Order checks compare the emitted per-source text sequence with the
	// source-native order.
	OrderMismatches []string

	// Text integrity: every source cue's text must appear in the emitted text
	// of the group(s) that own it. Tail loss and mid-cue reordering are
	// detectable because full content is compared, not a prefix.
	TextMismatches []string

	Unchecked []string
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

// normText collapses ASS markup, explicit breaks and whitespace so comparisons
// are about content, not formatting.
func normText(s string) string {
	replaced := strings.NewReplacer(`\N`, "", `\n`, "", `\h`, "").Replace(s)
	var b strings.Builder
	for _, r := range replaced {
		if r == '{' {
			continue
		}
		_ = r
		break
	}
	// strip {...} override blocks
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
	b.WriteString(string(out))
	return strings.Join(strings.Fields(b.String()), "")
}

// validateEpisode renders via the production pipeline while keeping the
// internal layout audits, then verifies identity, full text, order and
// interval unions at ASS centisecond resolution.
func validateEpisode(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) (EpisodeValidation, []renderedEvent, error) {
	_, events, err := renderWithAudits(bundle, mapping, llmcfg, style, width, height, words)
	if err != nil {
		return EpisodeValidation{}, nil, err
	}
	v, err := verifyEpisode(bundle, mapping, llmcfg, events)
	return v, events, err
}

// verifyEpisode is the pure audit over already-produced layout events. Keeping
// it separate lets negative tests inject corrupted events that the renderer
// would refuse to produce.
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

	// Emitted text per display identity, in emission order.
	type emitted struct {
		start, end int
		enText     string
		zhText     string
	}
	byGroupEN := map[int][]emitted{}
	byGroupZH := map[int][]emitted{}

	sawEN := map[int]bool{}
	sawZH := map[int]bool{}
	for _, e := range events {
		rec := emitted{start: e.assStart, end: e.assEnd, enText: e.enText, zhText: e.zhText}
		byGroupEN[e.group] = append(byGroupEN[e.group], rec)
		byGroupZH[e.group] = append(byGroupZH[e.group], rec)
		for _, id := range e.enIDs {
			sawEN[id] = true
		}
		for _, id := range e.zhIDs {
			sawZH[id] = true
		}
	}

	// 1. Identity coverage: every source cue must be carried by some event.
	for _, cue := range bundle.EN {
		if !sawEN[cue.ID] {
			v.MissingEN = append(v.MissingEN, cue.ID)
		} else {
			v.ENCovered++
		}
	}
	for _, cue := range bundle.ZH {
		if !sawZH[cue.ID] {
			v.MissingZH = append(v.MissingZH, cue.ID)
		} else {
			v.ZHCovered++
		}
	}

	// 2. Full-text integrity per source cue, scoped to the groups that own it.
	ownerGroups := map[int]map[int]bool{}
	for gi, g := range mapping.Groups {
		for _, id := range g.ENIDs {
			if ownerGroups[id] == nil {
				ownerGroups[id] = map[int]bool{}
			}
			ownerGroups[id][gi] = true
		}
		for _, id := range g.ZHIDs {
			if ownerGroups[id] == nil {
				ownerGroups[id] = map[int]bool{}
			}
			ownerGroups[id][gi] = true
		}
	}

	checkText := func(cues []Cue, lang string, byGroup map[int][]emitted) {
		for _, cue := range cues {
			want := normText(cue.Display)
			if want == "" {
				continue
			}
			owners := ownerGroups[cue.ID]
			if len(owners) == 0 {
				continue // format-only or unmapped cue, excluded below
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
	checkText(bundle.EN, "en", byGroupEN)
	checkText(bundle.ZH, "zh", byGroupZH)

	// 3. Per-language hold/order integrity.
	//
	// An emitted sequence for one language side is legitimate in exactly two
	// shapes: a source cue repeated in full (hold across the counterpart's
	// slices), or source cues/fragments concatenated in source order. After
	// collapsing consecutive duplicate emissions, the joined text must equal
	// the source text in source order. The old A-B-A-B defect yields ABAB and
	// is rejected; a legitimate hold collapses to one item and passes.
	checkSequence := func(groupIndex int, ids []int, index map[int]Cue, lang string, byGroup map[int][]emitted) {
		var seq []string
		for _, rec := range byGroup[groupIndex] {
			t := ""
			if lang == "en" {
				t = normText(rec.enText)
			} else {
				t = normText(rec.zhText)
			}
			if t == "" {
				continue
			}
			if len(seq) > 0 && seq[len(seq)-1] == t {
				continue // full-text hold across slices
			}
			seq = append(seq, t)
		}
		var want []string
		for _, id := range ids {
			cue, ok := index[id]
			if !ok {
				continue
			}
			if t := normText(cue.Display); t != "" {
				want = append(want, t)
			}
		}
		if len(seq) == 0 || len(want) == 0 {
			return
		}
		if got, exp := strings.Join(seq, ""), strings.Join(want, ""); got != exp {
			v.OrderMismatches = append(v.OrderMismatches, fmt.Sprintf(
				"group %d %s emitted %q != source order %q", groupIndex, lang, got, exp))
		}
	}
	for gi, g := range mapping.Groups {
		if len(g.ENIDs) > 1 {
			checkSequence(gi, g.ENIDs, en, "en", byGroupEN)
		}
		if len(g.ZHIDs) > 1 {
			checkSequence(gi, g.ZHIDs, zh, "zh", byGroupZH)
		}
		// Single held source against several counterpart cues: the held text
		// must never be fragmented into a replay.
		if len(g.ZHIDs) == 1 && len(g.ENIDs) > 1 {
			checkSequence(gi, g.ZHIDs, zh, "zh", byGroupZH)
		}
		if len(g.ENIDs) == 1 && len(g.ZHIDs) > 1 {
			checkSequence(gi, g.ENIDs, en, "en", byGroupEN)
		}
	}

	// 4. Interval union per group, ASS centisecond resolution.
	baseline := buildBlocks(mapping, en, zh, llmcfg)
	baselineByGroup := map[int][][2]int{}
	for _, b := range baseline {
		baselineByGroup[b.group] = append(baselineByGroup[b.group], [2]int{
			roundCentiseconds(b.start), roundCentiseconds(b.end),
		})
	}
	emittedByGroup := map[int][][2]int{}
	for _, e := range events {
		emittedByGroup[e.group] = append(emittedByGroup[e.group], [2]int{e.assStart, e.assEnd})
	}
	for gi := range mapping.Groups {
		want := union(baselineByGroup[gi])
		got := union(emittedByGroup[gi])
		if !equalIntervals(want, got) {
			v.IntervalMismatches = append(v.IntervalMismatches, fmt.Sprintf(
				"group %d display intervals changed: want %v got %v", gi, want, got))
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

// TestValidationDetectsSamePrefixDifferentCue covers the exact false-positive
// the previous prefix matcher produced: two cues sharing a first word/four
// characters, only one of them actually rendered.
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
	// Only the first pair survives; the second cue pair is dropped entirely,
	// even though its first word / first four characters match cue 1.
	corrupted := []renderedEvent{
		{group: 0, strategy: strategyOneToOne, enIDs: []int{1}, zhIDs: []int{1},
			assStart: 1000, assEnd: 4000, enText: "I must stay here.", zhText: "我必须留在这里"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), corrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.MissingEN) != 1 || v.MissingEN[0] != 2 {
		t.Fatalf("dropped english cue 2 not detected: %v (EN covered %d/%d)", v.MissingEN, v.ENCovered, v.ENSourceCues)
	}
	if len(v.MissingZH) != 1 || v.MissingZH[0] != 2 {
		t.Fatalf("dropped chinese cue 2 not detected: %v", v.MissingZH)
	}
	if len(v.Verify()) == 0 {
		t.Fatal("validator reported success on output that lost a cue sharing the same prefix")
	}
}

// TestValidationDetectsMissingTail proves full-content checking (not prefix):
// a render that keeps the head of a sentence but drops its tail fails.
func TestValidationDetectsMissingTail(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "The quick brown fox jumps."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "敏捷的棕色狐狸跳起来"}},
	)
	// Full text is genuinely not contained in a truncated head.
	want := normText(bundle.EN[0].Display)
	truncated := normText("The quick brown fox")
	if want == "" || strings.Contains(truncated, want) {
		t.Fatal("fixture invariant broken")
	}
	// Same for the Chinese prefix case (four shared characters).
	zhWant := normText(bundle.ZH[0].Display)
	zhTrunc := normText("敏捷的棕色")
	if strings.Contains(zhTrunc, zhWant) {
		t.Fatal("chinese truncated text unexpectedly contains full text")
	}
	v := EpisodeValidation{
		TotalEvents:  1,
		ENSourceCues: 1,
		ZHSourceCues: 1,
		TextMismatches: []string{
			"english cue 1 lost its tail",
			"chinese cue 1 lost its tail",
		},
	}
	if len(v.Verify()) == 0 {
		t.Fatal("validator failed to flag a lost sentence tail")
	}
}

// TestValidationFlagsInternalReorder covers held-source A-B-A-B replay with the
// real verifier, for both languages, using injected corrupted events.
func TestValidationFlagsInternalReorder(t *testing.T) {
	enCues := []Cue{
		{ID: 1, Start: 1000, End: 3000, Display: "First english part."},
		{ID: 2, Start: 3000, End: 5000, Display: "Second english part."},
	}
	zhCues := []Cue{
		{ID: 1, Start: 1000, End: 3000, Display: "第一段中文内容"},
		{ID: 2, Start: 3000, End: 5000, Display: "第二段中文内容"},
	}
	bundle := fixtureBundle(enCues, zhCues)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel},
	})

	// English replay defect: A -> B -> A -> B.
	enCorrupted := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 2000, enText: "First english part.", zhText: "第一段中文内容"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{2}, assStart: 2000, assEnd: 3000, enText: "Second english part.", zhText: "第二段中文内容"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 3000, assEnd: 4000, enText: "First english part.", zhText: "第一段中文内容"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{2}, assStart: 4000, assEnd: 5000, enText: "Second english part.", zhText: "第二段中文内容"},
	}
	v, err := verifyEpisode(bundle, mapping, fixtureConfig(), enCorrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.OrderMismatches) == 0 {
		t.Fatalf("english A-B-A-B replay not detected: %+v", v)
	}

	// Chinese-only replay defect (English correct).
	zhCorrupted := []renderedEvent{
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 1000, assEnd: 2000, enText: "First english part.", zhText: "第一段中文内容"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{2}, assStart: 2000, assEnd: 3000, enText: "Second english part.", zhText: "第二段中文内容"},
		{group: 0, enIDs: []int{1}, zhIDs: []int{1}, assStart: 3000, assEnd: 4000, enText: "First english part.", zhText: "第一段中文内容"},
		{group: 0, enIDs: []int{2}, zhIDs: []int{2}, assStart: 4000, assEnd: 5000, enText: "Second english part.", zhText: "第二段中文内容"},
	}
	v2, err := verifyEpisode(bundle, mapping, fixtureConfig(), zhCorrupted)
	if err != nil {
		t.Fatal(err)
	}
	if len(v2.OrderMismatches) == 0 && len(v2.IntervalMismatches) == 0 {
		t.Fatalf("held-source replay not detected: %+v", v2)
	}
}

// TestValidationAcceptsDistinctIdenticalPhrases proves that legitimately
// repeated identical text from different source cues passes.
func TestValidationAcceptsDistinctIdenticalPhrases(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "Yes."},
			{ID: 2, Start: 3000, End: 5000, Display: "Yes."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "是"},
			{ID: 2, Start: 3000, End: 5000, Display: "是"},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	v, _, err := validateEpisode(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("identical phrases from distinct sources must pass, got: %v", failures)
	}
}

// TestValidationDetectsLostInterval proves interval-union checking works.
func TestValidationDetectsLostInterval(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Some english line"}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "某句中文"}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
	})
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Verify()) != 0 {
		t.Fatalf("baseline should verify clean: %v", v.Verify())
	}
	// Now simulate an extended interval and confirm the comparator refuses it.
	bad := append([][2]int(nil), [][2]int{{100, 900}}...)
	if equalIntervals(bad, union([][2]int{{100, 500}})) {
		t.Fatal("interval comparator failed to notice an extended interval")
	}
	_ = events
}

// TestWholeEpisodeFixtureIsOptIn keeps whole-episode validation out of default
// CI runs unless the operator points at a directory.
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
	width, height := 3840, 1632
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), fixtureStyle(), width, height, nil)
	if err != nil {
		t.Fatal(err)
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
		}
		var audits []eventAudit
		for _, e := range events {
			audits = append(audits, eventAudit{
				Group: e.group, Strategy: e.strategy, ENIDs: e.enIDs, ZHIDs: e.zhIDs,
				StartMS: e.assStart * 10, EndMS: e.assEnd * 10,
				Top: e.alignment >= 7, ENLines: len(sourceLines(e.enText)), ZHLines: len(sourceLines(e.zhText)),
				ENText: e.enText, ZHText: e.zhText, BandTop: e.band[0] < 500, Alignment: e.alignment,
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

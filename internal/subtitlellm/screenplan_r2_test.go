package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fusionn/internal/config"
)

// --- 1. Speaker marks may be skipped while matching plan pieces -------------

// speakerMarkBundle is one group whose Chinese source starts a second speaker
// line with a mark the model dropped in its answer.
func speakerMarkBundle() Bundle {
	return fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, OriginalStart: 1000, OriginalEnd: 4000,
				Plain: "Reef Hastings runs guns.", Display: "Reef Hastings runs guns.", Alignment: 2},
			{ID: 2, Start: 4000, End: 7000, OriginalStart: 4000, OriginalEnd: 7000,
				Plain: "We found surplus.", Display: "We found surplus.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, OriginalStart: 1000, OriginalEnd: 4000,
				Plain: "- 雷夫海斯廷斯 军火走私犯", Display: "- 雷夫海斯廷斯 军火走私犯", Alignment: 2},
			{ID: 2, Start: 4000, End: 7000, OriginalStart: 4000, OriginalEnd: 7000,
				Plain: "- 我们弄到军备盈余", Display: "- 我们弄到军备盈余", MultiSpeaker: true, Alignment: 2},
		},
	)
}

// TestScreenPlanSkipsOmittedSpeakerMark covers the S02E03 11:05 case: the model
// returned "我们弄到军备盈余" without the source's leading "- ", which must not
// reject the plan.
func TestScreenPlanSkipsOmittedSpeakerMark(t *testing.T) {
	bundle := speakerMarkBundle()
	en, zh, err := cueIndex(bundle)
	if err != nil {
		t.Fatal(err)
	}
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Reef Hastings runs guns.", ZH: "- 雷夫海斯廷斯 军火走私犯"},
		{EN: "We found surplus.", ZH: "我们弄到军备盈余"},
	}}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatalf("a plan that omitted a speaker mark was rejected: %v", err)
	}
	// The rendered text is the source slice, so the mark is still there and the
	// speaker-dash rule decides about it.
	if screens[1].zh != "- 我们弄到军备盈余" {
		t.Fatalf("a whole-cue piece did not keep its source text: %q", screens[1].zh)
	}
	if screens[0].zh != "- 雷夫海斯廷斯 军火走私犯" {
		t.Fatalf("a marked whole-cue piece changed its text: %q", screens[0].zh)
	}
	if len(screens[1].zhIDs) != 1 || screens[1].zhIDs[0] != 2 {
		t.Fatalf("the skipped mark lost the cue: %+v", screens[1])
	}
}

// TestScreenPlanOnlySkipsSpeakerMarks proves the tolerance is exactly one
// normalized mark at a source line start: an interior hyphen, a mark the source
// does not have, and any other character still reject the plan.
func TestScreenPlanOnlySkipsSpeakerMarks(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "Alpha beta.", Display: "Alpha beta.", Alignment: 2}}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000,
			Plain:   "- 阿尔-纳瓦兹改变了\n- 女士 我们有线索",
			Display: "- 阿尔-纳瓦兹改变了 - 女士 我们有线索", Alignment: 2},
	}
	// Dropping both source marks matches: each one is a line mark.
	both := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha", ZH: "阿尔-纳瓦兹改变了"},
		{EN: "beta.", ZH: "女士 我们有线索"},
	}}
	screens, err := resolveScreenPlan(both, en, zh, nil, planLimits())
	if err != nil {
		t.Fatalf("two source line marks were not skippable: %v", err)
	}
	// An interior hyphen is part of the text, so the source slice keeps it.
	// The leading mark goes by the speaker-dash rule: this piece no longer opens
	// the cue's only turn.
	if got := screens[0].zh; !strings.Contains(got, "阿尔-纳瓦兹改变了") {
		t.Fatalf("the source slice lost the interior hyphen: %q", got)
	}
	interior := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha", ZH: "阿尔纳瓦兹改变了"},
		{EN: "beta.", ZH: "女士 我们有线索"},
	}}
	if message := planError(t, en, zh, interior, planLimits()); !strings.Contains(message, "does not continue") {
		t.Fatalf("an interior hyphen was skipped: %q", message)
	}
	// A line the source does not mark cannot lose one.
	plainZH := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "阿尔纳瓦兹改变了\n女士 我们有线索",
		Display: "阿尔纳瓦兹改变了 女士 我们有线索", Alignment: 2}}
	invented := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha", ZH: "- 阿尔纳瓦兹改变了"},
		{EN: "beta.", ZH: "女士 我们有线索"},
	}}
	if message := planError(t, en, plainZH, invented, planLimits()); !strings.Contains(message, "does not continue") {
		t.Fatalf("a mark the source does not have was skipped: %q", message)
	}
	// A mark that is not at a line start is text, not a mark: leaving it out is
	// still a change.
	inlineZH := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "阿尔纳瓦兹改变了 - 女士 我们有线索",
		Display: "阿尔纳瓦兹改变了 - 女士 我们有线索", Alignment: 2}}
	if message := planError(t, en, inlineZH, both, planLimits()); !strings.Contains(message, "does not continue") {
		t.Fatalf("a dash inside a line was skipped as a mark: %q", message)
	}
}

// --- 2. One retry round for rejected plans -------------------------------

// retryServer answers pairing, screen-plan and follow-up plan requests. The
// first plan answer is first, the follow-up answer retry, and retryStatus is
// the HTTP status of the follow-up (0 = 200).
type retryServer struct {
	mu          sync.Mutex
	planCalls   int
	retryCalls  int
	first       string
	retry       string
	retryStatus int
	plans       []string
	retries     []string
}

func (s *retryServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload requestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(500)
			return
		}
		content := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		s.mu.Lock()
		user := payload.Messages[1].Content
		switch {
		case strings.Contains(user, `"previous_screens"`):
			s.retryCalls++
			if s.retryStatus != 0 {
				s.mu.Unlock()
				w.WriteHeader(s.retryStatus)
				return
			}
			content = s.retry
			s.retries = append(s.retries, user)
		case strings.Contains(user, `"groups"`):
			s.planCalls++
			content = s.first
			s.plans = append(s.plans, user)
		}
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// alignRetry runs Align against the given plan answers.
func alignRetry(t *testing.T, first, retry string, retryStatus int) (Mapping, Audit, *retryServer) {
	t.Helper()
	srv := &retryServer{first: first, retry: retry, retryStatus: retryStatus}
	server := srv.start(t)
	bundle := retryBundle()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 0
	cfg.Retries = 0
	cfg.Workers = 1
	cfg.EnChars = 65
	cfg.ZhChars = 20
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return mapping, audit, srv
}

// retryBundle is one group of two English cues and two Chinese cues. The first
// English cue is long, so a plan that cuts it after one short word puts the
// first screen well under the 800ms in-cue floor: the pre-render check rejects
// that plan on timing, which is exactly what a follow-up request must fix.
func retryBundle() Bundle {
	return fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 9000, OriginalStart: 1000, OriginalEnd: 9000,
				Plain:   "Yes. This is the rest of a long English line about proof.",
				Display: "Yes. This is the rest of a long English line about proof.", Alignment: 2},
			{ID: 2, Start: 9000, End: 11000, OriginalStart: 9000, OriginalEnd: 11000,
				Plain: "Then we go.", Display: "Then we go.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, OriginalStart: 1000, OriginalEnd: 5000,
				Plain: "甲", Display: "甲", Alignment: 2},
			{ID: 2, Start: 5000, End: 11000, OriginalStart: 5000, OriginalEnd: 11000,
				Plain: "乙", Display: "乙", Alignment: 2},
		},
	)
}

// shortScreenPlan cuts the first English cue after one word, so the first
// screen is shorter than 800ms and the pre-render check rejects it.
const shortScreenPlan = `{"plans":[{"group":"en=1,2;zh=1,2","screens":[{"en":"Yes.","zh":"甲"},{"en":"This is the rest of a long English line about proof.","zh":"乙"},{"en":"Then we go.","zh":""}]}]}`

// fixedScreenPlan keeps the first English cue whole: it passes.
const fixedScreenPlan = `{"plans":[{"group":"en=1,2;zh=1,2","screens":[{"en":"Yes. This is the rest of a long English line about proof.","zh":"甲 乙"},{"en":"Then we go.","zh":""}]}]}`

func TestAlignRetriesARejectedPlanAndRecoversIt(t *testing.T) {
	mapping, audit, srv := alignRetry(t, shortScreenPlan, fixedScreenPlan, 0)
	if srv.planCalls != 1 || srv.retryCalls != 1 {
		t.Fatalf("plan calls = %d retry calls = %d, want 1 and 1", srv.planCalls, srv.retryCalls)
	}
	if audit.ScreenPlans == nil || audit.ScreenPlans.Retried != 1 || audit.ScreenPlans.Recovered != 1 {
		t.Fatalf("retry audit = %+v, want one retried and one recovered", audit.ScreenPlans)
	}
	if len(mapping.ScreenPlans) != 1 || len(mapping.ScreenPlans[0].Screens) != 2 {
		t.Fatalf("the recovered plan was not stored: %+v", mapping.ScreenPlans)
	}
	record := audit.ScreenPlans.Retries[0]
	if record.Result != "recovered" || record.Reason == "" || len(record.ENIDs) != 2 || len(record.ZHIDs) != 2 {
		t.Fatalf("retry record = %+v", record)
	}
	// The first request carries no previous screens; the follow-up carries the
	// rejected screens and the exact reason.
	if len(srv.plans) != 1 || strings.Contains(srv.plans[0], "previous_screens") {
		t.Fatalf("the first request carried retry data: %+v", srv.plans)
	}
	if len(srv.retries) != 1 {
		t.Fatalf("retry requests = %d, want 1", len(srv.retries))
	}
	follow := srv.retries[0]
	if !strings.Contains(follow, "previous_screens") || !strings.Contains(follow, "rejection_reason") {
		t.Fatalf("the follow-up did not carry the previous screens and the reason: %s", follow)
	}
	if !strings.Contains(follow, "shorter than 800ms") {
		t.Fatalf("the follow-up did not carry the exact reason: %s", follow)
	}
}

func TestAlignRetryKeepsTheOriginalWhenItFailsAgain(t *testing.T) {
	mapping, audit, srv := alignRetry(t, shortScreenPlan, shortScreenPlan, 0)
	if srv.retryCalls != 1 {
		t.Fatalf("retry calls = %d, want 1", srv.retryCalls)
	}
	if audit.ScreenPlans.Retried != 1 || audit.ScreenPlans.Recovered != 0 {
		t.Fatalf("retry audit = %+v, want one retried and none recovered", audit.ScreenPlans)
	}
	if len(mapping.ScreenPlans) != 1 || len(mapping.ScreenPlans[0].Screens) != 3 {
		t.Fatalf("the original plan was not kept: %+v", mapping.ScreenPlans)
	}
	if got := audit.ScreenPlans.Retries[0].Result; got != "kept" {
		t.Fatalf("retry result = %q, want kept", got)
	}
}

func TestAlignRetryFailureDoesNotFailTheMerge(t *testing.T) {
	mapping, audit, srv := alignRetry(t, shortScreenPlan, "", http.StatusInternalServerError)
	if srv.retryCalls != 1 {
		t.Fatalf("retry calls = %d, want 1", srv.retryCalls)
	}
	if len(mapping.Groups) != 1 {
		t.Fatalf("a failed retry changed the pairing: %+v", mapping.Groups)
	}
	if len(mapping.ScreenPlans) != 1 || len(mapping.ScreenPlans[0].Screens) != 3 {
		t.Fatalf("a failed retry dropped the original plan: %+v", mapping.ScreenPlans)
	}
	if audit.ScreenPlans.Retried != 1 || audit.ScreenPlans.Recovered != 0 {
		t.Fatalf("retry audit = %+v", audit.ScreenPlans)
	}
	if len(audit.ScreenPlans.Failures) == 0 {
		t.Fatal("a failed retry was not recorded")
	}
}

func TestAlignRetryAcceptsAPlanThatPassesTheCheck(t *testing.T) {
	mapping, audit, srv := alignRetry(t, fixedScreenPlan, "", 0)
	if srv.retryCalls != 0 {
		t.Fatalf("a passing plan asked for a retry: %d", srv.retryCalls)
	}
	if audit.ScreenPlans.Retried != 0 || len(mapping.ScreenPlans) != 1 {
		t.Fatalf("retry audit = %+v plans = %+v", audit.ScreenPlans, mapping.ScreenPlans)
	}
}

func TestAlignRetryIsCached(t *testing.T) {
	dir := t.TempDir()
	// One server, so both runs make the same request and share the cache.
	srv := &retryServer{first: shortScreenPlan, retry: fixedScreenPlan}
	server := srv.start(t)
	run := func() (Mapping, Audit) {
		bundle := retryBundle()
		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 0
		cfg.Retries = 0
		cfg.Workers = 1
		cfg.EnChars = 65
		cfg.ZhChars = 20
		cfg.CacheDir = dir
		mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return mapping, audit
	}
	first, _ := run()
	entries, err := os.ReadDir(filepath.Join(dir, cacheNamespace))
	if err != nil {
		t.Fatal(err)
	}
	var plain, followUp int
	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Name(), "plan-retry-"):
			followUp++
		case strings.HasPrefix(entry.Name(), "plan-"):
			plain++
		}
	}
	if plain != 1 || followUp != 1 {
		t.Fatalf("cache files: %d plan, %d follow-up; want 1 each", plain, followUp)
	}
	second, audit := run()
	if len(first.ScreenPlans) != 1 || len(second.ScreenPlans) != 1 {
		t.Fatalf("plans = %+v / %+v", first.ScreenPlans, second.ScreenPlans)
	}
	if audit.ScreenPlans.CacheHits < 2 {
		t.Fatalf("the cached plan and follow-up were not reused: %+v", audit.ScreenPlans)
	}
	if audit.ScreenPlans.Recovered != 1 {
		t.Fatalf("a cached follow-up answer was not validated again: %+v", audit.ScreenPlans)
	}
}

// --- 3. Parenthesized Chinese-only text is on-screen text -----------------

// TestParenthesizedUnknownChineseOnlyMovesToTheTop covers the 35:02 case: a
// Chinese-only sign in full-width parentheses that collides with bottom speech
// moves to the free top, while a dialogue kind or a partly parenthesized line
// stays at the bottom.
func TestParenthesizedUnknownChineseOnlyMovesToTheTop(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    string
		plain   string
		wantTop bool
	}{
		{"parenthesized_unknown", KindUnknown, "（查一下“卡斯特尔”）", true},
		{"dialogue_kind_stays", KindDialogue, "（查一下“卡斯特尔”）", false},
		{"missing_kind_stays", "", "（查一下“卡斯特尔”）", false},
		{"partial_parentheses_stay", KindUnknown, "（查一下“卡斯特尔”）还没找到", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noWidthMeasurer(t)
			bundle := fixtureBundle(
				[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "Check the roster.", Plain: "Check the roster.", Alignment: 2}},
				[]Cue{
					{ID: 1, Start: 1000, End: 4000, Display: "查一下卡斯特尔", Plain: "查一下卡斯特尔", Alignment: 2},
					{ID: 2, Start: 1500, End: 3500, Display: tc.plain, Plain: tc.plain, Alignment: 2},
				},
			)
			mapping := fixtureMapping(bundle, []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: tc.kind},
			})
			out, _, err := renderWithAudits(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range parseDialogues(t, string(out)) {
				if !strings.Contains(e.text, "卡斯特尔") || strings.Contains(e.text, "查一下卡斯特尔") {
					continue
				}
				found = true
				if got := strings.HasPrefix(e.style, "Top"); got != tc.wantTop {
					t.Fatalf("sign style = %q, want top=%v", e.style, tc.wantTop)
				}
			}
			if !found {
				t.Fatalf("the sign text was not rendered")
			}
		})
	}
}

// --- 4. Width measurement uses the playback aspect -------------------------

func TestWidthFactorFollowsThePlaybackAspect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		frameW     int
		frameH     int
		wantFactor float64
	}{
		// 3840x1632 is wider than 16:9, so script x units span more pixels.
		{"scope_3840x1632", 3840, 1632, (1632.0 / 1080) / (3840.0 / 1920)},
		{"16x9_1920x1080", 1920, 1080, 1},
		{"4x3_1440x1080", 1440, 1080, (1080.0 / 1080) / (1440.0 / 1920)},
		{"unknown_size", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := renderGeometry{canvasWidth: 1920, canvasHeight: 1080, frameWidth: tc.frameW, frameHeight: tc.frameH}
			got := g.widthFactor()
			if diff := got - tc.wantFactor; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("widthFactor = %v, want %v", got, tc.wantFactor)
			}
		})
	}
	if f := (renderGeometry{canvasWidth: 1920, canvasHeight: 1080, frameWidth: 3840, frameHeight: 1632}).widthFactor(); f < 0.75 || f > 0.76 {
		t.Fatalf("scope factor = %v, want about 0.756", f)
	}
	if f := (renderGeometry{canvasWidth: 1920, canvasHeight: 1080, frameWidth: 1440, frameHeight: 1080}).widthFactor(); f < 1.33 || f > 1.34 {
		t.Fatalf("4:3 factor = %v, want about 1.333", f)
	}
}

// TestPlanWidthCheckHasNoExemption covers the superseded width path: a plan
// Chinese piece over zh_chars is rejected however narrow it measures. The user
// chose to cut both languages rather than show one long line because a
// measurement says it fits, so the pre-render check and the render check agree.
func TestPlanWidthCheckHasNoExemption(t *testing.T) {
	calls := 0
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		calls++
		return 1, true // narrow enough for any screen
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = old })
	// A two-line source, so the single-line exception does not apply either.
	cue := Cue{ID: 1, Start: 1000, End: 6000, Plain: "很长的一块\n中文字幕", Display: "很长的一块中文字幕", Alignment: 2}
	index := map[int]Cue{1: cue}
	lang, err := newPlanLanguage(index, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	spans, err := lang.matchPieces([]string{"很长的一块中文字幕"})
	if err != nil {
		t.Fatal(err)
	}
	limits := planLimits()
	limits.zhChars = 5
	checkErr := checkPlanChinese("很长的一块中文字幕", lang, spans[0], lang.touched(spans[0]), limits)
	if checkErr == nil {
		t.Fatal("a Chinese piece over the limit passed on a width measurement")
	}
	if !strings.Contains(checkErr.Error(), "characters, limit 5;") ||
		!strings.Contains(checkErr.Error(), "cut it at a space, punctuation or cue boundary") {
		t.Fatalf("rejection = %q, want the character-count reason and the fix", checkErr)
	}
	if calls != 0 {
		t.Fatalf("the width was measured %d times, want no measurement at all", calls)
	}
	// At the limit it passes.
	limits.zhChars = countedLengthFor("很长的一块中文字幕", true)
	if err := checkPlanChinese("很长的一块中文字幕", lang, spans[0], lang.touched(spans[0]), limits); err != nil {
		t.Fatalf("a Chinese piece at the limit was rejected: %v", err)
	}
}

// TestPlanChineseWholeSingleLineCueKeepsTheSourceException covers the Chinese
// mirror of the English exception: a piece that is exactly one whole Chinese cue
// whose source is a single line may exceed zh_chars, because it has no allowed
// cut and the deterministic renderer shows that line whole anyway. A cut piece
// and a two-line source still count.
func TestPlanChineseWholeSingleLineCueKeepsTheSourceException(t *testing.T) {
	index := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 6000, Plain: "差点忘了我们在这里期间你叫詹姆斯多诺万",
			Display: "差点忘了我们在这里期间你叫詹姆斯多诺万", Alignment: 2},
		2: {ID: 2, Start: 6000, End: 9000, Plain: "差点忘了\n我们在这里期间", Display: "差点忘了 我们在这里期间", Alignment: 2},
	}
	limits := planLimits()
	limits.zhChars = 5
	// Exactly one whole cue, single-line source: accepted past the limit.
	one, err := newPlanLanguage(index, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	spans, err := one.matchPieces([]string{"差点忘了我们在这里期间你叫詹姆斯多诺万"})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPlanChinese("差点忘了我们在这里期间你叫詹姆斯多诺万", one, spans[0], one.touched(spans[0]), limits); err != nil {
		t.Fatalf("a whole single-line Chinese cue lost its source exception: %v", err)
	}
	// A cut piece of the same cue is not whole, so the limit applies.
	cut, err := newPlanLanguage(index, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	cutSpans, err := cut.matchPieces([]string{"差点忘了我们在这里期间", "你叫詹姆斯多诺万"})
	if err != nil {
		t.Fatal(err)
	}
	message := checkPlanChinese("差点忘了我们在这里期间", cut, cutSpans[0], cut.touched(cutSpans[0]), limits)
	if message == nil || !strings.Contains(message.Error(), "characters, limit 5") {
		t.Fatalf("a cut piece past the limit passed: %v", message)
	}
	// A two-line source is not a single line, so the limit applies even whole.
	two, err := newPlanLanguage(index, []int{2}, true)
	if err != nil {
		t.Fatal(err)
	}
	twoSpans, err := two.matchPieces([]string{"差点忘了 我们在这里期间"})
	if err != nil {
		t.Fatal(err)
	}
	message = checkPlanChinese("差点忘了 我们在这里期间", two, twoSpans[0], two.touched(twoSpans[0]), limits)
	if message == nil || !strings.Contains(message.Error(), "characters, limit 5") {
		t.Fatalf("a two-line source past the limit passed: %v", message)
	}
}

// TestPlanChineseLimitMatchesThePreRenderAndRenderCheck proves the two checks
// agree: a plan the alignment accepted is never rejected for Chinese width at
// render time, because both run the same character-count rule.
func TestPlanChineseLimitMatchesThePreRenderAndRenderCheck(t *testing.T) {
	// One whole Chinese cue over zh_chars with no cut point: the plan passes the
	// pre-render check only when it is within the limit, so it must be rejected
	// both there and at render, never accepted by one and refused by the other.
	en, _ := planCues()
	zhLong := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲\n和他姐妹的死", Display: "我从没跟莫谈过他父亲和他姐妹的死", Alignment: 2}}
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲和他姐妹的死"},
	}}
	align := planGeometry{enChars: 65, zhChars: 5}
	render := planGeometry{enChars: 65, zhChars: 5}
	_, alignErr := resolveScreenPlan(plan, en, zhLong, nil, align)
	_, renderErr := resolveScreenPlan(plan, en, zhLong, nil, render)
	if alignErr == nil || renderErr == nil {
		t.Fatalf("a plan over the Chinese limit passed a check: align=%v render=%v", alignErr, renderErr)
	}
	if alignErr.Error() != renderErr.Error() {
		t.Fatalf("the two checks disagree: %q vs %q", alignErr, renderErr)
	}
	limits := planLimits()
	limits.zhChars = 20
	if _, err := resolveScreenPlan(plan, en, zhLong, nil, limits); err != nil {
		t.Fatalf("a plan within the Chinese limit was rejected: %v", err)
	}
}

// TestDialogueMergeUsesTheAspectFactor covers the dialogue merge: a merged line
// passes the character cap only when the converted width fits.
func TestDialogueMergeUsesTheAspectFactor(t *testing.T) {
	// 1900px at 3840x1632 becomes about 1436px, under the 1760px usable width.
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		return 1900, true
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = old })
	bundle := dialogueMergeBundle()
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	merged := func(width, height int) bool {
		_, events, err := renderWithAudits(bundle, mapping, fixtureConfig(), fixtureStyle(), width, height, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if strings.Contains(e.zhText, "抱歉") && strings.Contains(e.zhText, "证据") &&
				len(sourceLines(e.zhText)) == 1 {
				return true
			}
		}
		return false
	}
	if !merged(3840, 1632) {
		t.Fatal("a dialogue line that fits after the scope conversion was not merged")
	}
	if merged(1920, 1080) {
		t.Fatal("a dialogue line wider than the screen was merged")
	}
	if merged(0, 0) {
		t.Fatal("a wide dialogue line was merged without a frame size")
	}
}

// --- 5. The Chinese count excludes spaces --------------------------------

func TestChineseCountExcludesSpaces(t *testing.T) {
	for _, tc := range []struct {
		text string
		en   int
		zh   int
	}{
		// 12:40: 19 counted characters plus two spaces.
		{"差点忘了 我们在这里期间 你叫“詹姆斯多诺万”", 21, 19},
		{"甲乙 丙丁", 5, 4},
		{"Hello, world.", 11, 10},
		{"", 0, 0},
	} {
		if got := countedLength(tc.text); got != tc.en {
			t.Errorf("countedLength(%q) = %d, want %d", tc.text, got, tc.en)
		}
		if got := countedLengthFor(tc.text, true); got != tc.zh {
			t.Errorf("countedLengthFor(%q, zh) = %d, want %d", tc.text, got, tc.zh)
		}
	}
}

// TestChineseSpaceIsFreeAtEveryDecisionPoint walks the decision points that
// compare against zh_chars: joining, the wrap limit, split options, the plan
// length check and the dialogue merge cap.
func TestChineseSpaceIsFreeAtEveryDecisionPoint(t *testing.T) {
	// Joining: 10 counted characters plus one space, cap 10.
	cues := []Cue{
		{ID: 1, Start: 1000, End: 2000, Display: "一二三四五", Plain: "一二三四五", Alignment: 2},
		{ID: 2, Start: 2000, End: 3000, Display: "六七八九十", Plain: "六七八九十", Alignment: 2},
	}
	if !joinable(cues, 10, 500, 0, true) {
		t.Error("two Chinese cues whose joined text counts 10 were not joinable at cap 10")
	}
	// English still counts the joining space, so the same text needs cap 11.
	if joinable(cues, 10, 500, 5500, false) {
		t.Error("the English count stopped counting the joining space")
	}
	if !joinable(cues, 11, 500, 5500, false) {
		t.Error("the English join at cap 11 failed")
	}
	// The wrap limit: 10 counted characters plus a space stays on one line.
	if got := wrapDisplay("一二三四五 六七八九十", "zh", 10, false); got != "一二三四五 六七八九十" {
		t.Errorf("a Chinese line whose counted length is 10 was wrapped: %q", got)
	}
	// Split options: the same text needs no option at cap 10.
	if options := textOptions("一二三四五 六七八九十", "", 10, "zh"); len(options) != 0 {
		t.Errorf("a Chinese text within the cap offered %v", options)
	}
	// The plan length check.
	limits := planLimits()
	limits.zhChars = 10
	if err := checkPlanChinese("一二三四五 六七八九十", nil, planSpan{}, nil, limits); err != nil {
		t.Errorf("a Chinese piece within the cap was rejected: %v", err)
	}
	// The one-sided text choice.
	if got := oneSidedText("一二三四五\n六七八九十", "一二三四五 六七八九十", 10, true); got != "一二三四五 六七八九十" {
		t.Errorf("a one-sided Chinese cue within the cap was not one line: %q", got)
	}
	// English keeps counting spaces.
	if err := checkPlanEnglish("a b c d e f", nil, planSpan{}, nil, planGeometry{enChars: 5}); err == nil {
		t.Error("the English count stopped counting spaces")
	}
}

// TestChineseCountingKeepsSplitOptionsWorking proves the counting change did not
// make the split decisions unreachable: a text that really is over the cap
// still splits, and each part is within the cap.
func TestChineseCountingKeepsSplitOptionsWorking(t *testing.T) {
	text := "一二三四五六七八九十 甲乙丙丁戊己庚辛壬癸"
	options := textOptions(text, "", 10, "zh")
	if len(options) == 0 {
		t.Fatalf("an over-cap Chinese text offered no option")
	}
	for _, parts := range options {
		if !partsFit(parts, 10, true) {
			t.Fatalf("an option exceeds the cap: %v", parts)
		}
	}
}

var _ = config.LLMConfig{}

// --- Join rule: English cues stay apart when the Chinese can be cut ---------

// TestScreenPlanJoinRuleRejectsCuttableChinese covers the lead's ruling: joining
// two English cues shows the later one early, so it is allowed only when the
// Chinese piece has no space, punctuation or cue boundary to cut at.
func TestScreenPlanJoinRuleRejectsCuttableChinese(t *testing.T) {
	en, zh := planCues()
	// The Chinese has a space between the two cues' text, so it can be cut.
	withSpace := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲 和他姐妹的死", Display: "我从没跟莫谈过他父亲 和他姐妹的死", Alignment: 2}}
	join := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲 和他姐妹的死"},
	}}
	message := planError(t, en, withSpace, join, planLimits())
	if !strings.Contains(message, "joins English cues 1 and 2, but its Chinese can be cut") {
		t.Fatalf("a cuttable Chinese join passed: %q", message)
	}
	// One unbreakable phrase has no cut point, so the join is the only way to
	// keep one line per language and it passes.
	unbreakable := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲和他姐妹的死", Display: "我从没跟莫谈过他父亲和他姐妹的死", Alignment: 2}}
	unbreakableJoin := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲和他姐妹的死"},
	}}
	if _, err := resolveScreenPlan(unbreakableJoin, en, unbreakable, nil, planLimits()); err != nil {
		t.Fatalf("a join of unbreakable Chinese was rejected: %v", err)
	}
	// Punctuation is a cut point too: a Chinese piece with a comma can be cut.
	withPunct := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲，和他姐妹的死", Display: "我从没跟莫谈过他父亲，和他姐妹的死", Alignment: 2}}
	punctJoin := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲，和他姐妹的死"},
	}}
	if message := planError(t, en, withPunct, punctJoin, planLimits()); !strings.Contains(message, "joins English cues") {
		t.Fatalf("a Chinese piece with punctuation was not treated as cuttable: %q", message)
	}
	_ = zh
}

// TestScreenPlanJoinRuleFixesThe1840Group covers the real 18:40 shape: EN 286
// and 287 joined under one Chinese cue that has a space. The plan is rejected,
// and the corrected answer of two screens is accepted.
func TestScreenPlanJoinRuleFixesThe1840Group(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1120870, End: 1122290, OriginalStart: 1120870, OriginalEnd: 1122290,
				Plain: "Mo was never religious.", Display: "Mo was never religious.", Alignment: 2},
			{ID: 2, Start: 1122290, End: 1124000, OriginalStart: 1122290, OriginalEnd: 1124000,
				Plain: "His father was an academic.", Display: "His father was an academic.", Alignment: 2},
		},
		[]Cue{{ID: 1, Start: 1120870, End: 1124000, OriginalStart: 1120870, OriginalEnd: 1124000,
			Plain: "莫从来都不是虔诚的信徒 他父亲是个学者", Display: "莫从来都不是虔诚的信徒 他父亲是个学者", Alignment: 2}},
	)
	en, zh, err := cueIndex(bundle)
	if err != nil {
		t.Fatal(err)
	}
	joined := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Mo was never religious. His father was an academic.", ZH: "莫从来都不是虔诚的信徒 他父亲是个学者"},
	}}
	message := planError(t, en, zh, joined, planLimits())
	if !strings.Contains(message, "joins English cues 1 and 2") {
		t.Fatalf("the 18:40 join passed: %q", message)
	}
	// The corrected answer: two screens, the Chinese cut at its space, so the
	// second English cue starts at its own time.
	fixed := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Mo was never religious.", ZH: "莫从来都不是虔诚的信徒"},
		{EN: "His father was an academic.", ZH: "他父亲是个学者"},
	}}
	screens, err := resolveScreenPlan(fixed, en, zh, nil, planLimits())
	if err != nil {
		t.Fatalf("the corrected two-screen answer was rejected: %v", err)
	}
	if len(screens) != 2 {
		t.Fatalf("screens = %d, want 2", len(screens))
	}
	if screens[1].start != 1122290 {
		t.Fatalf("the second screen starts at %d, want the second cue start 1122290", screens[1].start)
	}
	if screens[0].end != 1122290 {
		t.Fatalf("the first screen ends at %d, want 1122290", screens[0].end)
	}
}

// TestAlignRecoversAJoinWithTheJoinRule covers the retry and the rule together:
// the first answer joins the English cues, the follow-up answer splits them, and
// the recovered plan is what the renderer applies.
func TestAlignRecoversAJoinWithTheJoinRule(t *testing.T) {
	const joined = `{"plans":[{"group":"en=1,2;zh=1,2","screens":[{"en":"Yes. This is the rest of a long English line about proof. Then we go.","zh":"甲 乙"}]}]}`
	const split = `{"plans":[{"group":"en=1,2;zh=1,2","screens":[{"en":"Yes. This is the rest of a long English line about proof.","zh":"甲"},{"en":"Then we go.","zh":"乙"}]}]}`
	mapping, audit, srv := alignRetry(t, joined, split, 0)
	if srv.retryCalls != 1 {
		t.Fatalf("retry calls = %d, want 1", srv.retryCalls)
	}
	if audit.ScreenPlans.Retried != 1 || audit.ScreenPlans.Recovered != 1 {
		t.Fatalf("retry audit = %+v, want one recovered", audit.ScreenPlans)
	}
	if len(mapping.ScreenPlans) != 1 || len(mapping.ScreenPlans[0].Screens) != 2 {
		t.Fatalf("the split plan was not stored: %+v", mapping.ScreenPlans)
	}
	// The follow-up carried the join reason, so the model knows what to fix.
	if len(srv.retries) != 1 {
		t.Fatalf("retry requests = %d", len(srv.retries))
	}
	if !strings.Contains(srv.retries[0], "joins English cues") {
		t.Fatalf("the follow-up carried the wrong reason: %s", srv.retries[0])
	}
}

// --- V1: an English piece may not reach across a cue boundary -------------

// TestScreenPlanRejectsEnglishCrossingACueBoundary covers V1: a piece that holds
// part of one English cue with another cue shows the next cue's text early, so it
// is rejected. The same text passes when each cue is held whole.
func TestScreenPlanRejectsEnglishCrossingACueBoundary(t *testing.T) {
	// The 05:41 shape: ZH 85 is one unbreakable unit, EN 97 is whole, and the
	// first piece reaches into EN 98 with "watching the".
	en := map[int]Cue{
		1: {ID: 1, Start: 345471, End: 347805, Plain: "and you spend the rest of your days in a cell",
			Display: "and you spend the rest of your days in a cell", Alignment: 2},
		2: {ID: 2, Start: 347806, End: 349932, Plain: "watching the Justice Department destroy the lives",
			Display: "watching the Justice Department destroy the lives", Alignment: 2},
		3: {ID: 3, Start: 349933, End: 351477, Plain: "of everyone who stood in your corner.",
			Display: "of everyone who stood in your corner.", Alignment: 2},
	}
	zh := map[int]Cue{
		1: {ID: 1, Start: 345471, End: 348848, Plain: "你下半辈子都在监狱里看着司法部", Display: "你下半辈子都在监狱里看着司法部", Alignment: 2},
		2: {ID: 2, Start: 348849, End: 351852, Plain: "摧毁所有曾经站在你那边的人的生活", Display: "摧毁所有曾经站在你那边的人的生活", Alignment: 2},
	}
	crossing := ScreenPlan{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "and you spend the rest of your days in a cell watching the", ZH: "你下半辈子都在监狱里看着司法部"},
		{EN: "Justice Department destroy the lives", ZH: ""},
		{EN: "of everyone who stood in your corner.", ZH: "摧毁所有曾经站在你那边的人的生活"},
	}}
	message := planError(t, en, zh, crossing, planLimits())
	if !strings.Contains(message, "crosses a cue boundary with a partial cue") {
		t.Fatalf("a piece that reached into the next cue passed: %q", message)
	}
	// Both sides hold whole cues: the plan passes.
	fixed := ScreenPlan{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "and you spend the rest of your days in a cell", ZH: "你下半辈子都在监狱里看着司法部"},
		{EN: "watching the Justice Department destroy the lives of everyone who stood in your corner.", ZH: ""},
		{EN: "", ZH: "摧毁所有曾经站在你那边的人的生活"},
	}}
	if message := planError(t, en, zh, fixed, planLimits()); strings.Contains(message, "crosses a cue boundary") {
		t.Fatalf("a whole-cue plan was rejected as crossing: %q", message)
	}
	// A cut inside ONE cue with another cue is the 42:00 shape: part of cue 575
	// plus all of cue 576.
	if message := planError(t, en, zh, ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1},
		Screens: []ScreenText{
			{EN: "and you spend the rest of your days in a cell watching the", ZH: "你下半辈子都在监狱里看着司法部"},
			{EN: "Justice Department destroy the lives", ZH: ""},
		}}, planLimits()); !strings.Contains(message, "crosses a cue boundary") {
		t.Fatalf("a partial cue joined with the next cue passed: %q", message)
	}
}

// --- V2: an English-only screen may hold whole cues only ------------------

// TestScreenPlanRejectsPartialEnglishOnlyScreen covers V2: a screen with no
// Chinese may not be produced by an in-cue cut, because the rest of that cue is
// shown early and then repeated or dropped.
func TestScreenPlanRejectsPartialEnglishOnlyScreen(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000,
		Plain: "Open the door now please.", Display: "Open the door now please.", Alignment: 2}}
	zh := map[int]Cue{1: {ID: 1, Start: 1000, End: 3000, Plain: "开门", Display: "开门", Alignment: 2}}
	partial := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "开门"},
		{EN: "now please.", ZH: ""},
	}}
	message := planError(t, en, zh, partial, planLimits())
	if !strings.Contains(message, "has no chinese and does not hold english cue 1 whole") {
		t.Fatalf("a partial English-only screen passed: %q", message)
	}
	// With Chinese on both screens the in-cue cut is the normal case and passes.
	bothWithChinese := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 3000, Plain: "开门", Display: "开门", Alignment: 2},
		2: {ID: 2, Start: 3000, End: 5000, Plain: "请现在", Display: "请现在", Alignment: 2},
	}
	paired := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "开门"},
		{EN: "now please.", ZH: "请现在"},
	}}
	if _, err := resolveScreenPlan(paired, en, bothWithChinese, nil, planLimits()); err != nil {
		t.Fatalf("an in-cue cut with Chinese on both screens was rejected: %v", err)
	}
	// An English-only screen made of a whole cue is allowed: it is text with no
	// Chinese counterpart.
	enOnly := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Open the door now please.", ZH: "开门"},
		{EN: "Then close it.", ZH: ""},
	}}
	withSecond := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000, Plain: "Open the door now please.", Display: "Open the door now please.", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 7000, Plain: "Then close it.", Display: "Then close it.", Alignment: 2},
	}
	if _, err := resolveScreenPlan(enOnly, withSecond, zh, nil, planLimits()); err != nil {
		t.Fatalf("an English-only screen of a whole cue was rejected: %v", err)
	}
}

// --- Reviewer P1: marks come from the source line structure ---------------

// TestSpeakerMarksComeFromSourceLineStarts covers P1: an unmarked line must not
// let a later search match repeated text, so an inline dash is never mistaken
// for a line-start mark.
func TestSpeakerMarksComeFromSourceLineStarts(t *testing.T) {
	// "Alpha - beta" holds an inline dash; the second line "- beta" repeats the
	// rest of the first line. Only rune 13 starts a line with a mark.
	cue := Cue{ID: 1, Start: 1000, End: 10000, Plain: "Alpha - beta\n- beta", Display: "Alpha - beta - beta", Alignment: 2}
	lang, err := newPlanLanguage(map[int]Cue{1: cue}, []int{1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(lang.marks) != 1 || lang.marks[0] != 13 {
		t.Fatalf("line marks = %v, want [13] (the dash that starts the second line)", lang.marks)
	}
	// A piece that omits the INLINE dash is rejected: that dash is text.
	zh := map[int]Cue{1: {ID: 1, Start: 1000, End: 10000, Plain: "甲 乙 丙", Display: "甲 乙 丙", Alignment: 2}}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha", ZH: "甲"},
		{EN: "beta", ZH: "乙"},
		{EN: "- beta", ZH: "丙"},
	}}
	message := planError(t, map[int]Cue{1: cue}, zh, plan, planLimits())
	if !strings.Contains(message, "does not continue") {
		t.Fatalf("an omitted inline dash was accepted: %q", message)
	}
	// Omitting the real line-start mark is still tolerated: the piece takes both
	// lines' text without their marks, and the source slice restores them.
	zhWhole := map[int]Cue{1: {ID: 1, Start: 1000, End: 10000, Plain: "甲乙丙", Display: "甲乙丙", Alignment: 2}}
	ok := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha - beta beta", ZH: "甲乙丙"},
	}}
	screens, err := resolveScreenPlan(ok, map[int]Cue{1: cue}, zhWhole, nil, planLimits())
	if err != nil {
		t.Fatalf("an omitted line-start mark was rejected: %v", err)
	}
	if screens[0].en != "Alpha - beta - beta" {
		t.Fatalf("the source slice did not restore the line-start mark: %q", screens[0].en)
	}
}

// --- Reviewer P3: omitted marks are tolerated inside a piece --------------

// TestSpeakerMarkMayBeOmittedInsideAPiece covers P3: a piece that spans two
// marked source lines and drops both marks is accepted, and the rendered text
// restores them from the source.
func TestSpeakerMarkMayBeOmittedInsideAPiece(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000, Plain: "Alpha beta.", Display: "Alpha beta.", Alignment: 2}}
	zh := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000, Plain: "- 甲\n- 乙", Display: "- 甲 - 乙",
		Alignment: 2, MultiSpeaker: true}}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha beta.", ZH: "甲 乙"},
	}}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatalf("marks omitted inside a piece were rejected: %v", err)
	}
	// The rendered text is the source slice; the dash rule then decides.
	if screens[0].zh != "- 甲 - 乙" {
		t.Fatalf("the source marks were not restored: %q", screens[0].zh)
	}
	// A piece may also drop only the inner mark.
	inner := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha beta.", ZH: "- 甲 乙"},
	}}
	if got, err := resolveScreenPlan(inner, en, zh, nil, planLimits()); err != nil {
		t.Fatalf("the inner omitted mark was rejected: %v", err)
	} else if got[0].zh != "- 甲 - 乙" {
		t.Fatalf("the inner mark was not restored: %q", got[0].zh)
	}
	// Non-mark text inside a piece may still not be dropped.
	dropped := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Alpha beta.", ZH: "甲"},
	}}
	if message := planError(t, en, zh, dropped, planLimits()); message == "" {
		t.Fatalf("dropped text inside a piece was accepted: %q", message)
	}
}

// --- Reviewer P2: the prompt must allow the cuts the resolver accepts -----

// TestScreenPlanPromptAllowsInCueCuts covers P2: the planner is told to keep
// English whole only when a screen reaches across a cue boundary. A cut inside
// one English cue, with Chinese on both screens, is the case the resolver
// accepts and the Chinese cap often requires.
func TestScreenPlanPromptAllowsInCueCuts(t *testing.T) {
	for _, forbidden := range []string{
		"Never cut one English cue into pieces",
		"An empty Chinese side is always allowed",
	} {
		if strings.Contains(screenPlanPrompt, forbidden) {
			t.Fatalf("the planner prompt still says %q", forbidden)
		}
	}
	if !strings.Contains(screenPlanPrompt, "You may cut one English cue between words") {
		t.Fatal("the planner prompt does not tell the model that an in-cue cut is allowed")
	}
	// The permission must be conditional, or the model cuts cues that leave the
	// Chinese unchanged and freezes it, or cuts a cue too short to split.
	for _, required := range []string{
		"every resulting screen has Chinese text",
		"each with its own Chinese piece",
		"at least 800ms",
	} {
		if !strings.Contains(screenPlanPrompt, required) {
			t.Fatalf("the in-cue permission is missing %q", required)
		}
	}
	// The candidate that needs exactly that cut resolves once the prompt allows it.
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: "Open the door now please.",
		Display: "Open the door now please.", Alignment: 2}}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000, Plain: "一二三四五六七八九十", Display: "一二三四五六七八九十", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 9000, Plain: "甲乙丙丁戊己庚辛壬癸", Display: "甲乙丙丁戊己庚辛壬癸", Alignment: 2},
	}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "一二三四五六七八九十"},
		{EN: "now please.", ZH: "甲乙丙丁戊己庚辛壬癸"},
	}}
	if _, err := resolveScreenPlan(plan, en, zh, nil, planGeometry{enChars: 65, zhChars: 10}); err != nil {
		t.Fatalf("the in-cue solution its resolver accepts was rejected: %v", err)
	}
}

// --- Reviewer P4: the whole text must be one parenthetical ----------------

// TestParenthesizedTextRequiresOneEnclosingPair covers P4: a cue whose main text
// sits outside parentheses is not a sign.
func TestParenthesizedTextRequiresOneEnclosingPair(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"（查一下“卡斯特尔”）", true},
		{"（低声）快走（关门）", false},
		{"（甲（乙）丙）", true},
		{"（低声）快走", false},
		{"快走（关门）", false},
		{"（）", true},
		{"（", false},
	} {
		if got := parenthesizedText(tc.text); got != tc.want {
			t.Errorf("parenthesizedText(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
	// The sign moves to the top; the partly parenthesized line stays at the bottom.
	noWidthMeasurer(t)
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Plain: "Leave.", Display: "Leave.", Alignment: 2}},
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Plain: "走吧", Display: "走吧", Alignment: 2},
			{ID: 2, Start: 1500, End: 4000, Plain: "（低声）快走（关门）", Display: "（低声）快走（关门）", Alignment: 2},
			{ID: 3, Start: 1600, End: 3900, Plain: "（甲（乙）丙）", Display: "（甲（乙）丙）", Alignment: 2},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindUnknown},
		{ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindUnknown},
	})
	out, _, err := renderWithAudits(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sawSpoken, sawNested bool
	for _, e := range parseDialogues(t, string(out)) {
		switch {
		case strings.Contains(e.text, "快走"):
			sawSpoken = true
			if strings.HasPrefix(e.style, "Top") {
				t.Fatalf("partly parenthesized spoken text moved to the top: %q", e.style)
			}
		case strings.Contains(e.text, "甲（乙）丙"):
			sawNested = true
			if !strings.HasPrefix(e.style, "Top") {
				t.Fatalf("a nested one-pair sign stayed at the bottom: %q", e.style)
			}
		}
	}
	if !sawSpoken || !sawNested {
		t.Fatalf("the fixtures were not rendered: spoken=%v nested=%v", sawSpoken, sawNested)
	}
}

// --- Reviewer N1: a numeric minus is not a speaker mark -------------------

// TestNumericMinusIsNotASpeakerMark covers N1: the loader, the matcher and the
// display rule share one definition of a mark, so "-50" keeps its minus and a
// piece may never drop it.
func TestNumericMinusIsNotASpeakerMark(t *testing.T) {
	// The loader keeps a negative number unchanged.
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"-50 is the change", "-50 is the change"},
		{"—5 minutes left", "—5 minutes left"},
		{"-- dash pair", "-- dash pair"},
		{"- speaker line", "- speaker line"},
		{"-speaker line", "- speaker line"},
	} {
		if got := cleanLine(tc.in); got != tc.want {
			t.Errorf("cleanLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// No mark is recorded for a cue that only holds a negative number.
	cue := Cue{ID: 1, Start: 1000, End: 6000, Plain: "-50 is the change", Display: "-50 is the change", Alignment: 2}
	lang, err := newPlanLanguage(map[int]Cue{1: cue}, []int{1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(lang.marks) != 0 {
		t.Fatalf("a numeric minus was recorded as a mark: %v", lang.marks)
	}
	// A piece may not skip the "-5", so dropping it is a text change.
	if _, err := lang.matchPieces([]string{"0 is the change"}); err == nil ||
		!strings.Contains(err.Error(), "does not continue") {
		t.Fatalf("a skipped numeric minus was accepted: %v", err)
	}
	// A valid in-cue cut keeps the minus in the first piece.
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: cleanLine("-50 degrees then we leave"),
		Display: cleanLine("-50 degrees then we leave"), Alignment: 2}}
	zh := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: "零下五十度 然后我们离开",
		Display: "零下五十度 然后我们离开", Alignment: 2}}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "-50 degrees", ZH: "零下五十度"},
		{EN: "then we leave", ZH: "然后我们离开"},
	}}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if screens[0].en != "-50 degrees" {
		t.Fatalf("the numeric minus was dropped from the piece: %q", screens[0].en)
	}
	// A real mark is still a mark, and dropping it is still tolerated.
	marked := Cue{ID: 1, Start: 1000, End: 6000, Plain: "- 甲\n- 乙", Display: "- 甲 - 乙", Alignment: 2}
	markedLang, err := newPlanLanguage(map[int]Cue{1: marked}, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(markedLang.marks) != 2 {
		t.Fatalf("real speaker marks were lost: %v", markedLang.marks)
	}
}

// --- Reviewer N2 / N3: the prompt must match the validator ----------------

// TestScreenPlanPromptStatesTheValidatorConditions covers N2 and N3 as source
// rules: the in-cue permission quotes the validator's own conditions instead of
// a cut-point heuristic, and the length rule states the single-line exception.
func TestScreenPlanPromptStatesTheValidatorConditions(t *testing.T) {
	// N2: the heuristic is gone and the real conditions are stated.
	if strings.Contains(screenPlanPrompt, "more cut points than the English has cues") {
		t.Fatal("the prompt still uses the cut-point heuristic that excludes one English cue with one Chinese cut point")
	}
	for _, required := range []string{
		"every resulting screen has Chinese text",
		"each screen next to the cut lasts at least 800ms",
		"each with its own Chinese piece",
	} {
		if !strings.Contains(screenPlanPrompt, required) {
			t.Fatalf("the in-cue permission is missing the validator condition %q", required)
		}
	}
	// Both empty-side rules are stated, since the validator enforces both.
	for _, required := range []string{
		"a screen with no English may hold whole Chinese cues only",
		"A screen with no Chinese may hold whole English cues only",
	} {
		if !strings.Contains(screenPlanPrompt, required) {
			t.Fatalf("the prompt omits the empty-side rule %q", required)
		}
	}
	// N3: the single-line source exception is stated for both languages.
	if !strings.Contains(screenPlanPrompt, "may exceed its limit") {
		t.Fatal("the prompt omits the whole-single-line length exception")
	}
	if !strings.Contains(screenPlanPrompt, "single line in the source") {
		t.Fatal("the prompt does not say the exception needs a single source line")
	}
	// A prompt must never forbid what the resolver accepts: the whole-single-line
	// join is accepted, and so is the one-English/two-Chinese in-cue cut.
	english := []Cue{
		{ID: 1, Start: 1000, End: 5000, Plain: "First English cue", Display: "First English cue", Alignment: 2},
		{ID: 2, Start: 5000, End: 9000, Plain: "Second English cue", Display: "Second English cue", Alignment: 2},
	}
	chinese := []Cue{{ID: 1, Start: 1000, End: 9000,
		Plain:   "甲乙丙丁戊己庚辛壬癸一二三四五六七八九十甲乙",
		Display: "甲乙丙丁戊己庚辛壬癸一二三四五六七八九十甲乙", Alignment: 2}}
	bundle := fixtureBundle(english, chinese)
	en, zh, err := cueIndex(bundle)
	if err != nil {
		t.Fatal(err)
	}
	join := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "First English cue Second English cue", ZH: "甲乙丙丁戊己庚辛壬癸一二三四五六七八九十甲乙"},
	}}
	if _, err := resolveScreenPlan(join, en, zh, nil, planGeometry{enChars: 65, zhChars: 20}); err != nil {
		t.Fatalf("the whole-single-line join the prompt describes was rejected: %v", err)
	}
	// One English cue, two unbreakable Chinese cues, one allowed Chinese cut.
	oneEN := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: "Open the door now please.",
		Display: "Open the door now please.", Alignment: 2}}
	twoZH := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000, Plain: "一二三四五六七八九十", Display: "一二三四五六七八九十", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 9000, Plain: "甲乙丙丁戊己庚辛壬癸", Display: "甲乙丙丁戊己庚辛壬癸", Alignment: 2},
	}
	inCue := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "一二三四五六七八九十"},
		{EN: "now please.", ZH: "甲乙丙丁戊己庚辛壬癸"},
	}}
	if _, err := resolveScreenPlan(inCue, oneEN, twoZH, nil, planGeometry{enChars: 65, zhChars: 10}); err != nil {
		t.Fatalf("the one-English/two-Chinese in-cue cut was rejected: %v", err)
	}
	// The retry prompt carries the exception too, so a length rejection and its
	// fix never contradict.
	if !strings.Contains(screenPlanRetryPrompt, "single-line source cue") {
		t.Fatal("the retry prompt omits the single-line exception")
	}
}

// --- Round 6: the length reason says why the exception does not apply ------

// TestLengthRejectionExplainsWhyTheExceptionDoesNotApply covers the round-6
// wording: a Chinese or English piece over its limit names the cue and the
// reason the whole-single-line exception is unavailable, so a retry knows what
// to change. The rule itself is unchanged.
func TestLengthRejectionExplainsWhyTheExceptionDoesNotApply(t *testing.T) {
	// Cue 1 is two source lines, so its whole piece gets no exception.
	twoLines := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000, Plain: "甲乙\n丙丁戊", Display: "甲乙 丙丁戊", Alignment: 2}}
	lang, err := newPlanLanguage(twoLines, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	spans, err := lang.matchPieces([]string{"甲乙 丙丁戊"})
	if err != nil {
		t.Fatal(err)
	}
	limits := planGeometry{enChars: 65, zhChars: 2}
	got := checkPlanChinese("甲乙 丙丁戊", lang, spans[0], lang.touched(spans[0]), limits)
	if got == nil {
		t.Fatal("a two-line whole piece past the limit passed")
	}
	if !strings.Contains(got.Error(), "5 characters, limit 2") ||
		!strings.Contains(got.Error(), "cue 1 has two source lines") ||
		!strings.Contains(got.Error(), "cut it at a space, punctuation or cue boundary") {
		t.Fatalf("rejection = %q, want the count, the cue and the reason, and the fix", got)
	}
	// A cut piece of a single-line cue says it is part of that cue.
	oneLine := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000, Plain: "甲乙丙丁戊", Display: "甲乙丙丁戊", Alignment: 2}}
	cut, err := newPlanLanguage(oneLine, []int{1}, true)
	if err != nil {
		t.Fatal(err)
	}
	cutSpans, err := cut.matchPieces([]string{"甲乙", "丙丁戊"})
	if err != nil {
		t.Fatal(err)
	}
	got = checkPlanChinese("丙丁戊", cut, cutSpans[1], cut.touched(cutSpans[1]), planGeometry{enChars: 65, zhChars: 2})
	if got == nil || !strings.Contains(got.Error(), "this piece is part of cue 1") {
		t.Fatalf("a cut piece did not name its cue: %v", got)
	}
	// A piece that spans several cues names the range.
	three := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 3000, Plain: "甲乙", Display: "甲乙", Alignment: 2},
		2: {ID: 2, Start: 3000, End: 6000, Plain: "丙丁戊", Display: "丙丁戊", Alignment: 2},
	}
	joined, err := newPlanLanguage(three, []int{1, 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	joinedSpans, err := joined.matchPieces([]string{"甲乙 丙丁戊"})
	if err != nil {
		t.Fatal(err)
	}
	got = checkPlanChinese("甲乙 丙丁戊", joined, joinedSpans[0], joined.touched(joinedSpans[0]), planGeometry{enChars: 65, zhChars: 2})
	if got == nil || !strings.Contains(got.Error(), "spans cues 1 to 2") {
		t.Fatalf("a multi-cue piece did not name its range: %v", got)
	}
	// The same wording exists for English.
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000, Plain: "one two\nthree four", Display: "one two three four", Alignment: 2}}
	enLang, err := newPlanLanguage(en, []int{1}, false)
	if err != nil {
		t.Fatal(err)
	}
	enSpans, err := enLang.matchPieces([]string{"one two three four"})
	if err != nil {
		t.Fatal(err)
	}
	enGot := checkPlanEnglish("one two three four", enLang, enSpans[0], enLang.touched(enSpans[0]), planGeometry{enChars: 5})
	if enGot == nil || !strings.Contains(enGot.Error(), "cue 1 has two source lines") ||
		!strings.Contains(enGot.Error(), "cut it at a space or a cue boundary") {
		t.Fatalf("the English rejection lacks the reason: %v", enGot)
	}
}

package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSplitCheckResponse(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    map[int]bool
	}{
		{name: "all answered", content: `{"items":[{"id":1,"same":false},{"id":0,"same":true}]}`, want: map[int]bool{0: true, 1: false}},
		{name: "missing item", content: `{"items":[{"id":0,"same":true}]}`},
		{name: "repeated id", content: `{"items":[{"id":0,"same":true},{"id":0,"same":false}]}`},
		{name: "id out of range", content: `{"items":[{"id":0,"same":true},{"id":2,"same":true}]}`},
		{name: "missing answer", content: `{"items":[{"id":0,"same":true},{"id":1}]}`},
		{name: "not JSON", content: `same`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateSplitCheckResponse(json.RawMessage(tt.content), 2)
			if tt.want == nil {
				if err == nil {
					t.Fatalf("accepted %s as %v", tt.content, got)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestSplitChecksConfirmed(t *testing.T) {
	checks := newSplitChecks([]SplitCheck{
		{ZH: []string{"甲", "乙"}, EN: []string{"A", "B"}, Same: true},
		{ZH: []string{"丙", "丁"}, EN: []string{"C", "D"}, Same: false},
	}, true)
	if !checks.confirmed([]string{"甲", "乙"}, []string{"A", "B"}) {
		t.Fatal("known same=true check not confirmed")
	}
	if checks.confirmed([]string{"丙", "丁"}, []string{"C", "D"}) {
		t.Fatal("known same=false check confirmed")
	}
	if checks.confirmed([]string{"戊", "己"}, []string{"E", "F"}) {
		t.Fatal("unknown check confirmed")
	}
	if checks.confirmed([]string{"甲"}, []string{"A"}) || checks.confirmed([]string{"甲", "乙"}, []string{"A"}) {
		t.Fatal("single or mismatched parts confirmed")
	}
	if len(checks.pending) != 1 {
		t.Fatalf("pending = %v, want only the unknown question", checks.pending)
	}
	var none *splitChecks
	if none.confirmed([]string{"甲", "乙"}, []string{"A", "B"}) {
		t.Fatal("nil checks confirmed")
	}
}

// londonInputs has two English cues and one two-line Chinese cue that fits on
// one line. The Chinese may follow the English only after a check.
func londonInputs() (Bundle, Mapping) {
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "London has one of the most", Plain: "London has one of the most"},
			{ID: 2, Start: 3000, End: 5000, Display: "sophisticated surveillance systems.", Plain: "sophisticated surveillance systems."},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "伦敦拥有世界上最复杂的监控系统之一", Plain: "伦敦拥有世界上\n最复杂的监控系统之一"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	return bundle, mapping
}

func TestChineseFollowsEnglishOnlyAfterCheck(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars, cfg.ZhChars = 40, 20
	bundle, mapping := londonInputs()
	chinese := func(m Mapping) []string {
		output, err := Render(bundle, m, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range parseDialogues(t, string(output)) {
			zhOnly := e.text
			if idx := strings.Index(zhOnly, `{\rEng}`); idx >= 0 {
				zhOnly = zhOnly[:idx]
			}
			got = append(got, strings.TrimSuffix(stripOverrideTags(zhOnly), `\N`))
		}
		return got
	}

	pending, err := collectSplitChecks(bundle, mapping.Groups, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantQuestion := SplitCheck{
		ZH: []string{"伦敦拥有世界上", "最复杂的监控系统之一"},
		EN: []string{"London has one of the most", "sophisticated surveillance systems."},
	}
	if len(pending) != 1 || !reflect.DeepEqual(pending[0], wantQuestion) {
		t.Fatalf("questions = %+v, want %+v", pending, wantQuestion)
	}

	rejected := mapping
	rejected.SplitChecks = []SplitCheck{wantQuestion}
	for name, m := range map[string]Mapping{"unchecked": mapping, "rejected": rejected} {
		if got := chinese(m); len(got) != 2 || got[0] != got[1] {
			t.Fatalf("%s: Chinese did not stay whole over both English cues: %q", name, got)
		}
	}
	if got := chinese(confirmAllSplits(t, bundle, mapping, cfg)); !reflect.DeepEqual(got, wantQuestion.ZH) {
		t.Fatalf("confirmed: Chinese = %q, want each line with its English cue", got)
	}
}

// TestSnapBeyond500msNeedsCheck pairs with the unchecked 750ms case in
// TestReview939JoinedCounterpartKeepsProjectedNativeBoundary.
func TestSnapBeyond500msNeedsCheck(t *testing.T) {
	es := []Cue{
		review939Cue(1, 1000, 3500, strings.Repeat("An ordinary lead sentence. ", 4), false),
		review939Cue(2, 3500, 5000, "First reply.", false),
		review939Cue(3, 5000, 7000, "Second reply.", false),
	}
	zs := []Cue{
		review939Cue(1, 1000, 4250, "开始的陈述", false),
		review939Cue(2, 4250, 7000, "- 好\n- 不", true),
	}
	b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1, 2, 3}, []int{1, 2})})
	cfg := review939Config()
	_, events, err := renderWithAudits(b, confirmAllSplits(t, b, m, cfg), cfg, renderTestStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		for _, id := range e.zhIDs {
			if id == 2 && e.assStart != 3500 {
				t.Fatalf("checked switch 750ms away did not snap to the English boundary 3500: %+v", events)
			}
			if id == 2 {
				return
			}
		}
	}
	t.Fatalf("second Chinese cue missing: %+v", events)
}

// TestSnapComparesShownTimes is the 08:45 case: the switches are 501 ms apart,
// 0.50 s as the file shows them, so the Chinese switches with the English
// without an order check.
func TestSnapComparesShownTimes(t *testing.T) {
	noWidthMeasurer(t)
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 520187, End: 522063, Display: "the U.S. Justice Department has sided", Plain: "the U.S. Justice Department has sided"},
			{ID: 2, Start: 522064, End: 525233, Display: "with the fallen SEALs of Alpha Platoon and their case", Plain: "with the fallen SEALs\nof Alpha Platoon and their case"},
			{ID: 3, Start: 525234, End: 526692, Display: "against Capstone Industries.", Plain: "against Capstone Industries."},
		},
		[]Cue{
			{ID: 1, Start: 520103, End: 524732, Display: "美国司法部 支持阵亡的海豹突击队A排", Plain: "美国司法部\n支持阵亡的海豹突击队A排"},
			{ID: 2, Start: 524733, End: 527318, Display: "对顶点工业的诉讼", Plain: "对顶点工业的诉讼"},
		},
	)
	groups := []Group{{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}}
	out, _, err := renderWithAudits(bundle, fixtureMapping(bundle, groups), fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range parseDialogues(t, string(out)) {
		if strings.Contains(e.text, "对顶点工业的诉讼") {
			if e.start != 525230 {
				t.Fatalf("Chinese switch at %d, want the English switch 525230:\n%s", e.start, out)
			}
			return
		}
	}
	t.Fatalf("second Chinese cue missing:\n%s", out)
}

func splitCheckServer(t *testing.T, calls *int, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if status != http.StatusOK {
			http.Error(w, "busy", status)
			return
		}
		var payload requestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			return
		}
		var view struct {
			Items []struct {
				ID int `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &view); err != nil {
			t.Errorf("decode items: %v", err)
			return
		}
		items := []map[string]any{}
		for _, item := range view.Items {
			items = append(items, map[string]any{"id": item.ID, "same": true})
		}
		content, _ := json.Marshal(map[string]any{"items": items})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test-model",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCheckSplitsAsksOnceThenUsesCache(t *testing.T) {
	calls := 0
	server := splitCheckServer(t, &calls, http.StatusOK)
	cfg := testConfig(t, server.URL+"/v1")
	cfg.EnChars, cfg.ZhChars = 40, 20
	bundle, mapping := londonInputs()
	cacheDir := t.TempDir()

	checks, audit, requests, hits := NewEngine().checkSplits(context.Background(), bundle, mapping.Groups, cfg, cacheDir)
	if requests != 1 || hits != 0 || audit.Asked != 1 || audit.Confirmed != 1 || len(checks) != 1 || !checks[0].Same {
		t.Fatalf("checks %+v audit %+v requests %d hits %d", checks, audit, requests, hits)
	}
	again, _, requests, hits := NewEngine().checkSplits(context.Background(), bundle, mapping.Groups, cfg, cacheDir)
	if requests != 0 || hits != 1 || calls != 1 || !reflect.DeepEqual(again, checks) {
		t.Fatalf("cached run: checks %+v requests %d hits %d calls %d", again, requests, hits, calls)
	}
}

func TestCheckSplitsFailureLeavesQuestionUnanswered(t *testing.T) {
	calls := 0
	server := splitCheckServer(t, &calls, http.StatusServiceUnavailable)
	cfg := testConfig(t, server.URL+"/v1")
	cfg.EnChars, cfg.ZhChars = 40, 20
	cfg.Retries = 0
	bundle, mapping := londonInputs()

	checks, audit, requests, _ := NewEngine().checkSplits(context.Background(), bundle, mapping.Groups, cfg, t.TempDir())
	if len(checks) != 0 || audit.Asked != 1 || audit.Unanswered != 1 || len(audit.Failures) != 1 || requests != 1 {
		t.Fatalf("checks %+v audit %+v requests %d", checks, audit, requests)
	}
}

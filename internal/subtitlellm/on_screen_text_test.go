package subtitlellm

import (
	"strings"
	"testing"
)

// On-screen text ignores the model layer: untagged labels start at the
// bottom like their source, whatever layer each window returned. Only a
// collision with bottom speech moves a label to the top.
func TestParent33OnScreenTextIgnoresModelLayer(t *testing.T) {
	noWidthMeasurer(t)
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 5500, End: 6200, Display: "Bottom line.", Plain: "Bottom line."}},
		[]Cue{
			{ID: 1, Start: 1000, End: 1600, Display: "文字一", Plain: "文字一", Alignment: 2},
			{ID: 2, Start: 1700, End: 2300, Display: "文字二", Plain: "文字二", Alignment: 2},
			{ID: 3, Start: 2400, End: 3000, Display: "文字三", Plain: "文字三", Alignment: 2},
			{ID: 4, Start: 2500, End: 3200, Display: "顶部台词", Plain: "顶部台词", Top: true, Alignment: 8},
			{ID: 5, Start: 5000, End: 6000, Display: "文字四", Plain: "文字四", Alignment: 2},
		})
	groups := []Group{
		{ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{3}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{4}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue},
		{ZHIDs: []int{5}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	}
	out, _, err := renderWithAudits(bundle, fixtureMapping(bundle, groups), fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range parseDialogues(t, string(out)) {
		for _, label := range []string{"文字一", "文字二", "文字三", "文字四"} {
			if strings.Contains(e.text, label) {
				seen[label] = true
				wantTop := label == "文字四" // collides with bottom speech
				gotTop := strings.HasPrefix(e.style, "Top")
				if gotTop != wantTop {
					t.Errorf("%s: got style %q wantTop=%v", label, e.style, wantTop)
				}
			}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("probe lost visible signs: %v", seen)
	}
}

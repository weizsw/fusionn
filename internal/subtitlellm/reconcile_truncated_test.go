package subtitlellm

import (
	"reflect"
	"testing"
)

// makeTestCues creates sequential cues for testing reconciliation.
func makeTestCues(lang string, ids []int, starts []int, ends []int) []Cue {
	cues := make([]Cue, len(ids))
	for i, id := range ids {
		cues[i] = Cue{
			ID:            id,
			Start:         starts[i],
			End:           ends[i],
			OriginalStart: starts[i],
			OriginalEnd:   ends[i],
			Raw:           lang,
			Plain:         lang,
			Display:       lang,
			Style:         "Default",
			Alignment:     2,
			Changes:       []string{},
		}
	}
	return cues
}

// TestReconcileTruncatedPrefixProjectionAccepted verifies Case A / prefix projection:
// Owner W1 has full group G: EN[10, 11] to ZH[20, 21].
// Window W0 saw only EN[10] and ZH[20] in context (EN 11 and ZH 21 absent from W0 input).
// W0 returned exact projection EN[10] to ZH[20].
// Result: G is accepted; no conflicts.
func TestReconcileTruncatedPrefixProjectionAccepted(t *testing.T) {
	coreMS := 10000 // 10s cores: W0 is [0, 10000), W1 is [10000, 20000)

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 receives EN[10], ZH[20] only (e.g. context overlap reached 10500 but not 11000)
	// W1 receives EN[10, 11], ZH[20, 21]
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}

	// Verify the complete owner group was accepted
	var modelGroups []Group
	for _, g := range accepted {
		if g.Origin == OriginModel {
			modelGroups = append(modelGroups, g)
		}
	}
	if len(modelGroups) != 1 {
		t.Fatalf("expected 1 model group, got %d", len(modelGroups))
	}
	g := modelGroups[0]
	if !reflect.DeepEqual(g.ENIDs, []int{10, 11}) || !reflect.DeepEqual(g.ZHIDs, []int{20, 21}) {
		t.Fatalf("unexpected accepted group: %+v", g)
	}
	if g.OwnerWindow != 1 {
		t.Fatalf("expected owner window 1, got %d", g.OwnerWindow)
	}
	if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
		t.Fatalf("expected 0 restored cues, got EN=%v, ZH=%v", audit.Restored["en"], audit.Restored["zh"])
	}
}

// TestReconcileTruncatedSuffixProjectionAccepted verifies Case B / suffix projection:
// Owner W0 has full group G: EN[10, 11] to ZH[20, 21].
// Following window W1 saw only EN[11] and ZH[21] in context (EN 10 and ZH 20 absent from W1 input).
// W1 returned exact projection EN[11] to ZH[21].
// Result: G is accepted; no conflicts.
func TestReconcileTruncatedSuffixProjectionAccepted(t *testing.T) {
	coreMS := 10000 // 10s cores: W0 is [0, 10000), W1 is [10000, 20000)

	en := makeTestCues("en", []int{10, 11}, []int{8500, 9500}, []int{9000, 10200})
	zh := makeTestCues("zh", []int{20, 21}, []int{8500, 9500}, []int{9000, 10200})
	bundle := testBundle(t, en, zh)

	// W0 receives EN[10, 11], ZH[20, 21]
	// W1 receives EN[11], ZH[21] only (context overlap started at 9500)
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{11},
			ZHInput: []int{21},
			Valid: []Group{
				{ENIDs: []int{11}, ZHIDs: []int{21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}

	var modelGroups []Group
	for _, g := range accepted {
		if g.Origin == OriginModel {
			modelGroups = append(modelGroups, g)
		}
	}
	if len(modelGroups) != 1 {
		t.Fatalf("expected 1 model group, got %d", len(modelGroups))
	}
	g := modelGroups[0]
	if !reflect.DeepEqual(g.ENIDs, []int{10, 11}) || !reflect.DeepEqual(g.ZHIDs, []int{20, 21}) {
		t.Fatalf("unexpected accepted group: %+v", g)
	}
	if g.OwnerWindow != 0 {
		t.Fatalf("expected owner window 0, got %d", g.OwnerWindow)
	}
}

// TestReconcilePrefixAndSuffixProjectionsBothAccepted verifies a group with
// both preceding prefix truncation and succeeding suffix truncation.
func TestReconcilePrefixAndSuffixProjectionsBothAccepted(t *testing.T) {
	coreMS := 10000 // W0 [0, 10000), W1 [10000, 20000), W2 [20000, 30000)

	en := makeTestCues("en", []int{10, 11, 12}, []int{10200, 15000, 19800}, []int{10800, 15500, 20200})
	zh := makeTestCues("zh", []int{20, 21, 22}, []int{10200, 15000, 19800}, []int{10800, 15500, 20200})
	bundle := testBundle(t, en, zh)

	// W0 receives EN[10], ZH[20]
	// W1 receives EN[10, 11, 12], ZH[20, 21, 22] (owner of G)
	// W2 receives EN[12], ZH[22]
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11, 12},
			ZHInput: []int{20, 21, 22},
			Valid: []Group{
				{ENIDs: []int{10, 11, 12}, ZHIDs: []int{20, 21, 22}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   2,
			ENInput: []int{12},
			ZHInput: []int{22},
			Valid: []Group{
				{ENIDs: []int{12}, ZHIDs: []int{22}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}

	var modelGroups []Group
	for _, g := range accepted {
		if g.Origin == OriginModel {
			modelGroups = append(modelGroups, g)
		}
	}
	if len(modelGroups) != 1 {
		t.Fatalf("expected 1 model group, got %d", len(modelGroups))
	}
	g := modelGroups[0]
	if !reflect.DeepEqual(g.ENIDs, []int{10, 11, 12}) || !reflect.DeepEqual(g.ZHIDs, []int{20, 21, 22}) {
		t.Fatalf("unexpected accepted group: %+v", g)
	}
}

// TestReconcileTruncatedInputConflictingTranslationStillConflict verifies that if
// a truncated input returns a different translation or links an outside ID,
// it remains a conflict.
func TestReconcileTruncatedInputConflictingTranslationStillConflict(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21, 99}, []int{10500, 11000, 5000}, []int{10800, 11500, 5500})
	bundle := testBundle(t, en, zh)

	// W0 saw EN[10] and ZH[20, 99], but mapped EN[10] to ZH[99] instead of ZH[20]
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20, 99},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{99}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflicts due to contradictory translation, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("conflicting model group was accepted: %+v", g)
		}
	}
}

// TestReconcileTruncatedInputOmittedProvidedMemberStillConflict verifies that if
// a window was provided a member of G but the model omitted it, it remains a conflict.
func TestReconcileTruncatedInputOmittedProvidedMemberStillConflict(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 was provided EN[10], ZH[20] AND ZH[21] (only EN 11 was missing).
	// But W0's model output only mapped EN[10] to ZH[20], omitting provided ZH[21].
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflict because W0 omitted visible member ZH 21, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("conflicting model group was accepted: %+v", g)
		}
	}
}

// TestReconcileTwoFullyInformedWindowsDisagreeStillConflict verifies that if two
// windows both received all cues of a group but disagree on grouping, it is a conflict.
func TestReconcileTwoFullyInformedWindowsDisagreeStillConflict(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10}, []int{10500}, []int{10800})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 10600}, []int{10800, 10900})
	bundle := testBundle(t, en, zh)

	// Both W0 and W1 received EN[10] and ZH[20, 21].
	// W1 maps EN[10] to ZH[20, 21].
	// W0 maps EN[10] to ZH[20] only.
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflict between fully informed windows, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("conflicting model group was accepted: %+v", g)
		}
	}
}

// TestReconcileCompetingOwnerCandidateNotSuppressed verifies that a rival owner
// candidate is not suppressed as mere context.
func TestReconcileCompetingOwnerCandidateNotSuppressed(t *testing.T) {
	coreMS := 10000

	// Cue 10 is at 5000 (core 0), cue 11 is at 15000 (core 1)
	en := makeTestCues("en", []int{10, 11}, []int{5000, 15000}, []int{5500, 15500})
	zh := makeTestCues("zh", []int{20, 21}, []int{5000, 15000}, []int{5500, 15500})
	bundle := testBundle(t, en, zh)

	// W0 has owner candidate G1: EN[10, 11] to ZH[20, 21] (firstStart is 5000 -> W0 owner).
	// W1 has owner candidate G2: EN[11] to ZH[21] (firstStart is 15000 -> W1 owner).
	// G2 is an owner candidate for W1, NOT mere context. Neither should suppress the other.
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{11},
			ZHInput: []int{21},
			Valid: []Group{
				{ENIDs: []int{11}, ZHIDs: []int{21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflicts between competing owner candidates, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("competing owner candidate was accepted: %+v", g)
		}
	}
}

// TestReconcileLayerMismatchStillConflict verifies that a layer discrepancy between
// owner candidate and truncated context observation is still a conflict.
func TestReconcileLayerMismatchStillConflict(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 returned exact projection of IDs, but LayerTop instead of LayerBottom!
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflict due to layer mismatch, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("layer-conflicting group was accepted: %+v", g)
		}
	}
}

// TestReconcileKindMetadataMismatchStillConflict verifies that conflicting kind
// classifications between owner and truncated observation are not ignored.
func TestReconcileKindMetadataMismatchStillConflict(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 returned exact projection of IDs and same layer, but KindOnScreenText vs KindDialogue!
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflict due to kind mismatch, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("kind-conflicting group was accepted: %+v", g)
		}
	}
}

// TestReconcileUnknownInputMembershipPreservesOldProtection verifies that if
// a batch does not carry input membership (nil), the old conservative conflict behavior is kept.
func TestReconcileUnknownInputMembershipPreservesOldProtection(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 has nil ENInput and ZHInput (unknown membership)
	batches := []batch{
		{
			Index:   0,
			ENInput: nil,
			ZHInput: nil,
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflict when input membership is unknown, got 0")
	}

	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("group was accepted despite unknown input membership: %+v", g)
		}
	}
}

// TestReconcileSparseWindowsAndConfigurableOverlap verifies that sparse window
// indexes and non-default core sizes work cleanly without out-of-bounds errors.
func TestReconcileSparseWindowsAndConfigurableOverlap(t *testing.T) {
	coreMS := 5000 // 5s cores

	en := makeTestCues("en", []int{10, 11}, []int{10500, 11000}, []int{10800, 11500}) // in W2 [10000, 15000)
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 11000}, []int{10800, 11500})
	bundle := testBundle(t, en, zh)

	// Sparse indexes: Window 0 and Window 2 exist; Window 1 was empty / absent.
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   2,
			ENInput: []int{10, 11},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10, 11}, ZHIDs: []int{20, 21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts with sparse batches, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}

	var modelGroups []Group
	for _, g := range accepted {
		if g.Origin == OriginModel {
			modelGroups = append(modelGroups, g)
		}
	}
	if len(modelGroups) != 1 {
		t.Fatalf("expected 1 model group, got %d", len(modelGroups))
	}
	if modelGroups[0].OwnerWindow != 2 {
		t.Fatalf("expected owner window 2, got %d", modelGroups[0].OwnerWindow)
	}
}

// TestReconcileIDConservationExactlyOnce verifies that every cue in the source
// bundle appears exactly once in the accepted output.
func TestReconcileIDConservationExactlyOnce(t *testing.T) {
	coreMS := 10000

	en := makeTestCues("en", []int{1, 2, 3}, []int{1000, 10500, 11000}, []int{1500, 10800, 11500})
	zh := makeTestCues("zh", []int{1, 2, 3}, []int{1000, 10500, 11000}, []int{1500, 10800, 11500})
	bundle := testBundle(t, en, zh)

	// W0 has cue 1 (paired) and truncated observation of 2.
	// W1 has cue 2, 3 (paired).
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{1, 2},
			ZHInput: []int{1, 2},
			Valid: []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{2, 3},
			ZHInput: []int{2, 3},
			Valid: []Group{
				{ENIDs: []int{2, 3}, ZHIDs: []int{2, 3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}

	enSeen := map[int]int{}
	zhSeen := map[int]int{}
	for _, g := range accepted {
		for _, id := range g.ENIDs {
			enSeen[id]++
		}
		for _, id := range g.ZHIDs {
			zhSeen[id]++
		}
	}

	for _, id := range []int{1, 2, 3} {
		if enSeen[id] != 1 {
			t.Errorf("EN cue %d seen %d times (expected 1)", id, enSeen[id])
		}
		if zhSeen[id] != 1 {
			t.Errorf("ZH cue %d seen %d times (expected 1)", id, zhSeen[id])
		}
	}
}

package subtitlellm

import (
	"strings"
	"testing"
)

func TestAlignTranslatedPreservesOrderAndLayers(t *testing.T) {
	en, zh := testCues(3, 1000)
	en[0].Top = true
	zh[1].Top = true
	bundle := testBundle(t, en, zh)
	mapping, audit, err := AlignTranslated(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Groups) != len(en) {
		t.Fatalf("groups = %d, want %d", len(mapping.Groups), len(en))
	}
	for i, group := range mapping.Groups {
		layer := LayerTop
		if i == 2 {
			layer = LayerBottom
		}
		if len(group.ENIDs) != 1 || len(group.ZHIDs) != 1 || group.ENIDs[0] != i+1 || group.ZHIDs[0] != i+1 || group.Layer != layer || group.Origin != OriginTranslation {
			t.Fatalf("group %d = %+v", i, group)
		}
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil || mapping.Fingerprint != fingerprint || mapping.SourceFingerprint != bundle.Fingerprint {
		t.Fatalf("invalid mapping fingerprint: %v", err)
	}
	if audit.Pairing.ENPaired != 1 || audit.Pairing.ZHPaired != 1 || audit.Pairing.Low() {
		t.Fatalf("pairing = %+v", audit.Pairing)
	}
	if err := validateImportedGroups(mapping, bundle); err != nil {
		t.Fatalf("direct mapping failed import validation: %v", err)
	}
}

func TestAlignTranslatedRejectsInvalidBundle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Bundle)
		want   string
	}{
		{"fingerprint", func(b *Bundle) { b.Fingerprint = "wrong" }, "fingerprint mismatch"},
		{"ids", func(b *Bundle) { b.ZH[0].ID = 2 }, "not sequential"},
		{"original timing", func(b *Bundle) { b.ZH[0].OriginalStart++ }, "timing differs"},
		{"adjusted timing", func(b *Bundle) { b.ZH[0].Start++ }, "timing differs"},
		{"empty track", func(b *Bundle) { b.ZH = nil }, "no zh cues"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			en, zh := testCues(1, 1000)
			bundle := testBundle(t, en, zh)
			bundle.Fingerprint = ""
			tc.mutate(&bundle)
			_, _, err := AlignTranslated(bundle)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestImportRejectsInvalidTranslationGroups(t *testing.T) {
	for _, tc := range []struct {
		name  string
		enIDs []int
		zhIDs []int
		shift bool
		want  string
	}{
		{"one sided", []int{1}, nil, false, "same cue ID 1:1"},
		{"many to one", []int{1, 2}, []int{1}, false, "same cue ID 1:1"},
		{"different IDs", []int{1}, []int{2}, false, "same cue ID 1:1"},
		{"different timing", []int{1}, []int{1}, true, "timing differs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			en, zh := testCues(2, 1000)
			if tc.shift {
				zh[0].OriginalStart++
			}
			bundle := testBundle(t, en, zh)
			mapping := Mapping{Groups: []Group{{
				ENIDs: tc.enIDs, ZHIDs: tc.zhIDs, Layer: LayerBottom, Origin: OriginTranslation,
			}}}
			err := validateImportedGroups(mapping, bundle)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

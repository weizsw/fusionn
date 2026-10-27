package subtitlellm

import "fmt"

// AlignTranslated pairs a translation with its source without a model request.
// Both tracks must preserve cue order and timestamps.
func AlignTranslated(bundle Bundle) (Mapping, Audit, error) {
	bundle, err := ensureBundleFingerprint(bundle)
	if err != nil {
		return Mapping{}, Audit{}, err
	}
	if len(bundle.EN) != len(bundle.ZH) {
		return Mapping{}, Audit{}, fmt.Errorf("translated subtitle cue counts differ: en=%d zh=%d", len(bundle.EN), len(bundle.ZH))
	}
	groups := make([]Group, len(bundle.EN))
	for i, en := range bundle.EN {
		zh := bundle.ZH[i]
		if !sameTranslationTiming(en, zh) {
			return Mapping{}, Audit{}, fmt.Errorf("translated subtitle cue %d timing differs: en=%d-%d zh=%d-%d", en.ID, en.OriginalStart, en.OriginalEnd, zh.OriginalStart, zh.OriginalEnd)
		}
		layer := LayerBottom
		if en.Top || zh.Top {
			layer = LayerTop
		}
		groups[i] = Group{ENIDs: []int{en.ID}, ZHIDs: []int{zh.ID}, Layer: layer, Origin: OriginTranslation}
	}
	mapping := Mapping{
		Version: Version, SourceFingerprint: bundle.Fingerprint,
		Parameters: MappingParameters{
			MaxDistanceMS: MaxDistanceMS, SchemaEnforcement: "direct 1:1; cue counts and timestamps verified",
		},
		Groups: groups,
	}
	mapping.Fingerprint, err = mappingDigest(mapping)
	if err != nil {
		return Mapping{}, Audit{}, err
	}
	audit := Audit{
		Rejected: []RejectedRow{}, Conflicts: []Group{},
		Restored: map[string][]int{"en": {}, "zh": {}}, WindowMissing: []WindowMissing{},
		Pairing: pairingStats(bundle, groups), Coverage: Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		Provenance: &Provenance{Source: OriginTranslation, SourceFingerprint: bundle.Fingerprint},
	}
	return mapping, audit, nil
}

// Compare original times too so offset correction cannot hide a wrong source.
func sameTranslationTiming(en, zh Cue) bool {
	return en.Start == zh.Start && en.End == zh.End &&
		en.OriginalStart == zh.OriginalStart && en.OriginalEnd == zh.OriginalEnd
}

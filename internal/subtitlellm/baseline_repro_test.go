package subtitlellm

import (
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

func nToOneFixture(t *testing.T) (Bundle, Mapping, config.LLMConfig, config.ASSStyleConfig) {
	t.Helper()
	bundle := Bundle{Version: Version,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "When our memory pathways get disrupted,", Plain: "When our memory pathways\nget disrupted,"},
			{ID: 2, Start: 4000, End: 7000, Display: "it can lead to things like conflation.", Plain: "it can lead to things like conflation."},
		},
		ZH: []Cue{
			{ID: 1, Start: 1200, End: 6800, Display: "当我们的记忆通道被扰乱时 会导致混淆之类的情况", Plain: "当我们的记忆通道被扰乱时\n会导致混淆之类的情况"},
		},
	}
	digest, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = digest
	mapping := Mapping{Version: Version, SourceFingerprint: digest,
		Groups: []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}}}
	md, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = md
	cfg := config.LLMConfig{EnChars: 65, ZhChars: 20, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500}
	style := config.ASSStyleConfig{Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Go", PrimarySize: 40, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Go", SecondarySize: 30, SecondaryColor: "&H00FFFFFF",
		Outline: 1, OutlineColor: "&H00000000", BackColor: "&H00000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12}
	return bundle, mapping, cfg, style
}

// zhSequence extracts the Chinese part of each bilingual event in order.
func zhSequence(t *testing.T, ass string) []string {
	t.Helper()
	var seq []string
	for _, line := range strings.Split(ass, "\n") {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		f := strings.SplitN(line, ",", 10)
		if len(f) < 10 {
			continue
		}
		zh := strings.Split(f[9], `\N`)[0]
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
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		f := strings.SplitN(line, ",", 10)
		if len(f) < 10 {
			continue
		}
		idx := strings.Index(f[9], `{\rEng}`)
		if idx < 0 {
			continue
		}
		en := f[9][idx+len(`{\rEng}`):]
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

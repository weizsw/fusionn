package subtitle

import (
	"testing"

	"github.com/fusionn/internal/executor"
)

func newAnalyzer() *Analyzer {
	return NewAnalyzer(
		[]string{"eng", "en", "eng-us"},
		[]string{"chi", "zho", "zh-Hans", "zh-CN", "zh-Hant", "zh-TW"},
		[]string{"简体", "简中", "CHS"},
		[]string{"繁體", "繁中", "CHT"},
	)
}

func makeStream(index int, title string, disposition map[string]int, frames, bytes string) executor.StreamInfo {
	tags := map[string]string{"language": "eng"}
	if title != "" {
		tags["title"] = title
	}
	if frames != "" {
		tags["NUMBER_OF_FRAMES"] = frames
	}
	if bytes != "" {
		tags["NUMBER_OF_BYTES"] = bytes
	}
	return executor.StreamInfo{
		Index:       index,
		CodecType:   "subtitle",
		CodecName:   "subrip",
		Tags:        tags,
		Disposition: disposition,
	}
}

func makeStreamWithCodec(index int, codecName, title string, disposition map[string]int, frames, bytes string) executor.StreamInfo {
	stream := makeStream(index, title, disposition, frames, bytes)
	stream.CodecName = codecName
	return stream
}

func TestScoreEnglishTrack_ForcedDisposition(t *testing.T) {
	forced := makeStream(2, "", map[string]int{"forced": 1}, "23", "496")
	regular := makeStream(3, "", nil, "567", "16792")

	forcedScore := scoreEnglishTrack(forced, "", 23, 496, 567, 16792, 2)
	regularScore := scoreEnglishTrack(regular, "", 567, 16792, 567, 16792, 2)

	if forcedScore <= regularScore {
		t.Errorf("forced track penalty (%d) should be higher than regular (%d)", forcedScore, regularScore)
	}
}

func TestScoreEnglishTrack_ForcedByTitle(t *testing.T) {
	forced := makeStream(2, "Forced", nil, "23", "496")
	regular := makeStream(3, "", nil, "567", "16792")

	forcedScore := scoreEnglishTrack(forced, "forced", 23, 496, 567, 16792, 2)
	regularScore := scoreEnglishTrack(regular, "", 567, 16792, 567, 16792, 2)

	if forcedScore <= regularScore {
		t.Errorf("forced-by-title penalty (%d) should be higher than regular (%d)", forcedScore, regularScore)
	}
}

func TestScoreEnglishTrack_ForcedByFrameHeuristic(t *testing.T) {
	// No disposition, no title — only frame/byte count distinguishes
	forced := makeStream(2, "", nil, "23", "496")
	regular := makeStream(3, "", nil, "567", "16792")

	forcedScore := scoreEnglishTrack(forced, "", 23, 496, 567, 16792, 2)
	regularScore := scoreEnglishTrack(regular, "", 567, 16792, 567, 16792, 2)

	if forcedScore <= regularScore {
		t.Errorf("frame-heuristic penalty (%d) should be higher than regular (%d)", forcedScore, regularScore)
	}
}

func TestScoreEnglishTrack_ByteHeuristicOnly(t *testing.T) {
	// Same frame count so only byte heuristic can differ.
	smallBytes := makeStream(2, "", nil, "400", "1000")
	largeBytes := makeStream(3, "", nil, "400", "10000")

	smallScore := scoreEnglishTrack(smallBytes, "", 400, 1000, 400, 10000, 2)
	largeScore := scoreEnglishTrack(largeBytes, "", 400, 10000, 400, 10000, 2)

	if smallScore <= largeScore {
		t.Errorf("byte-heuristic penalty (%d) should be higher than reference (%d)", smallScore, largeScore)
	}
}

func TestScoreEnglishTrack_SDHByTitleOnly(t *testing.T) {
	sdhTitleOnly := makeStream(4, "SDH", nil, "567", "16792")
	regular := makeStream(3, "", nil, "567", "16792")

	sdhScore := scoreEnglishTrack(sdhTitleOnly, "sdh", 567, 16792, 567, 16792, 2)
	regularScore := scoreEnglishTrack(regular, "", 567, 16792, 567, 16792, 2)

	if sdhScore <= regularScore {
		t.Errorf("SDH title-only penalty (%d) should be higher than regular (%d)", sdhScore, regularScore)
	}
}

func TestScoreEnglishTrack_MissingFrameByteTagsNoHeuristicPenalty(t *testing.T) {
	noStats := makeStream(2, "", nil, "", "")

	score := scoreEnglishTrack(noStats, "", 0, 0, 567, 16792, 2)
	if score != 0 {
		t.Errorf("expected no heuristic penalty for missing frame/byte tags, got %d", score)
	}
}

func TestScoreEnglishTrack_HeuristicBoundaryAt25Percent(t *testing.T) {
	// Exactly 25% should NOT be penalized because the condition is strictly less-than.
	boundary := makeStream(2, "", nil, "100", "1000")

	score := scoreEnglishTrack(boundary, "", 100, 1000, 400, 4000, 2)
	if score != 0 {
		t.Errorf("expected no penalty at exactly 25%% boundary, got %d", score)
	}
}

func TestScoreEnglishTrack_SDH(t *testing.T) {
	sdh := makeStream(4, "SDH", map[string]int{"hearing_impaired": 1}, "714", "20287")
	regular := makeStream(3, "", nil, "567", "16792")
	forced := makeStream(2, "Forced", map[string]int{"forced": 1}, "23", "496")

	sdhScore := scoreEnglishTrack(sdh, "sdh", 714, 20287, 714, 20287, 3)
	regularScore := scoreEnglishTrack(regular, "", 567, 16792, 714, 20287, 3)
	forcedScore := scoreEnglishTrack(forced, "forced", 23, 496, 714, 20287, 3)

	if sdhScore <= regularScore {
		t.Errorf("SDH penalty (%d) should be higher than regular (%d)", sdhScore, regularScore)
	}
	if sdhScore >= forcedScore {
		t.Errorf("SDH penalty (%d) should be lower than forced (%d)", sdhScore, forcedScore)
	}
}

func TestDetectEnglishSubtitle_SingleTrack(t *testing.T) {
	a := newAnalyzer()

	streams := []executor.StreamInfo{
		makeStream(2, "Forced", map[string]int{"forced": 1}, "23", "496"),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("single English track should always be selected")
	}
	if track.Index != 2 {
		t.Errorf("expected index 2, got %d", track.Index)
	}
}

func TestDetectEnglishSubtitle_TieBreakByFrameCount(t *testing.T) {
	a := newAnalyzer()

	// Two tracks with identical metadata except frame count
	streams := []executor.StreamInfo{
		makeStream(3, "", nil, "400", "10000"),
		makeStream(4, "", nil, "600", "10000"),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("expected a track to be selected")
	}
	if track.Index != 4 {
		t.Errorf("expected index 4 (more frames), got %d", track.Index)
	}
}

func TestDetectEnglishSubtitle_RealWorld3Tracks(t *testing.T) {
	a := newAnalyzer()

	streams := []executor.StreamInfo{
		makeStream(2, "Forced", map[string]int{
			"default": 1, "original": 1, "forced": 1,
		}, "23", "496"),
		makeStream(3, "", map[string]int{
			"original": 1,
		}, "567", "16792"),
		makeStream(4, "SDH", map[string]int{
			"original": 1, "hearing_impaired": 1,
		}, "714", "20287"),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("expected a track to be selected")
	}
	if track.Index != 3 {
		t.Errorf("expected regular English at index 3, got index %d", track.Index)
	}
}

func TestDetectEnglishSubtitle_PrefersTextSubtitleOverBitmapSDH(t *testing.T) {
	a := newAnalyzer()

	streams := []executor.StreamInfo{
		makeStreamWithCodec(2, "subrip", "English [SRT]", nil, "1381", "43181"),
		makeStreamWithCodec(3, "hdmv_pgs_subtitle", "English [SDH]", nil, "3462", "27044840"),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("expected a track to be selected")
	}
	if track.Index != 2 {
		t.Errorf("expected extractable text subtitle at index 2, got index %d", track.Index)
	}
}

func TestDetectEnglishSubtitle_FallbackToSDHOverForced(t *testing.T) {
	a := newAnalyzer()

	streams := []executor.StreamInfo{
		makeStream(2, "Forced", map[string]int{"forced": 1}, "23", "496"),
		makeStream(4, "SDH", map[string]int{"hearing_impaired": 1}, "714", "20287"),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("expected a track to be selected")
	}
	if track.Index != 4 {
		t.Errorf("expected SDH at index 4 over forced, got index %d", track.Index)
	}
}

func TestDetectEnglishSubtitle_AllMetadataMissingFallsBackToStreamOrder(t *testing.T) {
	a := newAnalyzer()

	// Same language, no title/disposition/frame/byte metadata.
	// Both tracks score equally and have equal frame counts (0), so stream order wins.
	streams := []executor.StreamInfo{
		makeStream(2, "", nil, "", ""),
		makeStream(3, "", nil, "", ""),
	}

	track := a.detectEnglishSubtitle(streams)
	if track == nil {
		t.Fatal("expected a track to be selected")
	}
	if track.Index != 2 {
		t.Errorf("expected first track (index 2) by stream-order fallback, got index %d", track.Index)
	}
}

func TestHelpers_IsForced(t *testing.T) {
	if !isForced(executor.StreamInfo{Disposition: map[string]int{"forced": 1}}) {
		t.Error("expected isForced=true")
	}
	if isForced(executor.StreamInfo{Disposition: map[string]int{"forced": 0}}) {
		t.Error("expected isForced=false for forced=0")
	}
	if isForced(executor.StreamInfo{Disposition: nil}) {
		t.Error("expected isForced=false for nil disposition")
	}
}

func TestHelpers_IsHearingImpaired(t *testing.T) {
	if !isHearingImpaired(executor.StreamInfo{Disposition: map[string]int{"hearing_impaired": 1}}) {
		t.Error("expected isHearingImpaired=true")
	}
	if isHearingImpaired(executor.StreamInfo{Disposition: nil}) {
		t.Error("expected isHearingImpaired=false for nil disposition")
	}
}

func TestHelpers_TitleKeywords(t *testing.T) {
	if !isForcedByTitle("forced") {
		t.Error("expected 'forced' to match forced title")
	}
	if !isForcedByTitle("signs & songs") {
		t.Error("expected 'signs & songs' to match forced title")
	}
	if !isForcedByTitle("signs") {
		t.Error("expected 'signs' to match forced title")
	}
	if isForcedByTitle("english") {
		t.Error("'english' should not match forced title")
	}

	if !isSDHByTitle("sdh") {
		t.Error("expected 'sdh' to match SDH title")
	}
	if !isSDHByTitle("hearing impaired") {
		t.Error("expected 'hearing impaired' to match SDH title")
	}
	if !isSDHByTitle("cc") {
		t.Error("expected 'cc' to match SDH title")
	}
}

func TestHelpers_FrameByteCount(t *testing.T) {
	stream := makeStream(0, "", nil, "567", "16792")
	frames, ok := getFrameCount(stream)
	if !ok || frames != 567 {
		t.Errorf("expected frames=567, got %d (ok=%v)", frames, ok)
	}
	bytes, ok := getByteCount(stream)
	if !ok || bytes != 16792 {
		t.Errorf("expected bytes=16792, got %d (ok=%v)", bytes, ok)
	}

	// Missing tags
	noTags := executor.StreamInfo{Tags: nil}
	_, ok = getFrameCount(noTags)
	if ok {
		t.Error("expected ok=false for nil tags")
	}
	_, ok = getByteCount(noTags)
	if ok {
		t.Error("expected ok=false for nil tags")
	}
}

func TestDetectEnglishSubtitle_SetsIsSDH(t *testing.T) {
	analyzer := NewAnalyzer([]string{"eng"}, nil, nil, nil)

	tests := []struct {
		name      string
		streams   []executor.StreamInfo
		wantIsSDH bool
	}{
		{
			name: "SDH by disposition",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags:        map[string]string{"language": "eng"},
					Disposition: map[string]int{"hearing_impaired": 1}},
			},
			wantIsSDH: true,
		},
		{
			name: "SDH by title",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English SDH"}},
			},
			wantIsSDH: true,
		},
		{
			name: "Non-SDH track",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English"}},
			},
			wantIsSDH: false,
		},
		{
			name: "SDH exists but non-SDH wins",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English"}},
				{Index: 3, CodecType: "subtitle", CodecName: "subrip",
					Tags:        map[string]string{"language": "eng", "title": "English SDH"},
					Disposition: map[string]int{"hearing_impaired": 1}},
			},
			wantIsSDH: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := analyzer.detectEnglishSubtitle(tt.streams)
			if track == nil {
				t.Fatal("expected a track, got nil")
			}
			if track.IsSDH != tt.wantIsSDH {
				t.Errorf("IsSDH = %v, want %v", track.IsSDH, tt.wantIsSDH)
			}
		})
	}
}

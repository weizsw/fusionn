package subtitle

import (
	"testing"
)

func TestTranslationQueueShouldRun_SkipsWhenChineseSubPathSet(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: nil,
		},
		ChineseSubPath: "/path/to/bazarr-downloaded.zh.srt",
	}

	if proc.ShouldRun(pctx) {
		t.Error("TranslationQueue should NOT run when ChineseSubPath is already set (Bazarr found a subtitle)")
	}
}

func TestTranslationQueueShouldRun_RunsWhenNoChineseSub(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: nil,
		},
		ChineseSubPath: "",
	}

	if !proc.ShouldRun(pctx) {
		t.Error("TranslationQueue SHOULD run when no Chinese subtitle exists")
	}
}

func TestTranslationQueueShouldRun_SkipsWhenChineseTrackExists(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: &Track{Index: 2, Language: "chi"},
		},
	}

	if proc.ShouldRun(pctx) {
		t.Error("TranslationQueue should NOT run when Chinese track exists in media")
	}
}

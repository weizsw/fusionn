package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fusionn/internal/subtitlellm"
)

func makeTestBundle() subtitlellm.Bundle {
	return subtitlellm.Bundle{
		EN: []subtitlellm.Cue{
			{ID: 1, Start: 1000, End: 2000, Plain: "Hello world", Top: false},
			{ID: 2, Start: 2500, End: 3500, Plain: "Second line\nwith break", Top: false},
		},
		ZH: []subtitlellm.Cue{
			{ID: 1, Start: 1100, End: 2050, Plain: "你好世界这是一段非常长的中文测试内容用于检查UTF8安全截断", Top: false},
			{ID: 2, Start: 2600, End: 3400, Plain: "第二行含有换行\n以及竖线|测试", Top: false},
		},
	}
}

func makeTestMapping() subtitlellm.Mapping {
	return subtitlellm.Mapping{
		Groups: []subtitlellm.Group{
			{
				ENIDs:  []int{1},
				ZHIDs:  []int{1},
				Kind:   "dialogue",
				Layer:  "bottom",
				Origin: "model",
			},
			{
				ENIDs:  []int{2},
				ZHIDs:  []int{2},
				Kind:   "dialogue",
				Layer:  "bottom",
				Origin: "model",
			},
		},
	}
}

func makeTestEvents() []RenderedEvent {
	return []RenderedEvent{
		{
			GroupIndex: 0,
			ENIDs:      []int{1},
			ZHIDs:      []int{1},
			StartMS:    1000,
			EndMS:      2050,
			Alignment:  2,
			Strategy:   "1:1_anchor",
			ENText:     "Hello world",
			ZHText:     "你好世界这是一段非常长的中文测试内容用于检查UTF8安全截断",
		},
		{
			GroupIndex: 1,
			ENIDs:      []int{2},
			ZHIDs:      []int{2},
			StartMS:    2500,
			EndMS:      3500,
			Alignment:  2,
			Strategy:   "1:1_anchor",
			ENText:     "Second line\nwith break",
			ZHText:     "第二行含有换行\n以及竖线|测试",
		},
	}
}

func TestTruncateRunesAndMarkdownSanitize(t *testing.T) {
	longZH := "你好世界这是一个很长的中文测试句子"
	truncated := truncateRunes(longZH, 10)
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated string is not valid UTF-8: %q", truncated)
	}
	if len([]rune(truncated)) != 10 {
		t.Fatalf("expected 10 runes, got %d runes: %q", len([]rune(truncated)), truncated)
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Fatalf("expected ellipsis suffix, got: %q", truncated)
	}

	withSpecial := "第一行\n第二行|带管道"
	sanitized := sanitizeMarkdownCell(withSpecial, 20)
	if strings.Contains(sanitized, "\n") {
		t.Fatalf("newlines not stripped: %q", sanitized)
	}
	if !strings.Contains(sanitized, `\|`) {
		t.Fatalf("pipe not escaped: %q", sanitized)
	}
	if !utf8.ValidString(sanitized) {
		t.Fatalf("sanitized string is not valid UTF-8: %q", sanitized)
	}
}

// Negative Test 1: Missing rendered events for an existing group must fail validation.
func TestLedgerValidation_DetectsMissingGroup(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()
	events := makeTestEvents()

	// Drop group 1 from rendered events
	corruptedEvents := events[:1]

	_, errs := ValidateAndBuildLedger(bundle, mapping, nil, corruptedEvents)
	if len(errs) == 0 {
		t.Fatalf("expected validation failure for missing group 1, got 0 errors")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "group 1") && strings.Contains(e, "missing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error mentioning missing group 1, got: %v", errs)
	}
}

// Negative Test 2: Rendered event referencing a foreign/tampered cue ID must fail validation.
func TestLedgerValidation_DetectsTamperedCueAssociation(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()
	events := makeTestEvents()

	// Tamper event 0: add foreign EN ID 999
	events[0].ENIDs = []int{1, 999}

	_, errs := ValidateAndBuildLedger(bundle, mapping, nil, events)
	if len(errs) == 0 {
		t.Fatalf("expected validation failure for foreign cue ID 999, got 0 errors")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "999") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error mentioning foreign cue ID 999, got: %v", errs)
	}
}

// Negative Test 3: Rendered event referencing a non-existent group ID must fail validation.
func TestLedgerValidation_DetectsUntraceableOutputEvent(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()
	events := makeTestEvents()

	// Add an untraceable event referencing group 999
	events = append(events, RenderedEvent{
		GroupIndex: 999,
		ENIDs:      []int{1},
		ZHIDs:      []int{1},
		StartMS:    5000,
		EndMS:      6000,
		Alignment:  2,
		Strategy:   "untraceable",
	})

	_, errs := ValidateAndBuildLedger(bundle, mapping, nil, events)
	if len(errs) == 0 {
		t.Fatalf("expected validation failure for untraceable group 999, got 0 errors")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "999") && strings.Contains(e, "untraceable") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error mentioning untraceable group 999, got: %v", errs)
	}
}

// Positive Test 1: Synthetic valid input passes with zero errors and correct ledger entries.
func TestLedgerValidation_PositiveSynthetic(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()
	events := makeTestEvents()

	ledger, errs := ValidateAndBuildLedger(bundle, mapping, nil, events)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got: %v", errs)
	}
	if len(ledger) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger))
	}
	if ledger[0].ENLines != 1 || ledger[1].ENLines != 2 {
		t.Errorf("line counts mismatch: g0=%d, g1=%d", ledger[0].ENLines, ledger[1].ENLines)
	}
}

// Positive Test 2: Real live sample validation if opted in via environment variable.
func TestLedgerValidation_PositiveRealSample(t *testing.T) {
	sampleDir := os.Getenv("FUSIONN_VERIFY_SAMPLE_DIR")
	if sampleDir == "" {
		sampleDir = os.Getenv("FUSIONN_TEST_SAMPLE_DIR")
	}
	if sampleDir == "" {
		t.Skip("skipping real sample test: opt in via FUSIONN_VERIFY_SAMPLE_DIR or FUSIONN_TEST_SAMPLE_DIR")
	}
	if _, err := os.Stat(filepath.Join(sampleDir, "sources.json")); err != nil {
		t.Skipf("sample directory %s not present or missing sources.json, skipping", sampleDir)
	}

	rawBundle, err := os.ReadFile(filepath.Join(sampleDir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle subtitlellm.Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatal(err)
	}

	rawMapping, err := os.ReadFile(filepath.Join(sampleDir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mapping subtitlellm.Mapping
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		t.Fatal(err)
	}

	var audit AuditData
	if rawAudit, err := os.ReadFile(filepath.Join(sampleDir, "alignment", "audit.json")); err == nil {
		_ = json.Unmarshal(rawAudit, &audit)
	}

	valPath := filepath.Join(sampleDir, "validation.json")
	rawVal, err := os.ReadFile(valPath)
	if err != nil {
		t.Fatal(err)
	}
	var valReport struct {
		EventAudit []RenderedEvent `json:"event_audit"`
	}
	if err := json.Unmarshal(rawVal, &valReport); err != nil {
		t.Fatal(err)
	}

	ledger, errs := ValidateAndBuildLedger(bundle, mapping, &audit, valReport.EventAudit)
	if len(errs) != 0 {
		t.Fatalf("real sample ledger validation failed: %v", errs)
	}
	if len(ledger) != len(mapping.Groups) {
		t.Errorf("expected %d groups, got %d", len(mapping.Groups), len(ledger))
	}
	t.Logf("real sample validated: %d groups, %d rendered events", len(ledger), len(valReport.EventAudit))
}

func TestParseDialogueEventsFromASS_TDD(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()

	sampleASS := `[Script Info]
Title: Test
ScriptType: v4.00+
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,PingFang SC,80,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,1.4,1.5,2,50,50,19,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.05,Default,,0,0,0,,{\an2}Hello world\N你好世界这是一段非常长的中文测试内容用于检查UTF8安全截断
Dialogue: 0,0:00:02.50,0:00:03.50,Default,,0,0,0,,{\an2}Second line\nwith break\N第二行含有换行\n以及竖线|测试
`

	events, err := ParseDialogueEventsFromASS(sampleASS, bundle, mapping)
	if err != nil {
		t.Fatalf("ParseDialogueEventsFromASS failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 parsed events, got %d", len(events))
	}

	if events[0].GroupIndex != 0 || events[0].StartMS != 1000 || events[0].EndMS != 2050 || events[0].Alignment != 2 {
		t.Errorf("unexpected event 0: %+v", events[0])
	}
	if len(events[0].ENIDs) != 1 || events[0].ENIDs[0] != 1 || len(events[0].ZHIDs) != 1 || events[0].ZHIDs[0] != 1 {
		t.Errorf("unexpected IDs for event 0: EN=%v ZH=%v", events[0].ENIDs, events[0].ZHIDs)
	}

	if events[1].GroupIndex != 1 || events[1].StartMS != 2500 || events[1].EndMS != 3500 {
		t.Errorf("unexpected event 1: %+v", events[1])
	}

	// ValidateAndBuildLedger should succeed with these events
	ledger, errs := ValidateAndBuildLedger(bundle, mapping, nil, events)
	if len(errs) != 0 {
		t.Fatalf("ValidateAndBuildLedger with parsed ASS events failed: %v", errs)
	}
	if len(ledger) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger))
	}
}

func TestSemanticInheritance_TDD(t *testing.T) {
	bundle := makeTestBundle()
	mapping := makeTestMapping()
	events := makeTestEvents()

	ledger, errs := ValidateAndBuildLedger(bundle, mapping, nil, events)
	if len(errs) != 0 {
		t.Fatalf("build ledger: %v", errs)
	}

	fingerprint := ComputeBundleFingerprint(bundle)
	if fingerprint == "" {
		t.Fatalf("ComputeBundleFingerprint returned empty")
	}

	// Create temporary review file
	reviewData := []map[string]interface{}{
		{
			"fingerprint": fingerprint,
			"en_ids":      []int{1},
			"zh_ids":      []int{1},
			"en_text":     "Hello world",
			"zh_text":     "你好世界这是一段非常长的中文测试内容用于检查UTF8安全截断",
			"verdict":     "acceptable",
			"note":        "verified in prior baseline",
		},
		{
			"fingerprint": fingerprint,
			"en_ids":      []int{2},
			"zh_ids":      []int{99}, // Mismatched ZH ID
			"en_text":     "Second line\nwith break",
			"zh_text":     "第二行含有换行\n以及竖线|测试",
			"verdict":     "acceptable",
		},
	}
	rawReview, err := json.Marshal(reviewData)
	if err != nil {
		t.Fatal(err)
	}
	tmpReviewFile := filepath.Join(t.TempDir(), "semantic-review.json")
	if err := os.WriteFile(tmpReviewFile, rawReview, 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := InheritSemanticReviews(ledger, bundle, []string{tmpReviewFile})
	if err != nil {
		t.Fatalf("InheritSemanticReviews failed: %v", err)
	}

	if stats.InheritedCount != 1 {
		t.Errorf("expected 1 inherited review, got %d", stats.InheritedCount)
	}
	if stats.UnreviewedCount != 1 {
		t.Errorf("expected 1 unreviewed, got %d", stats.UnreviewedCount)
	}

	if ledger[0].SemanticVerdict != "acceptable" {
		t.Errorf("expected group 0 verdict 'acceptable', got %q", ledger[0].SemanticVerdict)
	}
	if ledger[1].SemanticVerdict != "unreviewed" {
		t.Errorf("expected group 1 verdict 'unreviewed', got %q", ledger[1].SemanticVerdict)
	}
}

func TestVerifyActualASSAgainstExpected_TDD(t *testing.T) {
	sampleASS := `[Script Info]
Title: Test
ScriptType: v4.00+
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,PingFang SC,80,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,1.4,1.5,2,50,50,19,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\an2}Line 1
Dialogue: 0,0:00:02.50,0:00:03.50,Default,,0,0,0,,{\an2}Line 2
`

	// Exact match passes
	failures, err := VerifyActualASSAgainstExpected([]byte(sampleASS), []byte(sampleASS))
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if len(failures) != 0 {
		t.Errorf("expected 0 failures for exact match, got: %v", failures)
	}

	// Mismatched dialogue count fails
	mismatchedASS := sampleASS + "Dialogue: 0,0:00:04.00,0:00:05.00,Default,,0,0,0,,{\\an2}Extra Line\n"
	failuresMismatch, errM := VerifyActualASSAgainstExpected([]byte(sampleASS), []byte(mismatchedASS))
	if errM != nil {
		t.Fatalf("verify mismatch error: %v", errM)
	}
	if len(failuresMismatch) == 0 {
		t.Errorf("expected failures for count mismatch, got 0")
	}
}

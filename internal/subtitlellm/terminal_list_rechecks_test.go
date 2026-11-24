package subtitlellm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

type terminalListExpectedEvent struct {
	episode string
	group   int
	start   string
	end     string
	style   string
	rows    string
}

func parseTerminalListTSV(t *testing.T, path string) []terminalListExpectedEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tsv %q: %v", path, err)
	}
	defer f.Close()

	var rows []terminalListExpectedEvent
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if lineNum == 1 {
			if line != "episode\tgroup\tstart\tend\tstyle\trows" {
				t.Fatalf("%s: unexpected TSV header %q", path, line)
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("%s:%d: expected exactly 6 columns, got %d: %q", path, lineNum, len(cols), line)
		}
		gid, err := strconv.Atoi(cols[1])
		if err != nil {
			t.Fatalf("%s:%d: invalid group id %q: %v", path, lineNum, cols[1], err)
		}
		rows = append(rows, terminalListExpectedEvent{
			episode: cols[0],
			group:   gid,
			start:   cols[2],
			end:     cols[3],
			style:   cols[4],
			rows:    cols[5],
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %q: %v", path, err)
	}
	return rows
}

func loadTerminalListSlice(t *testing.T, dir, filename string) (string, []int, Bundle, Mapping, WordTimings) {
	t.Helper()
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read slice fixture %q: %v", path, err)
	}

	var raw struct {
		Episode  string      `json:"episode"`
		GroupIDs []int       `json:"group_ids"`
		Bundle   Bundle      `json:"bundle"`
		Mapping  Mapping     `json:"mapping"`
		Words    WordTimings `json:"words"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal slice fixture %q: %v", path, err)
	}

	raw.Bundle.Version = Version
	digest, err := bundleDigest(raw.Bundle)
	if err != nil {
		t.Fatalf("compute bundle digest for %s: %v", raw.Episode, err)
	}
	raw.Bundle.Fingerprint = digest

	raw.Mapping.Version = Version
	raw.Mapping.SourceFingerprint = digest
	md, err := mappingDigest(raw.Mapping)
	if err != nil {
		t.Fatalf("compute mapping digest for %s: %v", raw.Episode, err)
	}
	raw.Mapping.Fingerprint = md

	return raw.Episode, raw.GroupIDs, raw.Bundle, raw.Mapping, raw.Words
}

func terminalListTestConfig() (config.LLMConfig, config.ASSStyleConfig) {
	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
	style := config.ASSStyleConfig{
		Enabled:         true,
		PlayResX:        1920,
		PlayResY:        1080,
		PrimaryFont:     "PingFang SC",
		PrimarySize:     80,
		PrimaryColor:    "&H08EFECEA",
		SecondaryFont:   "Helvetica Neue",
		SecondarySize:   49,
		SecondaryColor:  "&H087BC0E5",
		Bold:            false,
		Outline:         1.9,
		OutlineColor:    "&H30141414",
		Shadow:          1.9,
		BackColor:       "&H80000000",
		MarginV:         19,
		MarginLeft:      50,
		MarginRight:     50,
		Alignment:       2,
		BorderStyle:     1,
		WrapStyle:       "0",
		ScaleX:          100,
		Spacing:         0,
		LineHeightRatio: 1.23,
		BlockPadding:    16,
		CollisionGap:    12,
	}
	return cfg, style
}

func renderTerminalListSlice(
	t *testing.T,
	ep string,
	bundle Bundle,
	mapping Mapping,
	words WordTimings,
	groupIDs []int,
	llmCfg config.LLMConfig,
	styleCfg config.ASSStyleConfig,
) []terminalListExpectedEvent {
	t.Helper()
	rendered, events, _, err := renderPlanWithAudits(bundle, mapping, llmCfg, styleCfg, 3840, 1632, words)
	if err != nil {
		t.Fatalf("%s renderPlanWithAudits failed: %v", ep, err)
	}

	dialogues := parseDialogues(t, string(rendered))
	if len(dialogues) != len(events) {
		t.Fatalf("%s dialogues count mismatch with internal events: %d != %d", ep, len(dialogues), len(events))
	}

	var out []terminalListExpectedEvent
	for i, e := range events {
		if e.group < 0 || e.group >= len(groupIDs) {
			t.Fatalf("%s event %d has invalid group index %d", ep, i, e.group)
		}
		origGid := groupIDs[e.group]
		out = append(out, terminalListExpectedEvent{
			episode: ep,
			group:   origGid,
			start:   assTime(dialogues[i].start),
			end:     assTime(dialogues[i].end),
			style:   dialogues[i].style,
			rows:    dialogues[i].text,
		})
	}
	return out
}

func compareTerminalListEvents(t *testing.T, label string, got, want []terminalListExpectedEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s event count mismatch: got %d, want %d", label, len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.episode != w.episode || g.group != w.group || g.start != w.start || g.end != w.end || g.style != w.style || g.rows != w.rows {
			t.Errorf("%s event [%d] mismatch:\n  got:  %s group %d %s -> %s [%s] %q\n  want: %s group %d %s -> %s [%s] %q",
				label, i, g.episode, g.group, g.start, g.end, g.style, g.rows,
				w.episode, w.group, w.start, w.end, w.style, w.rows)
		}
	}
}

func TestTerminalListRecheckCases(t *testing.T) {
	prior := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		return 0, false
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = prior })

	testdataDir := filepath.Join("testdata", "terminal-list-rechecks")
	expectedEvents := parseTerminalListTSV(t, filepath.Join(testdataDir, "expected.tsv"))
	fallbackEvents := parseTerminalListTSV(t, filepath.Join(testdataDir, "fallback.tsv"))

	llmCfg, styleCfg := terminalListTestConfig()

	// 1. Replay recorded-success cases across both episodes (20 distinct groups total)
	for _, epFilename := range []string{"s02e05.json", "s02e06.json"} {
		ep, groupIDs, bundle, mapping, words := loadTerminalListSlice(t, testdataDir, epFilename)

		// Validate fixture cue indices and check that isAmbiguousGroup agrees with presence of semantic rechecks
		enIndex, zhIndex, err := cueIndex(bundle)
		if err != nil {
			t.Fatalf("%s cueIndex validation failed: %v", ep, err)
		}
		rechecksByKey := make(map[string]bool)
		for _, r := range mapping.SemanticRechecks {
			rechecksByKey[recheckGroupKey(r.ENIDs, r.ZHIDs)] = true
		}
		for i, g := range mapping.Groups {
			gid := groupIDs[i]
			wantAmbig := rechecksByKey[recheckGroupKey(g.ENIDs, g.ZHIDs)]
			gotAmbig := isAmbiguousGroup(g, enIndex, zhIndex)
			if gotAmbig != wantAmbig {
				t.Errorf("%s group %d isAmbiguousGroup=%v, want %v", ep, gid, gotAmbig, wantAmbig)
			}
		}

		gotEvents := renderTerminalListSlice(t, ep, bundle, mapping, words, groupIDs, llmCfg, styleCfg)

		// Filter out E06/478 (covered under fallback mode)
		var regularGot []terminalListExpectedEvent
		for _, e := range gotEvents {
			if ep == "S02E06" && e.group == 478 {
				continue
			}
			regularGot = append(regularGot, e)
		}

		var epExpected []terminalListExpectedEvent
		for _, exp := range expectedEvents {
			if exp.episode == ep {
				epExpected = append(epExpected, exp)
			}
		}

		// Compare complete episode-slice events in its own subtest so individual group diagnostics still run on count changes
		t.Run(ep+"/complete", func(t *testing.T) {
			compareTerminalListEvents(t, ep+"/complete", regularGot, epExpected)
		})

		// Case-specific subtests for failure localization
		for _, gid := range groupIDs {
			if ep == "S02E06" && gid == 478 {
				continue
			}
			targetGid := gid
			t.Run(fmt.Sprintf("%s/group_%d", ep, targetGid), func(t *testing.T) {
				var groupGot []terminalListExpectedEvent
				for _, e := range regularGot {
					if e.group == targetGid {
						groupGot = append(groupGot, e)
					}
				}
				var groupWant []terminalListExpectedEvent
				for _, e := range epExpected {
					if e.group == targetGid {
						groupWant = append(groupWant, e)
					}
				}
				compareTerminalListEvents(t, fmt.Sprintf("%s/group_%d", ep, targetGid), groupGot, groupWant)
			})
		}
	}

	// 2. Replay Uncertain fallback cases (E05 group 432 and E06 group 478 in intact episode slices)
	fallbackCases := []struct {
		episode  string
		filename string
		groupID  int
	}{
		{episode: "S02E05", filename: "s02e05.json", groupID: 432},
		{episode: "S02E06", filename: "s02e06.json", groupID: 478},
	}

	for _, fc := range fallbackCases {
		spec := fc
		t.Run(fmt.Sprintf("Fallback/%s_group_%d", spec.episode, spec.groupID), func(t *testing.T) {
			ep, groupIDs, bundle, mapping, words := loadTerminalListSlice(t, testdataDir, spec.filename)
			idx := -1
			for i, gid := range groupIDs {
				if gid == spec.groupID {
					idx = i
					break
				}
			}
			if idx < 0 {
				t.Fatalf("group %d not found in %s", spec.groupID, spec.filename)
			}
			targetGroup := mapping.Groups[idx]

			// Force Uncertain: true without Relations, leaving all other mapping context intact
			for i, r := range mapping.SemanticRechecks {
				if fmt.Sprintf("%v", r.ENIDs) == fmt.Sprintf("%v", targetGroup.ENIDs) {
					mapping.SemanticRechecks[i] = SemanticRecheck{
						ENIDs:     r.ENIDs,
						ZHIDs:     r.ZHIDs,
						Uncertain: true,
					}
				}
			}

			gotEvents := renderTerminalListSlice(t, ep, bundle, mapping, words, groupIDs, llmCfg, styleCfg)

			var fallbackGot []terminalListExpectedEvent
			for _, e := range gotEvents {
				if e.group == spec.groupID {
					fallbackGot = append(fallbackGot, e)
				}
			}

			var fallbackWant []terminalListExpectedEvent
			for _, exp := range fallbackEvents {
				if exp.episode == spec.episode && exp.group == spec.groupID {
					fallbackWant = append(fallbackWant, exp)
				}
			}

			compareTerminalListEvents(t, fmt.Sprintf("Fallback/%s_group_%d", spec.episode, spec.groupID), fallbackGot, fallbackWant)
		})
	}
}

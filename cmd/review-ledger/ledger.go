package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fusionn/internal/subtitlellm"
)

type RenderedEvent struct {
	GroupIndex int    `json:"group"`
	Strategy   string `json:"strategy"`
	ENIDs      []int  `json:"en_ids"`
	ZHIDs      []int  `json:"zh_ids"`
	StartMS    int    `json:"start_ms"`
	EndMS      int    `json:"end_ms"`
	Alignment  int    `json:"alignment"`
	Top        bool   `json:"top"`
	ENText     string `json:"en_text"`
	ZHText     string `json:"zh_text"`
	ENLines    int    `json:"en_lines"`
	ZHLines    int    `json:"zh_lines"`
	ZHMerged   bool   `json:"zh_merged"`
	ENMerged   bool   `json:"en_merged"`
}

type LedgerEntry struct {
	GroupID        int           `json:"group_id"`
	ENIDs          []int         `json:"en_ids"`
	ZHIDs          []int         `json:"zh_ids"`
	ENText         string        `json:"en_text"`
	ZHText         string        `json:"zh_text"`
	ENLines        int           `json:"en_lines"`
	ZHLines        int           `json:"zh_lines"`
	ENStartMS      int           `json:"en_start_ms"`
	ENEndMS        int           `json:"en_end_ms"`
	ZHStartMS      int           `json:"zh_start_ms"`
	ZHEndMS        int           `json:"zh_end_ms"`
	SourceUnionMS  [2]int        `json:"source_union_ms"`
	NativeTop      bool          `json:"native_top"`
	ModelKind      string        `json:"model_kind"`
	ModelLayer     string        `json:"model_layer"`
	Origin         string        `json:"origin"`
	RenderedEvents []LedgerEvent `json:"rendered_events"`
	CandidateFlags []string      `json:"candidate_flags"`
}

type LedgerEvent struct {
	EventIndex int    `json:"event_index"`
	StartMS    int    `json:"start_ms"`
	EndMS      int    `json:"end_ms"`
	Alignment  int    `json:"alignment"`
	Position   string `json:"position"`
	Strategy   string `json:"strategy"`
	ENText     string `json:"en_text"`
	ZHText     string `json:"zh_text"`
}

type AuditData struct {
	Restored struct {
		EN []int `json:"en"`
		ZH []int `json:"zh"`
	} `json:"restored"`
	Conflicts []struct {
		ENIDs []int `json:"en_ids"`
		ZHIDs []int `json:"zh_ids"`
	} `json:"conflicts"`
}

func countEffectiveLines(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	parts := strings.Split(text, "\n")
	count := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func sanitizeMarkdownCell(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return truncateRunes(s, maxRunes)
}

func ValidateAndBuildLedger(bundle subtitlellm.Bundle, mapping subtitlellm.Mapping, audit *AuditData, events []RenderedEvent) ([]LedgerEntry, []string) {
	var errs []string

	restoredENMap := make(map[int]bool)
	restoredZHMap := make(map[int]bool)
	if audit != nil {
		for _, id := range audit.Restored.EN {
			restoredENMap[id] = true
		}
		for _, id := range audit.Restored.ZH {
			restoredZHMap[id] = true
		}
	}

	enMap := make(map[int]subtitlellm.Cue)
	for _, c := range bundle.EN {
		enMap[c.ID] = c
	}
	zhMap := make(map[int]subtitlellm.Cue)
	for _, c := range bundle.ZH {
		zhMap[c.ID] = c
	}

	// Group to rendered events mapping
	eventsByGroup := make(map[int][]LedgerEvent)
	for idx, ev := range events {
		if ev.GroupIndex < 0 || ev.GroupIndex >= len(mapping.Groups) {
			errs = append(errs, fmt.Sprintf("untraceable event %d references non-existent group %d", idx, ev.GroupIndex))
			continue
		}

		grp := mapping.Groups[ev.GroupIndex]

		// Check foreign cues in event
		grpENMap := make(map[int]bool)
		for _, id := range grp.ENIDs {
			grpENMap[id] = true
		}
		for _, id := range ev.ENIDs {
			if _, exists := enMap[id]; !exists || !grpENMap[id] {
				errs = append(errs, fmt.Sprintf("event %d references foreign EN cue ID %d", idx, id))
			}
		}

		grpZHMap := make(map[int]bool)
		for _, id := range grp.ZHIDs {
			grpZHMap[id] = true
		}
		for _, id := range ev.ZHIDs {
			if _, exists := zhMap[id]; !exists || !grpZHMap[id] {
				errs = append(errs, fmt.Sprintf("event %d references foreign ZH cue ID %d", idx, id))
			}
		}

		pos := "bottom"
		if ev.Alignment >= 7 || ev.Top {
			pos = "top"
		}
		eventsByGroup[ev.GroupIndex] = append(eventsByGroup[ev.GroupIndex], LedgerEvent{
			EventIndex: idx,
			StartMS:    ev.StartMS,
			EndMS:      ev.EndMS,
			Alignment:  ev.Alignment,
			Position:   pos,
			Strategy:   ev.Strategy,
			ENText:     ev.ENText,
			ZHText:     ev.ZHText,
		})
	}

	var ledger []LedgerEntry

	for gIdx, grp := range mapping.Groups {
		rendEvents, hasEvents := eventsByGroup[gIdx]
		if !hasEvents && (len(grp.ENIDs) > 0 || len(grp.ZHIDs) > 0) {
			errs = append(errs, fmt.Sprintf("missing rendered event for group %d (EN:%v ZH:%v)", gIdx, grp.ENIDs, grp.ZHIDs))
		}

		var enTexts []string
		enStart, enEnd := -1, -1
		nativeTop := false
		for _, id := range grp.ENIDs {
			if cue, ok := enMap[id]; ok {
				enTexts = append(enTexts, cue.Plain)
				if enStart == -1 || cue.Start < enStart {
					enStart = cue.Start
				}
				if cue.End > enEnd {
					enEnd = cue.End
				}
				if cue.Top {
					nativeTop = true
				}
			} else {
				errs = append(errs, fmt.Sprintf("group %d references non-existent EN cue ID %d", gIdx, id))
			}
		}

		var zhTexts []string
		zhStart, zhEnd := -1, -1
		for _, id := range grp.ZHIDs {
			if cue, ok := zhMap[id]; ok {
				zhTexts = append(zhTexts, cue.Plain)
				if zhStart == -1 || cue.Start < zhStart {
					zhStart = cue.Start
				}
				if cue.End > zhEnd {
					zhEnd = cue.End
				}
				if cue.Top {
					nativeTop = true
				}
			} else {
				errs = append(errs, fmt.Sprintf("group %d references non-existent ZH cue ID %d", gIdx, id))
			}
		}

		unionStart := enStart
		if unionStart == -1 || (zhStart != -1 && zhStart < unionStart) {
			unionStart = zhStart
		}
		unionEnd := enEnd
		if unionEnd == -1 || (zhEnd != -1 && zhEnd > unionEnd) {
			unionEnd = zhEnd
		}

		combinedENText := strings.Join(enTexts, " ")
		combinedZHText := strings.Join(zhTexts, " ")

		// Candidate anomaly flags
		var flags []string
		if len(grp.ENIDs) == 0 && len(grp.ZHIDs) > 0 {
			flags = append(flags, "one_sided_zh")
		} else if len(grp.ZHIDs) == 0 && len(grp.ENIDs) > 0 {
			flags = append(flags, "one_sided_en")
		} else if len(grp.ENIDs) > 1 && len(grp.ZHIDs) == 1 {
			flags = append(flags, "many_to_one")
		} else if len(grp.ENIDs) == 1 && len(grp.ZHIDs) > 1 {
			flags = append(flags, "one_to_many")
		} else if len(grp.ENIDs) > 1 && len(grp.ZHIDs) > 1 {
			flags = append(flags, "many_to_many")
		}

		if enStart != -1 && zhStart != -1 {
			diff := enStart - zhStart
			if diff < -2000 || diff > 2000 {
				flags = append(flags, fmt.Sprintf("time_skew_%dms", diff))
			}
		}

		origin := grp.Origin
		if origin == "" {
			origin = "model"
		}
		if origin != "model" {
			flags = append(flags, "non_model_origin:"+origin)
		}
		for _, id := range grp.ENIDs {
			if restoredENMap[id] {
				flags = append(flags, fmt.Sprintf("audit_restored_en_%d", id))
			}
		}
		for _, id := range grp.ZHIDs {
			if restoredZHMap[id] {
				flags = append(flags, fmt.Sprintf("audit_restored_zh_%d", id))
			}
		}

		if grp.Kind == "" {
			flags = append(flags, "missing_kind")
		}

		if nativeTop {
			renderedBottom := false
			for _, re := range rendEvents {
				if re.Position == "bottom" {
					renderedBottom = true
					break
				}
			}
			if renderedBottom {
				flags = append(flags, "native_top_rendered_bottom")
			}
		}

		ledger = append(ledger, LedgerEntry{
			GroupID:        gIdx,
			ENIDs:          grp.ENIDs,
			ZHIDs:          grp.ZHIDs,
			ENText:         combinedENText,
			ZHText:         combinedZHText,
			ENLines:        countEffectiveLines(combinedENText),
			ZHLines:        countEffectiveLines(combinedZHText),
			ENStartMS:      enStart,
			ENEndMS:        enEnd,
			ZHStartMS:      zhStart,
			ZHEndMS:        zhEnd,
			SourceUnionMS:  [2]int{unionStart, unionEnd},
			NativeTop:      nativeTop,
			ModelKind:      grp.Kind,
			ModelLayer:     grp.Layer,
			Origin:         origin,
			RenderedEvents: rendEvents,
			CandidateFlags: flags,
		})
	}

	return ledger, errs
}

func main() {
	dir := flag.String("dir", "", "sample directory")
	outMD := flag.String("out-md", "", "markdown ledger output path")
	outJSON := flag.String("out-json", "", "json ledger output path")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "Usage: review-ledger -dir <sample-dir> [-out-md <path>] [-out-json <path>]")
		os.Exit(1)
	}

	var bundle subtitlellm.Bundle
	sourcesPath := filepath.Join(*dir, "sources.json")
	if rawBundle, err := os.ReadFile(sourcesPath); err == nil {
		if err := json.Unmarshal(rawBundle, &bundle); err != nil {
			fmt.Fprintf(os.Stderr, "unmarshal sources.json: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Fallback to en.source.srt and zh.source.srt
		enPath := filepath.Join(*dir, "en.source.srt")
		zhPath := filepath.Join(*dir, "zh.source.srt")
		loaded, errLoad := subtitlellm.LoadSources(enPath, zhPath)
		if errLoad != nil {
			fmt.Fprintf(os.Stderr, "could not load sources.json or (.srt files): %v\n", errLoad)
			os.Exit(1)
		}
		bundle = loaded
	}

	mappingPath := filepath.Join(*dir, "alignment", "mapping.json")
	if _, err := os.Stat(mappingPath); os.IsNotExist(err) {
		// Try mapping-recomputed.json or mapping.json directly in dir
		if _, err2 := os.Stat(filepath.Join(*dir, "mapping-recomputed.json")); err2 == nil {
			mappingPath = filepath.Join(*dir, "mapping-recomputed.json")
		} else if _, err3 := os.Stat(filepath.Join(*dir, "mapping.json")); err3 == nil {
			mappingPath = filepath.Join(*dir, "mapping.json")
		}
	}
	rawMapping, err := os.ReadFile(mappingPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read mapping: %v\n", err)
		os.Exit(1)
	}
	var mapping subtitlellm.Mapping
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		fmt.Fprintf(os.Stderr, "unmarshal mapping: %v\n", err)
		os.Exit(1)
	}

	var audit AuditData
	if rawAudit, err := os.ReadFile(filepath.Join(*dir, "alignment", "audit.json")); err == nil {
		_ = json.Unmarshal(rawAudit, &audit)
	} else if rawAudit, err := os.ReadFile(filepath.Join(*dir, "audit-recomputed.json")); err == nil {
		_ = json.Unmarshal(rawAudit, &audit)
	}

	var events []RenderedEvent
	valPath := filepath.Join(*dir, "validation.json")
	if rawVal, err := os.ReadFile(valPath); err == nil {
		var valReport struct {
			EventAudit []RenderedEvent `json:"event_audit"`
		}
		if err := json.Unmarshal(rawVal, &valReport); err == nil {
			events = valReport.EventAudit
		}
	}

	ledger, errs := ValidateAndBuildLedger(bundle, mapping, &audit, events)
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "Validation failed with %d errors:\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		os.Exit(2)
	}

	fmt.Printf("Ledger validation passed: %d groups, %d rendered events\n", len(ledger), len(events))

	if *outJSON != "" {
		blob, err := json.MarshalIndent(ledger, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal ledger: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outJSON, blob, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write ledger json: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote JSON ledger to %s\n", *outJSON)
	}

	if *outMD != "" {
		var sb strings.Builder
		sb.WriteString("# 逐组审查紧凑账目 (Review Ledger)\n\n")
		sb.WriteString("注：本账目连接原始模型分类、映射组与实际 ASS 渲染事件，供独立全量语义审查；不作语义通过特判。\n\n")
		sb.WriteString("| 组ID | 源EN:ZH | 原文(分行数) | 原时间并集 | 模型kind/layer | 实际输出事件(位置/样式) | 审查候选标记 |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

		for _, e := range ledger {
			srcCues := fmt.Sprintf("EN%v : ZH%v", e.ENIDs, e.ZHIDs)
			enTxtClean := sanitizeMarkdownCell(e.ENText, 40)
			zhTxtClean := sanitizeMarkdownCell(e.ZHText, 40)
			texts := fmt.Sprintf("EN(%d): %s<br>ZH(%d): %s", e.ENLines, enTxtClean, e.ZHLines, zhTxtClean)
			timeRange := fmt.Sprintf("%d-%d ms", e.SourceUnionMS[0], e.SourceUnionMS[1])
			modelMeta := fmt.Sprintf("%s / %s (%s)", e.ModelKind, e.ModelLayer, e.Origin)

			var outEvents []string
			for _, re := range e.RenderedEvents {
				outEvents = append(outEvents, fmt.Sprintf("#%d %s \\an%d [%d-%d]", re.EventIndex, re.Position, re.Alignment, re.StartMS, re.EndMS))
			}
			outStr := strings.Join(outEvents, "<br>")
			if outStr == "" {
				outStr = "*(无输出)*"
			}

			flagsStr := strings.Join(e.CandidateFlags, ", ")
			if flagsStr == "" {
				flagsStr = "-"
			}

			fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s | %s | %s |\n",
				e.GroupID, srcCues, texts, timeRange, modelMeta, outStr, flagsStr)
		}

		if err := os.WriteFile(*outMD, []byte(sb.String()), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write ledger md: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote Markdown ledger to %s\n", *outMD)
	}
}

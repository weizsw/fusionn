package service

import (
	"sort"
	"time"

	"github.com/asticode/go-astisub"
)

type Algo interface {
	MatchSubtitleCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item
	MatchSubtitleSegment(chineseItems, englishItems []*astisub.Item) []*astisub.Item
}

type algo struct{}

func NewAlgo() *algo {
	return &algo{}
}

func overlapDuration(start1, end1, start2, end2 time.Duration) time.Duration {
	if start1 < start2 {
		start1 = start2
	}
	if end1 > end2 {
		end1 = end2
	}
	if start1 < end1 {
		return end1 - start1
	}
	return 0
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (a *algo) MatchSubtitleCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item {
	var mergedItems []*astisub.Item

	// Sort both slices by StartAt time
	sort.Slice(chineseItems, func(i, j int) bool {
		return chineseItems[i].StartAt < chineseItems[j].StartAt
	})
	sort.Slice(englishItems, func(i, j int) bool {
		return englishItems[i].StartAt < englishItems[j].StartAt
	})

	i, j := 0, 0
	for i < len(chineseItems) && j < len(englishItems) {
		if abs(chineseItems[i].StartAt-englishItems[j].StartAt) <= timeTolerance ||
			abs(chineseItems[i].EndAt-englishItems[j].EndAt) <= timeTolerance {
			// Merge the subtitles
			merged := mergeItems(chineseItems[i], englishItems[j])
			mergedItems = append(mergedItems, merged...)
			i++
			j++
		} else if chineseItems[i].StartAt < englishItems[j].StartAt {
			mergedItems = append(mergedItems, chineseItems[i])
			i++
		} else {
			mergedItems = append(mergedItems, englishItems[j])
			j++
		}
	}

	// Append any remaining items
	mergedItems = append(mergedItems, chineseItems[i:]...)
	mergedItems = append(mergedItems, englishItems[j:]...)

	return mergedItems
}

func mergeItems(chinese, english *astisub.Item) []*astisub.Item {
	// Create a copy of the English item
	eng := *chinese
	eng.Style = &astisub.Style{
		ID: "Eng",
	}
	// Clear the Lines slice of the English copy
	eng.Lines = nil

	// Copy English lines to eng, setting the SSAFontName to "Eng"
	for _, englishLine := range english.Lines {
		newLine := astisub.Line{
			Items: make([]astisub.LineItem, len(englishLine.Items)),
		}
		for i, item := range englishLine.Items {
			newLine.Items[i] = item
		}
		eng.Lines = append(eng.Lines, newLine)
	}
	return []*astisub.Item{&eng, chinese}
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (a *algo) MatchSubtitleSegment(chineseItems, englishItems []*astisub.Item) []*astisub.Item {
	// Create time segments from both Chinese and English subtitles
	type TimeSegment struct {
		Start time.Duration
		End   time.Duration
	}
	var segments []TimeSegment

	// Collect all time segments
	for _, item := range append(chineseItems, englishItems...) {
		segments = append(segments, TimeSegment{
			Start: item.StartAt,
			End:   item.EndAt,
		})
	}

	// Sort and merge overlapping segments
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].Start == segments[j].Start {
			return segments[i].End < segments[j].End
		}
		return segments[i].Start < segments[j].Start
	})

	var mergedSegments []TimeSegment
	for _, seg := range segments {
		if len(mergedSegments) == 0 || mergedSegments[len(mergedSegments)-1].End <= seg.Start {
			mergedSegments = append(mergedSegments, seg)
		} else {
			last := len(mergedSegments) - 1
			if seg.End > mergedSegments[last].End {
				mergedSegments[last].End = seg.End
			}
		}
	}

	// Create merged items based on time segments
	var mergedItems []*astisub.Item
	for _, seg := range mergedSegments {
		var engLines []astisub.Line
		var chiLines []astisub.Line

		// Collect English lines that overlap with this segment
		for _, eng := range englishItems {
			if eng.EndAt > seg.Start && eng.StartAt < seg.End {
				engLines = append(engLines, eng.Lines...)
			}
		}

		// Collect Chinese lines that overlap with this segment
		for _, ch := range chineseItems {
			if ch.EndAt > seg.Start && ch.StartAt < seg.End {
				chiLines = append(chiLines, ch.Lines...)
			}
		}

		if len(engLines) > 0 || len(chiLines) > 0 {
			// Create English item
			if len(engLines) > 0 {
				engItem := &astisub.Item{
					StartAt: seg.Start,
					EndAt:   seg.End,
					Lines:   engLines,
					Style: &astisub.Style{
						ID: "Eng",
					},
				}
				mergedItems = append(mergedItems, engItem)
			}

			// Create Chinese item
			if len(chiLines) > 0 {
				chiItem := &astisub.Item{
					StartAt: seg.Start,
					EndAt:   seg.End,
					Lines:   chiLines,
					Style: &astisub.Style{
						ID: "Default",
					},
				}
				mergedItems = append(mergedItems, chiItem)
			}
		}
	}

	return mergedItems
}

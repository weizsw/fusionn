package repository

import (
	"sort"
	"time"

	"github.com/asticode/go-astisub"
)

type IAlgo interface {
	MatchSubtitlesCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item
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

func (a *algo) MatchSubtitlesCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item {
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
			mergedItem := mergeItems(chineseItems[i], englishItems[j])
			mergedItems = append(mergedItems, mergedItem)
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

func mergeItems(chinese, english *astisub.Item) *astisub.Item {
	merged := &astisub.Item{
		Index:   chinese.Index, // Or you could use a new index
		StartAt: chinese.StartAt,
		EndAt:   chinese.EndAt,
		Style:   chinese.Style, // You might want to decide which style to use
	}

	// Merge lines
	for _, chineseLine := range chinese.Lines {
		mergedLine := astisub.Line{VoiceName: chineseLine.VoiceName}
		for _, chineseLineItem := range chineseLine.Items {
			mergedLine.Items = append(mergedLine.Items, chineseLineItem)
		}
		merged.Lines = append(merged.Lines, mergedLine)
	}
	for _, englishLine := range english.Lines {
		mergedLine := astisub.Line{VoiceName: englishLine.VoiceName}
		for _, englishLineItem := range englishLine.Items {
			mergedLine.Items = append(mergedLine.Items, englishLineItem)
		}
		merged.Lines = append(merged.Lines, mergedLine)
	}

	return merged
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

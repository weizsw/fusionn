package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fusionn/internal/subtitlellm"
)

// SemanticReviewItem represents a review decision for an alignment entry.
type SemanticReviewItem struct {
	Fingerprint string `json:"fingerprint"`
	GroupID     int    `json:"group_id"`
	ENIDs       []int  `json:"en_ids"`
	ZHIDs       []int  `json:"zh_ids"`
	ENText      string `json:"en_text"`
	ZHText      string `json:"zh_text"`
	Verdict     string `json:"verdict"`
	Reason      string `json:"reason,omitempty"`
	Note        string `json:"note,omitempty"`
}

// SemanticInheritanceStats reports counts from review inheritance.
type SemanticInheritanceStats struct {
	TotalEntries    int
	InheritedCount  int
	UnreviewedCount int
	FlaggedCount    int
}

// equalIntSlicesStrict checks that two slices are identical in length and order.
// Crucially, it does NOT sort them!
func equalIntSlicesStrict(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isValidSemanticVerdict(v string) bool {
	switch v {
	case "acceptable", "confirmed-compatible", "legitimate-one-sided", "approved":
		return true
	default:
		return false
	}
}

// InheritSemanticReviews loads review items from files and strictly applies them to ledger entries.
func InheritSemanticReviews(ledger []LedgerEntry, bundle subtitlellm.Bundle, reviewFiles []string) (SemanticInheritanceStats, error) {
	stats := SemanticInheritanceStats{
		TotalEntries: len(ledger),
	}

	if bundle.Fingerprint == "" {
		return stats, fmt.Errorf("bundle fingerprint is missing")
	}

	var allItems []SemanticReviewItem
	for _, fPath := range reviewFiles {
		data, err := os.ReadFile(fPath)
		if err != nil {
			return stats, fmt.Errorf("read semantic review file %s: %w", fPath, err)
		}
		var items []SemanticReviewItem
		if err := json.Unmarshal(data, &items); err != nil {
			return stats, fmt.Errorf("unmarshal semantic review file %s: %w", fPath, err)
		}
		allItems = append(allItems, items...)
	}

	for i := range ledger {
		entry := &ledger[i]
		inherited := false

		for _, item := range allItems {
			// 1. Fingerprint must be present and match bundle.Fingerprint exactly.
			if item.Fingerprint == "" || item.Fingerprint != bundle.Fingerprint {
				continue
			}

			// 2. IDs must match in strict original order without sorting.
			if !equalIntSlicesStrict(entry.ENIDs, item.ENIDs) || !equalIntSlicesStrict(entry.ZHIDs, item.ZHIDs) {
				continue
			}

			// 3. Exact text check:
			// Non-empty tracks must have exact full original text (no trim, no normalization).
			// Empty tracks must have empty text ("").
			if len(entry.ENIDs) > 0 {
				if item.ENText == "" || item.ENText != entry.ENText {
					continue
				}
			} else {
				if item.ENText != "" {
					continue
				}
			}

			if len(entry.ZHIDs) > 0 {
				if item.ZHText == "" || item.ZHText != entry.ZHText {
					continue
				}
			} else {
				if item.ZHText != "" {
					continue
				}
			}

			// 4. Verdict check: must be a recognized non-empty verdict.
			if !isValidSemanticVerdict(item.Verdict) {
				continue
			}

			// Match found!
			reason := item.Reason
			if reason == "" {
				reason = item.Note
			}
			entry.SemanticVerdict = item.Verdict
			entry.SemanticNote = reason
			inherited = true
			break
		}

		if inherited {
			stats.InheritedCount++
		} else {
			if entry.SemanticVerdict == "" {
				entry.SemanticVerdict = "unreviewed"
			}
			stats.UnreviewedCount++
		}

		if len(entry.CandidateFlags) > 0 {
			stats.FlaggedCount++
		}
	}

	return stats, nil
}

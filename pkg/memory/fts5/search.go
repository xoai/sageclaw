package fts5

import (
	"math"
	"sort"
	"time"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

const (
	tagBoostFactor    = 0.15 // Up to 15% score increase for matching tags.
	recencyHalfLife   = 14.0 // Days — score halves every 14 days.
)

// filterByTags applies hard AND filtering: only memories with ALL filterTags.
func filterByTags(mems []sqlite.Memory, scores []float64, filterTags []string) ([]sqlite.Memory, []float64) {
	required := make(map[string]bool, len(filterTags))
	for _, t := range filterTags {
		required[t] = true
	}

	var filteredMems []sqlite.Memory
	var filteredScores []float64
	for i, m := range mems {
		if hasAllTags(m.Tags, required) {
			filteredMems = append(filteredMems, m)
			filteredScores = append(filteredScores, scores[i])
		}
	}
	return filteredMems, filteredScores
}

func hasAllTags(tags []string, required map[string]bool) bool {
	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	for t := range required {
		if !have[t] {
			return false
		}
	}
	return true
}

// applyTagBoost increases score for results matching soft-boost tags.
func applyTagBoost(score float64, memTags, boostTags []string) float64 {
	if len(boostTags) == 0 {
		return score
	}

	boostSet := make(map[string]bool, len(boostTags))
	for _, t := range boostTags {
		boostSet[t] = true
	}

	matches := 0
	for _, t := range memTags {
		if boostSet[t] {
			matches++
		}
	}
	if matches == 0 {
		return score
	}

	// Proportional boost: more tag matches = higher boost, up to tagBoostFactor.
	boostRatio := float64(matches) / float64(len(boostTags))
	return score * (1.0 + tagBoostFactor*boostRatio)
}

// applyRecencyDecay reduces score based on age with a 14-day half-life.
func applyRecencyDecay(score float64, updatedAt time.Time) float64 {
	days := time.Since(updatedAt).Hours() / 24.0
	if days <= 0 {
		return score
	}
	decay := math.Pow(0.5, days/recencyHalfLife)
	return score * decay
}

// sortByScore sorts entries by score descending.
func sortByScore(entries []memory.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})
}

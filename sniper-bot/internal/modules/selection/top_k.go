package selection

import (
	"fmt"
	"math"
	"sort"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

const RejectReasonBelowTopK = "below_top_k"

// BatchItem is one validated edge plus optional creator metadata for L6 dedup.
type BatchItem struct {
	Edge           contracts.ValidatedEdgeDTO
	CreatorAddress string
}

type rankedCandidate struct {
	item  BatchItem
	score float64
}

// PickTopK implements the greedy Top-K algorithm from docs/plans/2026-06-10-profit-restoration-plan.md §7.3.
// Returns one SelectionOutputDTO per input item (deterministic order matches inputs).
func PickTopK(
	items []BatchItem,
	openCount int,
	thresholds config.ModeThresholds,
	topK int,
	maxPerCreator int32,
	openByCreator map[string]int32,
) []contracts.SelectionOutputDTO {
	outputs := make([]contracts.SelectionOutputDTO, len(items))
	indexByEventID := make(map[string]int, len(items))
	for i, item := range items {
		outputs[i] = buildSelectionOutput(item.Edge, item.CreatorAddress, false, 0, 0, rejectForEdge(item.Edge))
		indexByEventID[item.Edge.EventID] = i
	}

	if topK <= 0 {
		topK = thresholds.MaxPositions
	}
	if topK <= 0 {
		topK = 1
	}

	if openCount >= thresholds.MaxPositions {
		for i := range outputs {
			if outputs[i].Selected {
				continue
			}
			if items[i].Edge.Decision == "ACCEPT" {
				outputs[i].RejectReason = fmt.Sprintf("max_open_positions_reached:%d", openCount)
			}
		}
		return outputs
	}

	slots := topK - openCount
	if slots > thresholds.MaxPositions-openCount {
		slots = thresholds.MaxPositions - openCount
	}
	if slots <= 0 {
		for i := range outputs {
			if items[i].Edge.Decision == "ACCEPT" && outputs[i].RejectReason == "" {
				outputs[i].RejectReason = fmt.Sprintf("max_open_positions_reached:%d", openCount)
			}
		}
		return outputs
	}

	var ranked []rankedCandidate
	for _, item := range items {
		if item.Edge.Decision != "ACCEPT" {
			continue
		}
		ranked = append(ranked, rankedCandidate{
			item:  item,
			score: combinedScore(item.Edge),
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].item.Edge.TokenAddress < ranked[j].item.Edge.TokenAddress
	})

	picked := make([]contracts.SelectionOutputDTO, 0, slots)
	pickedCreators := make(map[string]struct{})
	for _, cand := range ranked {
		if len(picked) >= slots {
			break
		}
		creator := cand.item.CreatorAddress
		if creator != "" {
			if _, dup := pickedCreators[creator]; dup {
				continue
			}
		}
		out := buildSelectionOutput(
			cand.item.Edge,
			creator,
			true,
			int32(len(picked)+1),
			cand.score,
			"",
		)
		out.DiversityBucket = diversityBucket(creator, false)
		picked = append(picked, out)
		if creator != "" {
			pickedCreators[creator] = struct{}{}
		}
	}

	if maxPerCreator > 0 {
		picked = FilterByCreatorOpenPositions(picked, openByCreator, maxPerCreator)
	}

	exploreSlots := explorationSlotCount(len(picked), thresholds.ExploreBudgetPct)
	for i := len(picked) - exploreSlots; i < len(picked); i++ {
		if i < 0 {
			break
		}
		picked[i].IsExploration = true
		picked[i].DiversityBucket = diversityBucket(picked[i].CreatorAddress, true)
	}

	pickedSet := make(map[string]contracts.SelectionOutputDTO, len(picked))
	for _, out := range picked {
		if out.Selected {
			pickedSet[out.CausationID] = out
		}
	}

	for _, cand := range ranked {
		idx, ok := indexByEventID[cand.item.Edge.EventID]
		if !ok {
			continue
		}
		if out, ok := pickedSet[cand.item.Edge.EventID]; ok {
			outputs[idx] = out
			continue
		}
		reason := RejectReasonBelowTopK
		if _, dup := pickedCreators[cand.item.CreatorAddress]; dup && cand.item.CreatorAddress != "" {
			reason = RejectReasonCreatorAlreadyOpen
		}
		outputs[idx] = buildSelectionOutput(
			cand.item.Edge,
			cand.item.CreatorAddress,
			false,
			0,
			cand.score,
			reason,
		)
	}

	return outputs
}

func combinedScore(in contracts.ValidatedEdgeDTO) float64 {
	return in.ProbabilityUsed * float64(in.ExpectedValueBps) / 1000.0
}

func rejectForEdge(in contracts.ValidatedEdgeDTO) string {
	if in.Decision != "ACCEPT" {
		return "edge_not_validated:" + in.RejectReason
	}
	return ""
}

func buildSelectionOutput(
	in contracts.ValidatedEdgeDTO,
	creator string,
	selected bool,
	rank int32,
	score float64,
	rejectReason string,
) contracts.SelectionOutputDTO {
	if !selected {
		score = 0
	}
	eventID := contracts.ContentIDFromString(fmt.Sprintf("sel:%s:%v", in.EventID, selected))
	return contracts.SelectionOutputDTO{
		EventID:          eventID,
		TraceID:          in.TraceID,
		CorrelationID:    in.CorrelationID,
		CausationID:      in.EventID,
		VersionID:        in.VersionID,
		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,
		CreatorAddress:   creator,
		Selected:         selected,
		Rank:             rank,
		CombinedScore:    score,
		DiversityBucket:  diversityBucket(creator, false),
		IsExploration:    false,
		RejectReason:     rejectReason,
		SelectedAt:       in.ValidatedAt,
	}
}

func diversityBucket(creator string, explore bool) string {
	if explore {
		return "explore"
	}
	if creator != "" {
		return creator
	}
	return "default"
}

func explorationSlotCount(selectedCount int, exploreBudgetPct float64) int {
	if selectedCount == 0 || exploreBudgetPct <= 0 {
		return 0
	}
	slots := int(math.Ceil(float64(selectedCount) * exploreBudgetPct / 100.0))
	if slots < 1 {
		slots = 1
	}
	if slots > selectedCount {
		slots = selectedCount
	}
	return slots
}

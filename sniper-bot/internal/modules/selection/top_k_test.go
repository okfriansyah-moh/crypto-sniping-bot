package selection

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func edge(id, token string, ev int32, prob float64) BatchItem {
	return BatchItem{
		Edge: contracts.ValidatedEdgeDTO{
			EventID:          id,
			TraceID:          "trace-1",
			CorrelationID:    "corr-1",
			VersionID:        "v1",
			TokenLifecycleID: "lc-" + id,
			TokenAddress:     token,
			Decision:         "ACCEPT",
			ExpectedValueBps: ev,
			ProbabilityUsed:  prob,
			ValidatedAt:      "2026-01-01T00:00:00Z",
		},
		CreatorAddress: "creator-" + token,
	}
}

func TestPickTopK_SelectsHighestScores(t *testing.T) {
	items := []BatchItem{
		edge("e-low", "token-z", 30, 0.5),  // score 0.015
		edge("e-high", "token-a", 100, 0.8), // score 0.080
		edge("e-mid", "token-m", 50, 0.6),   // score 0.030
	}
	thresholds := config.ModeThresholds{MaxPositions: 10, ExploreBudgetPct: 0}
	outs := PickTopK(items, 0, thresholds, 2, 0, nil)

	byID := map[string]contracts.SelectionOutputDTO{}
	for _, o := range outs {
		byID[o.CausationID] = o
	}
	if !byID["e-high"].Selected || byID["e-high"].Rank != 1 {
		t.Fatalf("expected e-high selected rank 1, got %+v", byID["e-high"])
	}
	if !byID["e-mid"].Selected || byID["e-mid"].Rank != 2 {
		t.Fatalf("expected e-mid selected rank 2, got %+v", byID["e-mid"])
	}
	if byID["e-low"].Selected || byID["e-low"].RejectReason != RejectReasonBelowTopK {
		t.Fatalf("expected e-low below_top_k, got %+v", byID["e-low"])
	}
}

func TestPickTopK_ExplorationMarksLowestRank(t *testing.T) {
	items := []BatchItem{
		edge("e1", "token-a", 100, 0.9),
		edge("e2", "token-b", 80, 0.8),
		edge("e3", "token-c", 60, 0.7),
	}
	thresholds := config.ModeThresholds{MaxPositions: 10, ExploreBudgetPct: 5.0}
	outs := PickTopK(items, 0, thresholds, 3, 0, nil)

	var exploreCount int
	for _, o := range outs {
		if o.Selected && o.IsExploration {
			exploreCount++
			if o.Rank != 3 {
				t.Errorf("exploration pick should be lowest rank, got rank %d", o.Rank)
			}
			if o.DiversityBucket != "explore" {
				t.Errorf("expected explore bucket, got %q", o.DiversityBucket)
			}
		}
	}
	if exploreCount != 1 {
		t.Fatalf("expected 1 exploration slot at 5%% of 3 picks, got %d", exploreCount)
	}
}

func TestPickTopK_PerCreatorDedupInBatch(t *testing.T) {
	a1 := edge("e1", "token-a", 100, 0.9)
	a2 := edge("e2", "token-b", 90, 0.9)
	a2.CreatorAddress = a1.CreatorAddress
	items := []BatchItem{a1, a2}
	thresholds := config.ModeThresholds{MaxPositions: 10}
	outs := PickTopK(items, 0, thresholds, 2, 0, nil)

	selected := 0
	for _, o := range outs {
		if o.Selected {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("diversity should allow only one pick per creator, got %d selected", selected)
	}
}

func TestPickTopK_OpenCountBlocksAll(t *testing.T) {
	items := []BatchItem{edge("e1", "token-a", 100, 0.9)}
	thresholds := config.ModeThresholds{MaxPositions: 3}
	outs := PickTopK(items, 3, thresholds, 3, 0, nil)
	if outs[0].Selected {
		t.Fatal("expected no selection when openCount == max_positions")
	}
}

func TestExplorationSlotCount(t *testing.T) {
	if got := explorationSlotCount(4, 5); got != 1 {
		t.Fatalf("expected 1 slot, got %d", got)
	}
	if got := explorationSlotCount(0, 5); got != 0 {
		t.Fatalf("expected 0 slots for empty selection, got %d", got)
	}
}

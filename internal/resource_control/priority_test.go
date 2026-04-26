package resource_control

import (
	"testing"
	"time"

	"crypto-sniping-bot/internal/app/config"
)

func testWeights() config.EventPriorityWeights {
	return DefaultWeights()
}

func TestComputePriority_ExitAlwaysHighest(t *testing.T) {
	w := testWeights()
	now := time.Now()
	exitP := ComputePriority("position_event", true, time.Time{}, now, w)
	openP := ComputePriority("position_event", false, time.Time{}, now, w)
	if exitP <= openP {
		t.Errorf("exit priority %d must exceed open priority %d", exitP, openP)
	}
}

func TestComputePriority_ReplacementAboveOpen(t *testing.T) {
	w := testWeights()
	now := time.Now()
	replP := ComputePriority("execution_replacement", false, time.Time{}, now, w)
	openP := ComputePriority("position_event", false, time.Time{}, now, w)
	if replP <= openP {
		t.Errorf("replacement priority %d must exceed open priority %d", replP, openP)
	}
}

func TestComputePriority_BaseWeightsOrdering(t *testing.T) {
	w := testWeights()
	now := time.Now()

	order := []string{
		"market_data_event",
		"data_quality_event",
		"feature_event",
		"edge_event",
		"validated_edge_event",
		"allocation_event",
	}

	prev := int32(-1)
	for _, et := range order {
		p := ComputePriority(et, false, time.Time{}, now, w)
		if p <= prev {
			t.Errorf("event %s priority %d should exceed previous %d", et, p, prev)
		}
		prev = p
	}
}

func TestComputePriority_UrgencyBonusNonNegative(t *testing.T) {
	w := testWeights()
	now := time.Now()
	expires := now.Add(10 * time.Second)
	p := ComputePriority("edge_event", false, expires, now, w)
	base := ComputePriority("edge_event", false, time.Time{}, now, w)
	if p < base {
		t.Errorf("urgency bonus made priority %d less than base %d", p, base)
	}
}

func TestComputePriority_ExpiredNoBonus(t *testing.T) {
	w := testWeights()
	now := time.Now()
	expired := now.Add(-5 * time.Second)
	p := ComputePriority("edge_event", false, expired, now, w)
	base := ComputePriority("edge_event", false, time.Time{}, now, w)
	if p != base {
		t.Errorf("expired event priority %d should equal base %d", p, base)
	}
}

func TestComputePriority_UnknownTypeZero(t *testing.T) {
	w := testWeights()
	now := time.Now()
	p := ComputePriority("unknown_event", false, time.Time{}, now, w)
	if p != 0 {
		t.Errorf("unknown event type should have priority 0, got %d", p)
	}
}

func TestPRIORITY_EXIT_AtLeastNineHundred(t *testing.T) {
	if PRIORITY_EXIT < 900 {
		t.Errorf("PRIORITY_EXIT must be ≥ 900, got %d", PRIORITY_EXIT)
	}
}

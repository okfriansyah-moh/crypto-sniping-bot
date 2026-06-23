package learning

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// OpportunityMonitor checks whether the pipeline is starving (too few trades)
// or overtrading (too many losses) and suggests an operational mode.
// Mode values: STRICT | BALANCED | EXPLORATION
type OpportunityMonitor struct {
	cfg *config.LearningConfig
}

// NewOpportunityMonitor returns an OpportunityMonitor.
func NewOpportunityMonitor(cfg *config.LearningConfig) *OpportunityMonitor {
	return &OpportunityMonitor{cfg: cfg}
}

// Check analyses recent evaluation data and recommends a mode transition.
// records should cover the monitoring window.
// Returns the recommended mode ("" = no change needed) and an explanation.
func (m *OpportunityMonitor) Check(
	_ context.Context,
	records []contracts.LearningRecordDTO,
	window time.Duration,
) (newMode string, reason string, err error) {
	if window <= 0 {
		return "", "", fmt.Errorf("opportunity monitor: window must be positive")
	}
	if len(records) == 0 {
		// No trades in window — possible starvation.
		return "EXPLORATION", "no_trades_in_window", nil
	}

	var tp, fp, fn int
	for _, r := range records {
		switch r.Classification {
		case "TP":
			tp++
		case "FP":
			fp++
		case "FN":
			fn++
		}
	}

	total := len(records)
	fpRate := float64(fp) / float64(total)
	fnRate := float64(fn) / float64(total)

	// Rug spike or high FP rate → STRICT
	if fpRate > 0.6 {
		return "STRICT", fmt.Sprintf("high_fp_rate:%.2f", fpRate), nil
	}

	// High FN rate (missed pumps) → EXPLORATION
	if fnRate > 0.5 && tp < 3 {
		return "EXPLORATION", fmt.Sprintf("high_fn_rate:%.2f:few_wins:%d", fnRate, tp), nil
	}

	// Starvation: fewer than 3 executed trades in the window
	executedCount := tp + fp
	if executedCount < 3 {
		return "EXPLORATION", fmt.Sprintf("starvation:executed=%d", executedCount), nil
	}

	// Healthy: stay in BALANCED
	return "BALANCED", fmt.Sprintf("healthy:tp=%d:fp=%d:fn=%d", tp, fp, fn), nil
}

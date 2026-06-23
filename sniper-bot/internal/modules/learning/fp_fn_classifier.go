package learning

// Classifier classifies a completed trade as TP, FP, TN, or FN.
// - TP (true positive):  we traded and made a profit (ExitReason=TP1|TP2, pnl > 0)
// - FP (false positive): we traded and lost (ExitReason=SL|TIME|RUG, pnl <= 0)
// - TN (true negative):  we rejected and the token did NOT pump (shadow, low return)
// - FN (false negative): we rejected but the token did pump (shadow, high return)
type Classifier struct{}

// Classify returns the classification string for a trade outcome.
//
//	outcome:  TP1 | TP2 | SL | TIME | RUG | MISSED_PUMP | CORRECT_REJECT
//	pnlPct:   realized PnL fraction (negative for loss); 0 for shadow trades
func (c *Classifier) Classify(outcome string, pnlPct float64) string {
	switch outcome {
	case "TP1", "TP2":
		if pnlPct > 0 {
			return "TP"
		}
		return "FP"
	case "SL", "TIME", "RUG":
		return "FP"
	case "MISSED_PUMP":
		return "FN"
	case "CORRECT_REJECT":
		return "TN"
	default:
		// Fallback: classify by PnL sign for executed trades.
		if pnlPct > 0 {
			return "TP"
		}
		return "FP"
	}
}

// ClassifyShadow classifies a shadow (rejected) trade by observed return.
//
//	observedReturnPct: the return the token achieved in the observation window
//	fnGainThreshold:   minimum return to call FN (from config.learning.fn_gain_threshold_pct)
func ClassifyShadow(observedReturnPct, fnGainThreshold float64) string {
	if observedReturnPct > fnGainThreshold {
		return "FN"
	}
	return "TN"
}

// OutcomeFromPosition maps an exit reason to a canonical outcome string.
func OutcomeFromPosition(exitReason string, pnlPct float64) string {
	switch exitReason {
	case "TP1":
		return "TP1"
	case "TP2":
		return "TP2"
	case "SL":
		return "SL"
	case "TIME":
		return "TIME"
	case "RUG":
		return "RUG"
	default:
		if pnlPct > 0 {
			return "TP1"
		}
		return "SL"
	}
}

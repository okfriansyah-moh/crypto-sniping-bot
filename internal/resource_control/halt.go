package resource_control

// HaltCondition describes why the system entered a halt state.
// Used for audit logging; not stored in the database directly.
type HaltCondition struct {
	// Reason is a human-readable description of the halt cause.
	Reason string
	// DrawdownPct is the drawdown percentage that triggered the halt.
	DrawdownPct float64
	// Mode is the resulting system mode: "HALTED" | "DEGRADED" | "BALANCED".
	Mode string
}

// EvaluateMode returns the system mode based on the current drawdown percentage
// and the previous mode. It enforces the state machine:
//
//	BALANCED  → DEGRADED  when dd >= degradedPct
//	DEGRADED  → HALTED    when dd >= haltPct
//	HALTED    → BALANCED  when dd <= resumePct (auto-resume only from HALTED)
//
// Any other transition (e.g. manual override via Telegram) is handled externally.
func EvaluateMode(currentMode string, drawdownPct, degradedPct, haltPct, resumePct float64) HaltCondition {
	switch {
	case drawdownPct >= haltPct:
		return HaltCondition{
			Reason:      "drawdown_halt_threshold_exceeded",
			DrawdownPct: drawdownPct,
			Mode:        "HALTED",
		}
	case drawdownPct >= degradedPct:
		return HaltCondition{
			Reason:      "drawdown_degraded_threshold_exceeded",
			DrawdownPct: drawdownPct,
			Mode:        "DEGRADED",
		}
	default:
		// Auto-resume: only transition HALTED → BALANCED when drawdown recovers.
		if currentMode == "HALTED" && drawdownPct <= resumePct {
			return HaltCondition{
				Reason:      "drawdown_recovered_auto_resume",
				DrawdownPct: drawdownPct,
				Mode:        "BALANCED",
			}
		}
		// Preserve current mode for DEGRADED → BALANCED (requires drawdown to fully recover).
		// For determinism, only HALTED auto-resumes. DEGRADED requires falling below resumePct as well.
		if currentMode == "DEGRADED" && drawdownPct <= resumePct {
			return HaltCondition{
				Reason:      "drawdown_recovered_degraded_resume",
				DrawdownPct: drawdownPct,
				Mode:        "BALANCED",
			}
		}
		return HaltCondition{
			Reason:      "no_change",
			DrawdownPct: drawdownPct,
			Mode:        currentMode,
		}
	}
}

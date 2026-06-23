package operator

import (
	"context"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// BuildFortressPosture combines system state, shadow gate, and gate evidence
// into a single fortress readiness banner for the operator dashboard.
func BuildFortressPosture(
	ctx context.Context,
	db database.Adapter,
	cfg *config.Config,
	evidenceDir string,
) (*contracts.FortressPostureDTO, error) {
	state, err := db.GetSystemState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get system state: %w", err)
	}
	if state == nil {
		return nil, fmt.Errorf("system state not initialized")
	}

	gate, err := BuildGateEvidence(ctx, db, evidenceDir)
	if err != nil {
		return nil, fmt.Errorf("build gate evidence: %w", err)
	}

	shadowGate, shadowErr := EvaluateShadowGate(ctx, NewShadowGateEvaluator(db, cfg))
	if shadowErr != nil {
		shadowGate = nil
	}

	executionMode := ""
	ingestionDelivery := deliveryStream
	if cfg != nil {
		executionMode = cfg.Execution.Mode
		ingestionDelivery = effectiveGlobalDelivery(cfg.Solana)
	}

	blockers := deriveFortressBlockers(gate, shadowGate)
	readiness := deriveReadinessState(gate, shadowGate, blockers, executionMode)
	nextAction := deriveNextAction(blockers, readiness, gate, shadowGate)

	shadowPass := shadowGate != nil && shadowGate.Pass
	throughput := ""
	if gate != nil {
		throughput = gate.ThroughputVerdict
	}

	return &contracts.FortressPostureDTO{
		ReadinessState:    readiness,
		Blockers:          blockers,
		NextAction:        nextAction,
		Mode:              state.Mode,
		ExecutionMode:     executionMode,
		IngestionDelivery: ingestionDelivery,
		ThroughputVerdict: throughput,
		ShadowGatePass:    shadowPass,
	}, nil
}

func deriveFortressBlockers(
	gate *contracts.GateEvidenceResponseDTO,
	shadow *contracts.ShadowGateBlockDTO,
) []string {
	var blockers []string
	if gate != nil {
		if gate.WSOLTokenAddressEmitted > 0 {
			blockers = append(blockers, "WSOL emitted as token address")
		}
		if gate.ShadowObserverFailed > 0 {
			blockers = append(blockers, fmt.Sprintf("shadow observer errors (%d)", gate.ShadowObserverFailed))
		}
		if gate.IngestionValidTokenRatio > 0 && gate.IngestionValidTokenRatio < 0.80 {
			blockers = append(blockers, "ingestion valid-token ratio below 80%")
		}
		if gate.MarketProbesBacklogRatio > 0.05 {
			blockers = append(blockers, "market probe backlog above 5%")
		}
		if gate.ThroughputVerdict == "CODE_DEFECT" && len(blockers) == 0 {
			blockers = append(blockers, "throughput verdict CODE_DEFECT")
		}
	}
	if shadow != nil && !shadow.Pass && shadow.Reason != "" {
		blockers = append(blockers, "shadow gate: "+shadow.Reason)
	}
	return blockers
}

func deriveReadinessState(
	gate *contracts.GateEvidenceResponseDTO,
	shadow *contracts.ShadowGateBlockDTO,
	blockers []string,
	executionMode string,
) string {
	if len(blockers) > 0 {
		return "BLOCKED"
	}
	traces := int64(0)
	if gate != nil {
		traces = gate.TracesCompleted
	}
	if traces < 1 {
		return "PIPELINE_PROOF"
	}
	if shadow != nil && !shadow.Pass {
		return "SHADOW_TRADING"
	}
	if executionMode == "live" {
		return "LIVE_READY"
	}
	return "SHADOW_READY"
}

func deriveNextAction(
	blockers []string,
	readiness string,
	gate *contracts.GateEvidenceResponseDTO,
	shadow *contracts.ShadowGateBlockDTO,
) string {
	if len(blockers) > 0 {
		return "Fix blocker: " + blockers[0]
	}
	switch readiness {
	case "PIPELINE_PROOF":
		return "Complete one full L0→L10 trace (learning_record emitted)"
	case "SHADOW_TRADING":
		if shadow != nil && shadow.Reason != "" {
			return shadow.Reason
		}
		return "Accumulate shadow trades until gate passes"
	case "SHADOW_READY":
		return "Review gate evidence; flip execution.mode to live when ready"
	case "LIVE_READY":
		return "Monitor executions and drawdown; no action required"
	default:
		if gate != nil && gate.ThroughputVerdict == "MARKET_QUIET" {
			return "Market quiet — verify ingestion delivery and rescan emissions"
		}
		return "Review fortress posture dashboard"
	}
}

// DeriveThroughputVerdict returns throughput verdict from gate evidence file + live DB.
func DeriveThroughputVerdict(ctx context.Context, db database.Adapter, evidenceDir string) (string, error) {
	gate, err := BuildGateEvidence(ctx, db, evidenceDir)
	if err != nil {
		return "", err
	}
	if gate == nil {
		return "", nil
	}
	return gate.ThroughputVerdict, nil
}

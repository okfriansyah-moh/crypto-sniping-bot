package learning

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// Recorder emits LearningRecordDTO on every executed position exit.
// Shadow=false records track real trade outcomes.
type Recorder struct {
	classifier *Classifier
}

// NewRecorder returns a Recorder ready to produce learning records.
func NewRecorder() *Recorder {
	return &Recorder{classifier: &Classifier{}}
}

// RecordExecuted builds a LearningRecordDTO from an exited PositionStateDTO.
// RecordID = SHA256(token_lifecycle_id || "executed")[:16].
// Trace: TraceID/CorrelationID copied from pos; CausationID = causationEventID.
func (r *Recorder) RecordExecuted(
	_ context.Context,
	pos contracts.PositionStateDTO,
	causationEventID string,
	versionID string,
	strategyStatus string,
) (contracts.LearningRecordDTO, error) {
	if pos.Status != "exited" {
		return contracts.LearningRecordDTO{}, fmt.Errorf("recorder: position not exited (status=%s)", pos.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	recordID := contracts.ContentIDFromString(pos.TokenLifecycleID + "executed")
	eventID := contracts.ContentIDFromString("lr-exec:" + pos.EventID)

	outcome := OutcomeFromPosition(pos.ExitReason, pos.PnlPct)
	classification := r.classifier.Classify(outcome, pos.PnlPct)

	cohort := CohortLabel(
		pos.PnlPct, // used as proxy score when feature not available
		0,          // age unavailable at this stage
		"executed",
	)

	dto := contracts.LearningRecordDTO{
		EventID:       eventID,
		TraceID:       pos.TraceID,
		CorrelationID: pos.CorrelationID,
		CausationID:   causationEventID,
		VersionID:     versionID,

		RecordID:         recordID,
		TokenLifecycleID: pos.TokenLifecycleID,

		Shadow:          false,
		Outcome:         outcome,
		Classification:  classification,
		PnlUsd:          pos.PnlUsd,
		PnlPct:          pos.PnlPct,
		PredictionError: 0, // filled by Phase 4 probability model if available
		Cohort:          cohort,

		Simulated:      pos.Status == "failed", // failed positions are simulated
		ExpiredSource:  false,
		StrategyStatus: strategyStatus,

		RecordedAt: now,
	}

	return dto, nil
}

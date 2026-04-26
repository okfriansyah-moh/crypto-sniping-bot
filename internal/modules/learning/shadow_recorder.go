package learning

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// ShadowRecorder emits shadow LearningRecordDTOs for every rejection event.
// Shadow=true records track tokens we rejected so we can measure false negatives.
type ShadowRecorder struct {
	classifier *Classifier
}

// NewShadowRecorder returns a ShadowRecorder.
func NewShadowRecorder() *ShadowRecorder {
	return &ShadowRecorder{classifier: &Classifier{}}
}

// RecordRejection builds a shadow LearningRecordDTO and a ShadowTrade row for
// a rejected token. The ShadowTrade's observation_complete is false until the
// shadow observer closes the window.
//
// stage:    data_quality | edge | validated_edge | selection
// tokenAddress / tokenLifecycleID: from the rejected event.
// Returns the LearningRecordDTO, ShadowTrade (ready to persist), and shadowID.
func (s *ShadowRecorder) RecordRejection(
	_ context.Context,
	stage string,
	tokenAddress string,
	tokenLifecycleID string,
	causationEventID string,
	versionID string,
	strategyStatus string,
) (contracts.LearningRecordDTO, database.ShadowTrade, error) {
	if stage == "" {
		return contracts.LearningRecordDTO{}, database.ShadowTrade{},
			fmt.Errorf("shadow recorder: stage must not be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	recordID := contracts.ContentIDFromString(tokenLifecycleID + "shadow")
	shadowID := contracts.ContentIDFromString(tokenLifecycleID + stage + now)
	eventID := contracts.ContentIDFromString("lr-shadow:" + causationEventID)
	traceID := contracts.ContentIDFromString("shadow-trace:" + tokenLifecycleID)

	cohort := CohortLabel(0, 0, stage)

	dto := contracts.LearningRecordDTO{
		EventID:       eventID,
		TraceID:       traceID,
		CorrelationID: tokenLifecycleID,
		CausationID:   causationEventID,
		VersionID:     versionID,

		RecordID:         recordID,
		TokenLifecycleID: tokenLifecycleID,

		Shadow:         true,
		Outcome:        "CORRECT_REJECT", // initial; updated when observation completes
		Classification: "TN",             // initial; updated by shadow observer to FN if token pumped
		PnlUsd:         0,
		PnlPct:         0,
		Cohort:         cohort,

		Simulated:      false,
		ExpiredSource:  false,
		StrategyStatus: strategyStatus,

		RecordedAt: now,
	}

	st := database.ShadowTrade{
		ShadowID:            shadowID,
		TokenAddress:        tokenAddress,
		Stage:               stage,
		RejectedAt:          now,
		ObservationComplete: false,
		ObservedReturnPct:   0,
		Classification:      "TN",
		LearningRecordID:    recordID,
		VersionID:           versionID,
	}

	return dto, st, nil
}

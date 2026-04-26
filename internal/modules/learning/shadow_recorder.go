package learning

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
)

// ShadowProposal carries the data needed to persist a shadow trade observation.
// It contains no database package dependency; the caller converts it to
// database.ShadowTrade before persisting.
type ShadowProposal struct {
	ShadowID            string
	TokenAddress        string
	Stage               string
	RejectedAt          string
	ObservationComplete bool
	ObservedReturnPct   float64
	Classification      string // initial: "TN"
	LearningRecordID    string
	VersionID           string
}

// ShadowRecorder emits shadow LearningRecordDTOs for every rejection event.
// Shadow=true records track tokens we rejected so we can measure false negatives.
type ShadowRecorder struct {
	classifier *Classifier
}

// NewShadowRecorder returns a ShadowRecorder.
func NewShadowRecorder() *ShadowRecorder {
	return &ShadowRecorder{classifier: &Classifier{}}
}

// RecordRejection builds a shadow LearningRecordDTO and a ShadowProposal for
// a rejected token. The ShadowProposal's observation_complete is false until the
// shadow observer closes the window.
//
// stage:    data_quality | edge | validated_edge | selection
// tokenAddress / tokenLifecycleID: from the rejected event.
// Returns the LearningRecordDTO, ShadowProposal (ready to convert and persist), and shadowID.
func (s *ShadowRecorder) RecordRejection(
	_ context.Context,
	stage string,
	tokenAddress string,
	tokenLifecycleID string,
	causationEventID string,
	versionID string,
	strategyStatus string,
) (contracts.LearningRecordDTO, ShadowProposal, error) {
	if stage == "" {
		return contracts.LearningRecordDTO{}, ShadowProposal{},
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

	sp := ShadowProposal{
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

	return dto, sp, nil
}

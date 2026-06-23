package learning

import (
	"context"
	"fmt"
	"math"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// Evaluator aggregates LearningRecordDTOs into an EvaluationDTO for a version window.
type Evaluator struct{}

// NewEvaluator returns an Evaluator.
func NewEvaluator() *Evaluator { return &Evaluator{} }

// EvaluateWindow computes EvaluationDTO metrics from a slice of records.
// records must already be filtered to the desired versionID and time window
// by the caller (adapter.GetLearningRecordsByWindow).
func (e *Evaluator) EvaluateWindow(
	_ context.Context,
	versionID string,
	start, end time.Time,
	records []contracts.LearningRecordDTO,
) (contracts.EvaluationDTO, error) {
	if versionID == "" {
		return contracts.EvaluationDTO{}, fmt.Errorf("evaluator: versionID must not be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	windowID := versionID + start.UTC().Format(time.RFC3339) + end.UTC().Format(time.RFC3339)
	evalID := contracts.ContentIDFromString("eval:" + windowID)
	traceID := contracts.ContentIDFromString(evalID)

	var tp, fp, tn, fn int32
	var totalWin, totalLoss, totalErr float64
	var winCount, lossCount, errCount int

	for _, r := range records {
		switch r.Classification {
		case "TP":
			tp++
			totalWin += r.PnlPct
			winCount++
		case "FP":
			fp++
			totalLoss += math.Abs(r.PnlPct)
			lossCount++
		case "TN":
			tn++
		case "FN":
			fn++
		}
		if r.PredictionError != 0 {
			totalErr += math.Abs(r.PredictionError)
			errCount++
		}
	}

	total := int32(len(records))

	// Expectancy = P(win) × avg_win − P(loss) × avg_loss
	var expectancy float64
	if total > 0 {
		pWin := float64(tp) / float64(total)
		pLoss := float64(fp) / float64(total)
		avgWin := 0.0
		if winCount > 0 {
			avgWin = totalWin / float64(winCount)
		}
		avgLoss := 0.0
		if lossCount > 0 {
			avgLoss = totalLoss / float64(lossCount)
		}
		expectancy = pWin*avgWin - pLoss*avgLoss
	}

	// Max drawdown: approximate as the worst single-trade loss.
	maxDrawdownPct := 0.0
	for _, r := range records {
		if r.PnlPct < -maxDrawdownPct {
			maxDrawdownPct = math.Abs(r.PnlPct)
		}
	}

	// Brier score: mean squared error of predicted prob vs actual outcome.
	// Here we use prediction_error as a proxy (Phase 4 fills this).
	brierScore := 0.0
	predErrMean := 0.0
	if errCount > 0 {
		predErrMean = totalErr / float64(errCount)
		brierScore = predErrMean * predErrMean // simplified
	}

	return contracts.EvaluationDTO{
		EventID:       contracts.ContentIDFromString("eval-evt:" + evalID),
		TraceID:       traceID,
		CorrelationID: traceID,
		CausationID:   "",
		VersionID:     versionID,

		EvaluationID: evalID,
		WindowStart:  start.UTC().Format(time.RFC3339Nano),
		WindowEnd:    end.UTC().Format(time.RFC3339Nano),
		SampleSize:   total,

		TruePositiveCount:  tp,
		FalsePositiveCount: fp,
		TrueNegativeCount:  tn,
		FalseNegativeCount: fn,

		Expectancy:          round4(expectancy),
		MaxDrawdownPct:      round4(maxDrawdownPct),
		BrierScore:          round4(brierScore),
		PredictionErrorMean: round4(predErrMean),

		EvaluatedAt: now,
	}, nil
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

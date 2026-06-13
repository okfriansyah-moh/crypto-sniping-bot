package workers

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

type abPromoterStub struct {
	stubAdapter
	shadow   *database.StrategyVersion
	active   *database.StrategyVersion
	records  map[string][]contracts.LearningRecordDTO
	promoted string
}

func (s *abPromoterStub) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return s.shadow, nil
}

func (s *abPromoterStub) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return s.active, nil
}

func (s *abPromoterStub) GetLearningRecordsByWindow(
	_ context.Context,
	versionID string,
	_, _ time.Time,
) ([]contracts.LearningRecordDTO, error) {
	return s.records[versionID], nil
}

func (s *abPromoterStub) PromoteStrategyVersion(_ context.Context, newVersionID string, _ int) error {
	s.promoted = newVersionID
	return nil
}

func TestRunABPromoter_NoShadowVersion_NoOp(t *testing.T) {
	adapter := &abPromoterStub{}
	cfg := minConfig()
	if err := RunABPromoter(context.Background(), adapter, cfg, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.promoted != "" {
		t.Fatal("expected no promotion")
	}
}

func TestRunABPromoter_ShouldPromote_PromotesVersion(t *testing.T) {
	now := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	shadowID := "shadow-v2"
	activeID := "active-v1"
	adapter := &abPromoterStub{
		shadow: &database.StrategyVersion{
			StrategyVersionID: shadowID,
			CreatedAt:         now,
			ShadowStartedAt:   &now,
			Status:            "shadow",
		},
		active: &database.StrategyVersion{
			StrategyVersionID: activeID,
			Status:            "active",
		},
		records: map[string][]contracts.LearningRecordDTO{
			shadowID: makeWinningRecords(shadowID, 35, 20),
			activeID: makeWinningRecords(activeID, 35, 10),
		},
	}
	cfg := minConfig()
	cfg.Learning.MinSampleSize = 30
	cfg.Learning.ShadowWindowMinutes = 60
	cfg.Learning.EvalWindowSeconds = 86400

	if err := RunABPromoter(context.Background(), adapter, cfg, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.promoted != shadowID {
		t.Fatalf("expected promotion of %s, got %q", shadowID, adapter.promoted)
	}
}

func makeWinningRecords(versionID string, n int, pnlPct float64) []contracts.LearningRecordDTO {
	out := make([]contracts.LearningRecordDTO, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contracts.LearningRecordDTO{
			VersionID:      versionID,
			Outcome:        "WIN",
			PnlPct:         pnlPct,
			Classification: "TP",
		})
	}
	return out
}

func TestShadowVersionReady_RespectsWindow(t *testing.T) {
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if shadowVersionReady(&database.StrategyVersion{CreatedAt: recent}, 60) {
		t.Fatal("recent shadow should not be ready")
	}
	if !shadowVersionReady(&database.StrategyVersion{CreatedAt: old}, 60) {
		t.Fatal("old shadow should be ready")
	}
}

func TestShadowVersionReady_PostgresTimestampFormat(t *testing.T) {
	old := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05.999999999Z07:00")
	if !shadowVersionReady(&database.StrategyVersion{CreatedAt: old}, 60) {
		t.Fatal("postgres-style timestamp should be parsed and considered ready")
	}
}

func TestShadowVersionReady_MalformedTimestampFailsClosed(t *testing.T) {
	if shadowVersionReady(&database.StrategyVersion{CreatedAt: "not-a-timestamp"}, 60) {
		t.Fatal("malformed timestamp must fail closed")
	}
}

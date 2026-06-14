package ingestion_solana

// pre_filter_test.go — White-box tests for the L0 pre-cohort filter (Task 25).
// Verifies that applyPreFilter correctly drops high-count creators, fails open
// on unknown creators, and is disabled when config.PreFilter.Enabled is false.
// Uses package ingestion_solana (not _test) to access the private method.

import (
	"context"
	"log/slog"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// mockCreatorReader is a test stub implementing CreatorProfileReader.
type mockCreatorReader struct {
	count int32
	known bool
	err   error
}

func (r *mockCreatorReader) GetCount(_ context.Context, _, _ string) (int32, bool, error) {
	return r.count, r.known, r.err
}

// newPreFilterModule creates a minimal Module wired for pre-filter tests.
func newPreFilterModule(enabled bool, maxCount int32, reader CreatorProfileReader) *Module {
	cfg := config.SolanaConfig{
		PreFilter: config.IngestionPreFilterConfig{
			Enabled:                  enabled,
			MaxCreatorPrevTokenCount: maxCount,
		},
	}
	m := New(cfg, "v-test", func(_ context.Context, _ contracts.MarketDataDTO) error { return nil }, slog.Default())
	if reader != nil {
		m.WithCreatorProfileReader(reader)
	}
	return m
}

// testDTO builds a minimal MarketDataDTO with a creator address for filter tests.
func testDTO(creator string) *contracts.MarketDataDTO {
	return &contracts.MarketDataDTO{
		Chain:          "solana",
		TokenAddress:   "TokenAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		CreatorAddress: creator,
		Market:         "solana-pumpfun-amm",
	}
}

// TestPreFilter_DropsHighCountCreator verifies that a creator whose total prior
// token count exceeds MaxCreatorPrevTokenCount is dropped (dropped=true) when
// the pre-filter is enabled and the reader returns a known count.
func TestPreFilter_DropsHighCountCreator(t *testing.T) {
	const maxCount int32 = 25
	reader := &mockCreatorReader{count: 49, known: true, err: nil}
	m := newPreFilterModule(true, maxCount, reader)
	dto := testDTO("SerialLauncherWallet111111111111111111111111")

	dropped := m.applyPreFilter(context.Background(), dto)

	if !dropped {
		t.Error("expected dropped=true for creator_count=49 > max=25, got dropped=false")
	}
	if m.preFilterDropped.Load() != 1 {
		t.Errorf("expected preFilterDropped counter=1, got %d", m.preFilterDropped.Load())
	}
}

// TestPreFilter_FailsOpenOnUnknownCreator verifies that when the reader returns
// known=false (profile not yet in cache), the filter returns dropped=false so
// the token passes through to DQ — the authoritative gate.
func TestPreFilter_FailsOpenOnUnknownCreator(t *testing.T) {
	const maxCount int32 = 25
	reader := &mockCreatorReader{count: 0, known: false, err: nil}
	m := newPreFilterModule(true, maxCount, reader)
	dto := testDTO("UnknownCreatorWallet1111111111111111111111111")

	dropped := m.applyPreFilter(context.Background(), dto)

	if dropped {
		t.Error("expected dropped=false (fail-open) when creator is unknown, got dropped=true")
	}
	if m.preFilterDropped.Load() != 0 {
		t.Errorf("expected preFilterDropped counter=0, got %d", m.preFilterDropped.Load())
	}
}

// TestPreFilter_DisabledByDefault verifies that when PreFilter.Enabled is false
// (the default), applyPreFilter always returns dropped=false regardless of the
// reader's response — the filter is a no-op and the token passes through.
func TestPreFilter_DisabledByDefault(t *testing.T) {
	// Even with a reader returning count=999 (well above any threshold), the
	// filter must return dropped=false when Enabled=false.
	reader := &mockCreatorReader{count: 999, known: true, err: nil}
	m := newPreFilterModule(false /* disabled */, 25, reader)
	dto := testDTO("AnyCreatorWallet1111111111111111111111111111")

	dropped := m.applyPreFilter(context.Background(), dto)

	if dropped {
		t.Error("expected dropped=false when PreFilter.Enabled=false, got dropped=true")
	}
	if m.preFilterDropped.Load() != 0 {
		t.Errorf("expected preFilterDropped counter=0, got %d", m.preFilterDropped.Load())
	}
}

// TestPreFilter_FailsOpenWhenReaderNil verifies that a nil reader (not injected)
// causes the filter to return dropped=false even when Enabled=true.
func TestPreFilter_FailsOpenWhenReaderNil(t *testing.T) {
	m := newPreFilterModule(true, 25, nil /* no reader */)
	dto := testDTO("AnyCreatorWallet1111111111111111111111111111")

	dropped := m.applyPreFilter(context.Background(), dto)

	if dropped {
		t.Error("expected dropped=false when reader is nil (fail-open), got dropped=true")
	}
}

// TestPreFilter_PassesCreatorBelowThreshold verifies that a creator whose count
// is exactly at the threshold is NOT dropped (threshold is an inclusive upper
// bound: count > max triggers drop; count == max passes through).
func TestPreFilter_PassesCreatorBelowThreshold(t *testing.T) {
	const maxCount int32 = 25
	reader := &mockCreatorReader{count: 25, known: true, err: nil}
	m := newPreFilterModule(true, maxCount, reader)
	dto := testDTO("BoundaryCreatorWallet111111111111111111111111")

	dropped := m.applyPreFilter(context.Background(), dto)

	if dropped {
		t.Errorf("expected dropped=false for count=25 == max=25 (boundary, not exceeded), got dropped=true")
	}
}

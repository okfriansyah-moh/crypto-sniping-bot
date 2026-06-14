package workers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/sniper-bot/internal/modules/probes"
)

// TestProbesWorker_NoProbes_PassThrough verifies that when no probes are
// registered, the worker emits a market_data_enriched event carrying the
// unmodified MarketDataDTO (modulo the re-derived EventID).
func TestProbesWorker_NoProbes_PassThrough(t *testing.T) {
	w := NewMarketProbesWorker(&stubAdapter{}, nil, nil)

	md := contracts.MarketDataDTO{
		EventID:       "md-1",
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		Chain:         "eth",
		Market:        "eth-uniswap-v2",
		TokenAddress:  "0xabc",
	}
	payload, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evt := &database.Event{
		EventID:       "md-1",
		EventType:     "market_data_event",
		Payload:       payload,
		TraceID:       md.TraceID,
		CorrelationID: md.CorrelationID,
		VersionID:     md.VersionID,
	}

	out, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output event")
	}
	if out.EventType != MarketDataEnrichedEventType {
		t.Fatalf("expected event type %q, got %q", MarketDataEnrichedEventType, out.EventType)
	}

	var enriched contracts.MarketDataDTO
	if err := json.Unmarshal(out.Payload, &enriched); err != nil {
		t.Fatalf("unmarshal enriched: %v", err)
	}
	if enriched.HoneypotSimKnown {
		t.Fatal("pass-through should leave HoneypotSimKnown=false")
	}
	if enriched.TokenAddress != md.TokenAddress {
		t.Fatalf("token address mutated: got %q", enriched.TokenAddress)
	}
}

// fakeProbe is a deterministic probe used to verify the worker invokes
// each registered probe and threads results into the emitted DTO.
type fakeProbe struct {
	name    string
	called  int
	enrich  func(in contracts.MarketDataDTO) contracts.MarketDataDTO
	wantErr error
}

func (p *fakeProbe) Name() string { return p.name }

func (p *fakeProbe) Probe(_ context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	p.called++
	if p.wantErr != nil {
		return in, p.wantErr
	}
	return p.enrich(in), nil
}

// TestProbesWorker_WithProbe_EnrichesDTO verifies a registered probe is
// invoked and its enrichment appears in the emitted event payload.
func TestProbesWorker_WithProbe_EnrichesDTO(t *testing.T) {
	fp := &fakeProbe{
		name: "fake_honeypot",
		enrich: func(in contracts.MarketDataDTO) contracts.MarketDataDTO {
			out := in
			out.HoneypotSimKnown = true
			out.BuySimSuccess = true
			out.SellSimSuccess = true
			return out
		},
	}
	w := NewMarketProbesWorker(&stubAdapter{}, []probes.MarketProbe{fp}, nil)

	md := contracts.MarketDataDTO{
		EventID:      "md-2",
		TraceID:      "trace-2",
		TokenAddress: "0xdef",
	}
	payload, _ := json.Marshal(md)
	evt := &database.Event{
		EventID:   "md-2",
		EventType: "market_data_event",
		Payload:   payload,
		TraceID:   md.TraceID,
	}

	out, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if fp.called != 1 {
		t.Fatalf("probe called %d times, want 1", fp.called)
	}
	var enriched contracts.MarketDataDTO
	if err := json.Unmarshal(out.Payload, &enriched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !enriched.HoneypotSimKnown || !enriched.BuySimSuccess || !enriched.SellSimSuccess {
		t.Fatalf("expected enrichment flags set, got %+v", enriched)
	}
	if enriched.EventID == md.EventID {
		t.Fatal("expected re-derived enriched EventID distinct from upstream")
	}
}

// countingPumpfunLpProbe tracks GetAccountInfo calls via the shared batch stub.
type countingPumpfunLpProbe struct {
	rpc *batchCountRPC
}

func (p *countingPumpfunLpProbe) Name() string { return "solana_pumpfun_lp" }

func (p *countingPumpfunLpProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	probe := probes.NewSolanaPumpfunLpProbe(p.rpc, fixedSolUsdProbe{px: 150}, probes.SolanaPumpfunLpConfig{
		Enabled:    true,
		TimeoutMs:  1000,
		Commitment: "confirmed",
	}, nil)
	return probe.Probe(ctx, in)
}

type fixedSolUsdProbe struct {
	px float64
}

func (f fixedSolUsdProbe) SolUsd(_ context.Context) (float64, bool) { return f.px, true }

type batchCountRPC struct {
	probes.SolanaProbeRPCClient
	multiCalls int
	accCalls   int
}

func (b *batchCountRPC) GetAccountInfo(ctx context.Context, pubkey, commitment string) (*probes.SolanaAccountData, error) {
	b.accCalls++
	return b.SolanaProbeRPCClient.GetAccountInfo(ctx, pubkey, commitment)
}

func (b *batchCountRPC) GetMultipleAccounts(ctx context.Context, pubkeys []string, commitment string) ([]*probes.SolanaAccountData, error) {
	b.multiCalls++
	return b.SolanaProbeRPCClient.GetMultipleAccounts(ctx, pubkeys, commitment)
}

func TestProbesWorker_RescanPhase2_SkipsPumpfunLp(t *testing.T) {
	fp := &fakeProbe{name: "solana_pumpfun_lp"}
	w := NewMarketProbesWorker(&stubAdapter{}, []probes.MarketProbe{fp}, nil).
		WithBatchAccounts(false, nil, nil, BatchAccountsConfig{RescanSkipPumpfunLpPhase2: true})

	md := contracts.MarketDataDTO{
		EventID:   "md-rescan",
		TraceID:   "trace-rescan",
		Chain:     "solana",
		Market:    "solana-pumpfun",
		Transport: "rescan_24h",
	}
	payload, _ := json.Marshal(md)
	evt := &database.Event{EventID: "md-rescan", EventType: "market_data_event", Payload: payload, TraceID: md.TraceID}

	if _, err := w.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if fp.called != 0 {
		t.Fatalf("expected pumpfun_lp skipped on Phase 2 rescan, probe called %d times", fp.called)
	}
}

func TestProbesWorker_BatchAccounts_SkipsIndividualRPC(t *testing.T) {
	mint := "TokenMintPubkey1111111111111111111111111111"
	pool := "BondingCurvePubkey111111111111111111111111"
	stub := &batchStubRPCForWorker{t: t}
	rpc := &batchCountRPC{SolanaProbeRPCClient: stub}

	w := NewMarketProbesWorker(&stubAdapter{}, []probes.MarketProbe{
		probes.NewSolanaAuthoritiesProbe(rpc, probes.SolanaAuthoritiesConfig{Enabled: true, TimeoutMs: 1000, Commitment: "confirmed"}, nil),
		&countingPumpfunLpProbe{rpc: rpc},
	}, nil).WithBatchAccounts(true, rpc, fixedSolUsdProbe{px: 150}, BatchAccountsConfig{
		AuthoritiesEnabled:   true,
		PumpfunLpEnabled:     true,
		AuthoritiesTimeoutMs: 1000,
		PumpfunLpTimeoutMs:   1000,
		Commitment:           "confirmed",
	})

	md := contracts.MarketDataDTO{
		EventID:      "md-batch",
		TraceID:      "trace-batch",
		Chain:        "solana",
		Market:       "solana-pumpfun",
		TokenAddress: mint,
		PoolAddress:  pool,
		Transport:    "ws",
	}
	payload, _ := json.Marshal(md)
	evt := &database.Event{EventID: "md-batch", EventType: "market_data_event", Payload: payload, TraceID: md.TraceID}

	out, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rpc.multiCalls != 1 {
		t.Fatalf("expected 1 getMultipleAccounts call, got %d", rpc.multiCalls)
	}
	if rpc.accCalls != 0 {
		t.Fatalf("expected 0 getAccountInfo calls after batch, got %d", rpc.accCalls)
	}
	var enriched contracts.MarketDataDTO
	if err := json.Unmarshal(out.Payload, &enriched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !enriched.SolanaAuthoritiesKnown || !enriched.LpStatsKnown {
		t.Fatalf("expected batch enrichment, got authorities=%v lp=%v", enriched.SolanaAuthoritiesKnown, enriched.LpStatsKnown)
	}
}

// batchStubRPCForWorker implements probe account fixtures for worker batch tests.
type batchStubRPCForWorker struct {
	t *testing.T
}

func (s *batchStubRPCForWorker) GetAccountInfo(_ context.Context, pubkey, _ string) (*probes.SolanaAccountData, error) {
	return s.account(pubkey), nil
}

func (s *batchStubRPCForWorker) GetMultipleAccounts(_ context.Context, pubkeys []string, _ string) ([]*probes.SolanaAccountData, error) {
	out := make([]*probes.SolanaAccountData, len(pubkeys))
	for i, k := range pubkeys {
		out[i] = s.account(k)
	}
	return out, nil
}

func (s *batchStubRPCForWorker) GetTokenLargestAccounts(_ context.Context, _ string, _ string) ([]probes.SolanaTokenHolder, error) {
	return nil, nil
}

func (s *batchStubRPCForWorker) GetDASAsset(_ context.Context, _ string) (*probes.DASAsset, error) {
	return nil, nil
}

func (s *batchStubRPCForWorker) account(pubkey string) *probes.SolanaAccountData {
	mint := "TokenMintPubkey1111111111111111111111111111"
	pool := "BondingCurvePubkey111111111111111111111111"
	switch pubkey {
	case mint:
		data := make([]byte, 82)
		return &probes.SolanaAccountData{
			DataB64: encodeB64(data),
			Owner:   "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
		}
	case pool:
		data := make([]byte, 49)
		data[0] = 0xAA
		putU64(data, 16, 30_000_000_000)
		putU64(data, 32, 1_000_000_000)
		putU64(data, 40, 1_000_000_000_000)
		return &probes.SolanaAccountData{DataB64: encodeB64(data)}
	default:
		s.t.Fatalf("unexpected pubkey %s", pubkey)
		return nil
	}
}

func encodeB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func putU64(b []byte, off int, v uint64) {
	for i := 0; i < 8; i++ {
		b[off+i] = byte(v >> (8 * i))
	}
}

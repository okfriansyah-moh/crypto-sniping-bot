package workers

import (
	"context"
	"encoding/json"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/modules/probes"
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

// Package probes defines the on-chain enrichment interface for upstream
// MarketDataDTO records. A probe takes an in-memory MarketDataDTO and
// returns an enriched copy with one or more *Known flag pairs flipped to
// true (per residual-risk #4 — populate the *Known fields the
// data_quality module already consumes).
//
// Design constraints (per architecture invariants):
//
//   - Probes are pure-ish: input DTO + chain RPC client → output DTO.
//     Probes MUST NOT mutate the input. The returned DTO is a copy with
//     the relevant fields filled in.
//   - Probes MUST NOT touch the database. Persistence is the worker's
//     job. Probes return data; the worker emits the enriched event.
//   - Probes MUST be safe to run with no configuration. When disabled
//     they return (in, nil) with the *Known flag left false. This makes
//     probe deployment opt-in — the framework ships dormant.
//   - Probes are testable with a fake RPC client. Each probe defines
//     the minimal RPC interface it needs in this package; we DO NOT
//     import internal/rpc here.
//
// Currently implemented probes:
//
//	honeypot_sim   — buy/sell simulation via callStatic on a deployed
//	                 simulation contract. Populates HoneypotSimKnown,
//	                 BuySimSuccess, SellSimSuccess.
//
// TODO: register tax_probe, lp_lock_probe, owner_privileges_probe,
// holder_dist_probe, wash_stats_probe — these are tracked in residual
// risk register #4 and require dedicated implementations.
package probes

import (
	"context"

	"crypto-sniping-bot/contracts"
)

// MarketProbe enriches a MarketDataDTO with on-chain ground truth. Each
// implementation is responsible for ONE *Known flag pair. Implementations
// MUST be pure-ish: input MarketDataDTO + chain RPC client → output
// MarketDataDTO with the *Known flag flipped to true (or false + an error
// reason). Never mutate input; return a copy.
type MarketProbe interface {
	// Name returns the stable probe identifier ("honeypot_sim", "tax",
	// "lp_lock", ...). Used for structured logging and metrics.
	Name() string

	// Probe consumes the input DTO and returns an enriched copy. On
	// transient errors the implementation SHOULD return (in, err) with
	// the relevant *Known flag left false so downstream Layer-1 detectors
	// degrade per the active operational-mode profile.
	Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error)
}

// ProbeResult captures the structured outcome of a single probe call for
// observability. Workers emit one of these per (probe, event) pair.
type ProbeResult struct {
	ProbeName  string
	Success    bool
	DurationMs int64
	Error      string
}

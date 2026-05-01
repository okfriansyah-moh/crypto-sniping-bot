// run_market_probes.go — optional pipeline stage that enriches
// market_data events with on-chain ground truth produced by the probes
// module.
//
// Wiring decision (residual-risk #4 — option 1, separate event type):
//
// We use a SEPARATE event type ("market_data_enriched") rather than
// re-publishing under the original "market_data_event" type. Reasons:
//
//   - Idempotency: the original event keeps its content-addressable
//     EventID; the enriched event derives a different EventID so the
//     event log is unambiguous about which DTO each consumer processed.
//   - Replay: bit-for-bit determinism is preserved because the enriched
//     DTO only ever appears under its own type, and replays fan in/out
//     through the same routing.
//   - Optionality: when probes.enabled=false the worker is not
//     registered; DQ continues consuming "market_data_event" directly.
//     When probes.enabled=true the orchestrator MUST route DQ on
//     "market_data_enriched" (wiring done in cmd/server.go).
//
// TODO: when additional probes land (tax, lp_lock, owner_privileges,
// holder_dist, wash_stats), register them in the same probe slice. The
// worker iterates probes in registration order; each probe enriches the
// DTO produced by the previous one. Errors from one probe MUST NOT abort
// the pipeline — the partially-enriched DTO is still emitted so the DQ
// layer's *Known-flag-driven degradation runs.

package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/modules/probes"
)

// MarketDataEnrichedEventType is the event type emitted by the probes
// worker. Downstream stages (data_quality) subscribe to this type when
// probes are enabled. When probes are disabled the worker is not
// registered and DQ continues consuming the raw market_data_event.
const MarketDataEnrichedEventType = "market_data_enriched"

// MarketProbesWorker runs each registered MarketProbe in sequence on
// every inbound market_data_event and emits a market_data_enriched
// event. With zero probes registered it acts as pass-through.
type MarketProbesWorker struct {
	adapter database.Adapter
	probes  []probes.MarketProbe
	logger  *slog.Logger
}

// NewMarketProbesWorker constructs a worker. Pass nil/empty `probeList`
// for pass-through mode. The adapter is held only for symmetry with
// other workers — this worker does NOT write to the database; the
// orchestrator emits the returned event onto the bus.
func NewMarketProbesWorker(adapter database.Adapter, probeList []probes.MarketProbe, logger *slog.Logger) *MarketProbesWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &MarketProbesWorker{
		adapter: adapter,
		probes:  probeList,
		logger:  logger,
	}
}

// Process decodes the inbound market_data_event, runs every registered
// probe in order, and emits a market_data_enriched event carrying the
// (possibly enriched) DTO.
//
// Probe failures degrade gracefully: a failing probe logs and is
// skipped, but the worker still emits the partially-enriched DTO so
// the DQ layer's degradation contract (the *Known flags) drives the
// behaviour.
func (w *MarketProbesWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var md contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &md); err != nil {
		return nil, fmt.Errorf("market_probes_worker: unmarshal: %w", err)
	}

	enriched := md
	results := make([]probes.ProbeResult, 0, len(w.probes))
	for _, p := range w.probes {
		start := time.Now()
		out, err := p.Probe(ctx, enriched)
		dur := time.Since(start).Milliseconds()
		res := probes.ProbeResult{
			ProbeName:  p.Name(),
			Success:    err == nil,
			DurationMs: dur,
		}
		if err != nil {
			res.Error = err.Error()
			w.logger.Warn("market_probe_failed",
				"probe", p.Name(),
				"event_id", evt.EventID,
				"trace_id", evt.TraceID,
				"error", err,
			)
		} else {
			enriched = out
		}
		results = append(results, res)
	}

	w.logger.Info("market_probes_completed",
		"event_id", evt.EventID,
		"trace_id", evt.TraceID,
		"probe_count", len(w.probes),
		"honeypot_sim_known", enriched.HoneypotSimKnown,
	)

	// Re-derive the EventID for the enriched DTO so it is distinct from
	// the upstream raw event (idempotency / event-log clarity). Use a
	// content-addressable derivation rooted in the upstream EventID so
	// replays produce identical IDs.
	enriched.EventID = contracts.ContentIDFromString(fmt.Sprintf("md_enriched:%s", md.EventID))

	return makeOutputEvent(
		enriched.EventID, enriched, MarketDataEnrichedEventType,
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

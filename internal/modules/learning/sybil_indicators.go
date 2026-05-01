package learning

import (
	"log/slog"

	"crypto-sniping-bot/contracts"
)

// ApplySybilIndicators populates record.SybilClusterIndicators on the
// suspicious "wash detector said OK but the trade still lost" case
// (residual risk #5 / F-SEC-08).
//
// The field is set ONLY when ALL of the following hold:
//   - the trade was a realized loss (record.PnlUsd < 0), AND
//   - the upstream wash detector reported a low score (dq.WashScore < maxWashScore), AND
//   - the wash-stats window saw enough distinct wallets (market.UniqueWallets1m > minWallets), AND
//   - the wash-stats block was actually populated (market.WashStatsKnown).
//
// Other records leave the field nil — the goal is to surface the
// bypass scenario specifically, not to bloat every learning record.
//
// Loss-pattern bucket routing:
//
//	TODO: route to LossBucketSybilSuspect when classifier is rewired.
//
// Until the typed bucket enum lands, we emit a structured log line
// `loss_bucket_sybil_suspect` so observability picks the events up
// without further code surgery.
//
// minWallets / maxWashScore come from config.learning.sybil_suspect_*.
// If logger is nil, slog.Default() is used.
func ApplySybilIndicators(
	record *contracts.LearningRecordDTO,
	market contracts.MarketDataDTO,
	dq contracts.DataQualityDTO,
	minWallets int,
	maxWashScore float64,
	logger *slog.Logger,
) {
	if record == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Only flag executed losses — shadow trades and wins do not count.
	if record.Shadow {
		return
	}
	if record.PnlUsd >= 0 {
		return
	}

	// Wash detector must have said the token was clean. If WashScore is
	// already high, the wash layer caught it — no Sybil flag needed.
	if dq.WashScore >= maxWashScore {
		return
	}

	// Need an actual measurement; absent stats cannot prove dispersion.
	if !market.WashStatsKnown {
		return
	}
	uniqueWallets := int(market.UniqueWallets1m)
	if uniqueWallets <= minWallets {
		return
	}

	indicators := &contracts.SybilIndicators{
		UniqueWallets1m:     uniqueWallets,
		WalletEntropyNats:   market.WalletEntropy,
		SuspectClusterSize:  0,     // reserved for funding-graph follow-up
		FundingSourceShared: false, // reserved for funding-graph follow-up
	}
	record.SybilClusterIndicators = indicators

	logger.Warn("loss_bucket_sybil_suspect",
		"record_id", record.RecordID,
		"token_lifecycle_id", record.TokenLifecycleID,
		"trace_id", record.TraceID,
		"pnl_usd", record.PnlUsd,
		"pnl_pct", record.PnlPct,
		"wash_score", dq.WashScore,
		"unique_wallets_1m", indicators.UniqueWallets1m,
		"wallet_entropy_nats", indicators.WalletEntropyNats,
	)
}

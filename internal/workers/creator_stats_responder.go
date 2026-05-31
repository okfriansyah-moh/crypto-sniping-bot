// creator_stats_responder.go — async event-bus worker that answers
// creator_stats_request events by fetching creator_profiles and emitting a
// telegram_event containing the formatted stats.
//
// Architecture invariants:
//   - Pure event-bus consumer: no direct Telegram API calls, no SQL.
//   - Calls only adapter.GetCreatorProfile — no raw DB driver.
//   - All event IDs are content-addressable (deriveEventID).
//   - Idempotent: ON CONFLICT DO NOTHING on InsertEvent.
//
// Request payload schema:
//
//	{"chain": "solana", "creator_address": "5n3LYFe..."}
//
// Emits a telegram_event whose text field carries the formatted creator stats.
// The existing Dispatcher reads telegram_event rows and forwards them to the
// operator chat — no changes to the Dispatcher are required.
package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// consumerNameCreatorStats is the stable consumer identity used for SKIP LOCKED.
const consumerNameCreatorStats = "creator_stats_responder"

// creatorStatsRequest is the payload carried by a creator_stats_request event.
type creatorStatsRequest struct {
	Chain       string `json:"chain"`
	CreatorAddr string `json:"creator_address"`
}

// telegramEventPayload mirrors the payload read by the Telegram Dispatcher.
// MessageType selects the alert template; Text carries the pre-formatted message.
type telegramEventPayload struct {
	MessageType string `json:"message_type"`
	Text        string `json:"text"`
}

// RunCreatorStatsResponder claims one creator_stats_request event per call,
// fetches the matching creator profile, and emits a telegram_event containing
// the formatted stats.
//
// Call site (cmd/server.go) wraps this in a tight loop with 100 ms idle backoff.
// Returns nil when no event was available (idle); returns the first fatal error
// otherwise.
func RunCreatorStatsResponder(
	ctx context.Context,
	adapter database.Adapter,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	evt, err := adapter.ClaimNextEvent(ctx, consumerNameCreatorStats,
		[]string{"creator_stats_request"})
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}

	var req creatorStatsRequest
	if err := json.Unmarshal(evt.Payload, &req); err != nil {
		logger.Warn("creator_stats_responder_decode_request",
			"event_id", evt.EventID,
			"error", err,
		)
		// Malformed payload — mark processed so the consumer offset advances.
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	if req.CreatorAddr == "" {
		logger.Warn("creator_stats_responder_empty_creator",
			"event_id", evt.EventID,
		)
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	chain := req.Chain
	if chain == "" {
		chain = "solana"
	}

	profile, found, err := adapter.GetCreatorProfile(ctx, chain, req.CreatorAddr)
	if err != nil {
		logger.Error("creator_stats_responder_get_profile",
			"event_id", evt.EventID,
			"chain", chain,
			"creator", req.CreatorAddr,
			"error", err,
		)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	text := FormatCreatorStats(profile, found)

	tgPayload := telegramEventPayload{
		MessageType: "creator_stats_response",
		Text:        text,
	}
	payloadBytes, err := json.Marshal(tgPayload)
	if err != nil {
		// Should never happen for a simple struct.
		logger.Error("creator_stats_responder_marshal_payload",
			"event_id", evt.EventID,
			"error", err,
		)
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	outEvtID := deriveEventID(evt.EventID, "creator_stats_response_telegram")
	outEvt := database.Event{
		EventID:       outEvtID,
		EventType:     "telegram_event",
		Payload:       payloadBytes,
		TraceID:       evt.TraceID,
		CorrelationID: evt.CorrelationID,
		// VersionID is required by Postgres InsertEvent validation
		// (database/engines/postgres/events.go) — propagate from source request.
		VersionID: evt.VersionID,
	}
	if cid := evt.EventID; cid != "" {
		outEvt.CausationID = &cid
	}

	if err := adapter.InsertEvent(ctx, outEvt); err != nil {
		logger.Error("creator_stats_responder_emit_telegram_event",
			"source_event_id", evt.EventID,
			"out_event_id", outEvtID,
			"error", err,
		)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("creator_stats_responder_processed",
		"source_event_id", evt.EventID,
		"chain", chain,
		"creator", req.CreatorAddr,
		"found", found,
	)

	return adapter.MarkEventProcessed(ctx, evt.EventID)
}

// FormatCreatorStats formats a CreatorProfile into a Telegram-ready HTML string.
// Handles the not-found case and guards against division by zero when TotalTokens is 0.
func FormatCreatorStats(profile contracts.CreatorProfile, found bool) string {
	if !found {
		return "❓ <b>Creator not found</b>\n\nNo launch history recorded for this creator address."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Creator Stats</b>\n<code>%s</code>\n",
		html.EscapeString(profile.CreatorAddress),
	))
	sb.WriteString(fmt.Sprintf("Chain: <code>%s</code>\n\n", html.EscapeString(profile.Chain)))
	sb.WriteString(fmt.Sprintf("Total tokens: <code>%d</code>\n", profile.TotalTokens))

	if profile.TotalTokens > 0 {
		total := float64(profile.TotalTokens)
		rugPct := float64(profile.RugTokens) / total * 100
		migratedPct := float64(profile.MigratedTokens) / total * 100
		goldenGemPct := float64(profile.GoldenGemTokens) / total * 100
		winPct := float64(profile.WinTokens) / total * 100
		lossPct := float64(profile.LossTokens) / total * 100

		sb.WriteString(fmt.Sprintf("Rug rate:       <code>%.1f%%</code> (%d)\n", rugPct, profile.RugTokens))
		sb.WriteString(fmt.Sprintf("Migrated:       <code>%.1f%%</code> (%d)\n", migratedPct, profile.MigratedTokens))
		sb.WriteString(fmt.Sprintf("Golden gem:     <code>%.1f%%</code> (%d)\n", goldenGemPct, profile.GoldenGemTokens))
		sb.WriteString(fmt.Sprintf("Win rate:       <code>%.1f%%</code> (%d)\n", winPct, profile.WinTokens))
		sb.WriteString(fmt.Sprintf("Loss rate:      <code>%.1f%%</code> (%d)\n", lossPct, profile.LossTokens))

		// Safety verdict based on rug rate.
		verdict := "✅ Low rug history"
		switch {
		case rugPct >= 50:
			verdict = "🔴 High rug risk"
		case rugPct >= 20:
			verdict = "⚠️ Elevated rug risk"
		}
		sb.WriteString(fmt.Sprintf("\nVerdict: %s\n", verdict))
	} else {
		sb.WriteString("\n<i>No resolved outcomes yet — token still active or outcome pending.</i>\n")
	}

	if !profile.FirstSeenAt.IsZero() {
		sb.WriteString(fmt.Sprintf("\nFirst seen: <code>%s</code>\n", profile.FirstSeenAt.Format("2006-01-02")))
		sb.WriteString(fmt.Sprintf("Last seen:  <code>%s</code>\n", profile.LastSeenAt.Format("2006-01-02")))
	}

	return sb.String()
}

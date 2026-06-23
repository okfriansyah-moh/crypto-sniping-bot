package operator

import (
	"strconv"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

const stuckPositionThreshold = time.Hour

func positionAge(openedAt string, now time.Time) time.Duration {
	if openedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, openedAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, openedAt)
		if err != nil {
			return 0
		}
	}
	return now.Sub(t)
}

func unrealizedPct(p contracts.PositionStateDTO) float64 {
	entry, ok1 := parseDecimal(p.EntryPrice)
	current, ok2 := parseDecimal(p.CurrentPrice)
	if !ok1 || !ok2 || entry == 0 {
		return 0
	}
	return (current - entry) / entry * 100
}

func unrealizedUsd(p contracts.PositionStateDTO) float64 {
	return p.EntrySizeUsd * (unrealizedPct(p) / 100.0)
}

func parseDecimal(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func summarizeClosed(positions []contracts.PositionStateDTO) (realized float64, wins, losses int32) {
	for _, p := range positions {
		realized += p.PnlUsd
		switch {
		case p.PnlUsd > 0:
			wins++
		case p.PnlUsd < 0:
			losses++
		}
	}
	return realized, wins, losses
}

func winRatePct(wins, losses int32) float64 {
	total := wins + losses
	if total == 0 {
		return 0
	}
	return float64(wins) / float64(total) * 100
}

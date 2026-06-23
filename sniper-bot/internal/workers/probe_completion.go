package workers

import (
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// mandatoryKnownMissing returns field names still unknown for DQ mandatory checks.
func mandatoryKnownMissing(md contracts.MarketDataDTO, pc config.ProbeCompletionConfig, dq *config.DataQualityRuntimeConfig) []string {
	if !pc.Enabled {
		return nil
	}
	var missing []string
	fields := pc.MandatoryFields
	if len(fields) == 0 {
		fields = []string{"social_links", "total_supply", "creator_count", "holder_dist"}
	}
	exemptHolder := isHolderExemptTopic(md.EventTopic, pc.ExemptHolderOnTopics)

	for _, f := range fields {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "social_links":
			if !md.SocialLinksKnown {
				missing = append(missing, "social_links")
			}
		case "total_supply":
			if dq != nil && dq.Thresholds.MaxTotalSupply > 0 && !md.TotalSupplyKnown {
				missing = append(missing, "total_supply")
			}
		case "creator_count":
			if md.CreatorAddress != "" && !md.CreatorPrevTokenCountKnown {
				missing = append(missing, "creator_count")
			}
		case "holder_dist":
			if !exemptHolder && dq != nil && dq.Thresholds.MinHolderCount > 0 && !md.HolderDistKnown {
				missing = append(missing, "holder_dist")
			}
		}
	}
	return missing
}

func isHolderExemptTopic(topic string, exempt []string) bool {
	for _, t := range exempt {
		if topic == t {
			return true
		}
	}
	return topic == "PumpFunCreate" || topic == "PumpFunAMMCreatePool"
}

func probePassComplete(missing []string) bool {
	return len(missing) == 0
}

func inlineRetryDelay(pc config.ProbeCompletionConfig) time.Duration {
	ms := pc.InlineRetryMs
	if ms <= 0 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}

func inlineRetryCount(pc config.ProbeCompletionConfig) int {
	if !pc.Enabled {
		return 0
	}
	n := pc.InlineRetries
	if n < 0 {
		return 0
	}
	return n
}

// shouldForceHolderProbeOnRescan returns true when rescan should run holder_dist
// despite phase-1 skip (probe partial / fair-chance tokens).
func shouldForceHolderProbeOnRescan(transport string, md contracts.MarketDataDTO) bool {
	if !strings.HasPrefix(transport, "rescan_") {
		return false
	}
	// Fresh pending retries always run full probe set.
	if transport == "probe_pending_retry" || transport == "probe_exhausted_retry" {
		return true
	}
	return !md.HolderDistKnown
}

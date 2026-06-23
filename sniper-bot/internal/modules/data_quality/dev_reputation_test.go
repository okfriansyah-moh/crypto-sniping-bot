package data_quality

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

func TestDetectDevReputation_UnknownWhenNeitherFieldKnown(t *testing.T) {
	in := contracts.MarketDataDTO{
		// Neither CreatorPrevTokenCountKnown nor SocialLinksKnown set.
	}
	got := DetectDevReputation(in, 5, 0.40)
	// Fail-closed: unknown dev history is max risk (Score=1.0).
	if got.Unknown {
		t.Fatalf("want Unknown=false (fail-closed), got Unknown=true: %+v", got)
	}
	if got.Score != 1.0 {
		t.Fatalf("want Score=1.0 for unknown dev (fail-closed), got %v", got.Score)
	}
	if !contains(got.Flags, "DEV_UNKNOWN_HISTORY") {
		t.Fatalf("want DEV_UNKNOWN_HISTORY flag, got %v", got.Flags)
	}
}

func TestDetectDevReputation_BelowThresholdNoFlag(t *testing.T) {
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      4, // below maxPrevTokens=5
		SocialLinksKnown:           true,
		HasSocialLinks:             true, // has socials
	}
	got := DetectDevReputation(in, 5, 0.40)
	if got.Unknown {
		t.Fatal("want Unknown=false")
	}
	if got.Score != 0 {
		t.Fatalf("want Score=0 for benign creator, got %v", got.Score)
	}
	if len(got.Flags) != 0 {
		t.Fatalf("want no flags, got %v", got.Flags)
	}
}

func TestDetectDevReputation_AtThresholdSerialLauncher(t *testing.T) {
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      5, // exactly at threshold
	}
	got := DetectDevReputation(in, 5, 0.40)
	if got.Unknown {
		t.Fatal("want Unknown=false")
	}
	if got.Score == 0 {
		t.Fatal("want Score>0 at threshold")
	}
	if !contains(got.Flags, "DEV_SERIAL_LAUNCHER") {
		t.Fatalf("want DEV_SERIAL_LAUNCHER flag, got %v", got.Flags)
	}
}

func TestDetectDevReputation_WellAboveThreshold(t *testing.T) {
	// 29 tokens, threshold=5 → ratio=5.8 → score=clamp(0.5*5.8,0,1)=1.0
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      29,
	}
	got := DetectDevReputation(in, 5, 0.40)
	if got.Score != 1.0 {
		t.Fatalf("want Score=1.0 for 29 tokens at threshold 5, got %v", got.Score)
	}
	if !contains(got.Flags, "DEV_SERIAL_LAUNCHER") {
		t.Fatalf("want DEV_SERIAL_LAUNCHER flag, got %v", got.Flags)
	}
}

func TestDetectDevReputation_NoSocialLinks(t *testing.T) {
	in := contracts.MarketDataDTO{
		SocialLinksKnown: true,
		HasSocialLinks:   false,
	}
	got := DetectDevReputation(in, 5, 0.40)
	if got.Unknown {
		t.Fatal("want Unknown=false")
	}
	if got.Score != 0.40 {
		t.Fatalf("want Score=0.40 for no-social, got %v", got.Score)
	}
	if !contains(got.Flags, "DEV_NO_SOCIAL_LINKS") {
		t.Fatalf("want DEV_NO_SOCIAL_LINKS flag, got %v", got.Flags)
	}
}

func TestDetectDevReputation_BothSignals(t *testing.T) {
	// Serial launcher (29 tokens) + no socials → avg(1.0, 0.40)=0.70
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      29,
		SocialLinksKnown:           true,
		HasSocialLinks:             false,
	}
	got := DetectDevReputation(in, 5, 0.40)
	wantScore := (1.0 + 0.40) / 2.0 // 0.70
	if abs64(got.Score-wantScore) > 1e-9 {
		t.Fatalf("want Score=%.4f, got %.4f", wantScore, got.Score)
	}
	if !contains(got.Flags, "DEV_SERIAL_LAUNCHER") {
		t.Fatalf("missing DEV_SERIAL_LAUNCHER in %v", got.Flags)
	}
	if !contains(got.Flags, "DEV_NO_SOCIAL_LINKS") {
		t.Fatalf("missing DEV_NO_SOCIAL_LINKS in %v", got.Flags)
	}
}

func TestDetectDevReputation_Determinism(t *testing.T) {
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      10,
		SocialLinksKnown:           true,
		HasSocialLinks:             false,
	}
	first := DetectDevReputation(in, 5, 0.40)
	second := DetectDevReputation(in, 5, 0.40)
	if first.Score != second.Score || first.Unknown != second.Unknown {
		t.Fatalf("non-deterministic: %+v vs %+v", first, second)
	}
}

func TestDetectDevReputation_DefaultsApplied(t *testing.T) {
	// maxPrevTokens <= 0 should default to 5.
	in := contracts.MarketDataDTO{
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      5,
	}
	got := DetectDevReputation(in, 0, 0) // 0 → defaults to max=5, noSocialRisk=0 (disabled)
	if !contains(got.Flags, "DEV_SERIAL_LAUNCHER") {
		t.Fatalf("want DEV_SERIAL_LAUNCHER when count=5 at default threshold, got %v", got.Flags)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

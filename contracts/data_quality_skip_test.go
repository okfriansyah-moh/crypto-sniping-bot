package contracts

import "testing"

// TestDataQualityDTO_SkipIsValidDecision verifies that the SKIP decision constant
// is defined, has the expected string value, and is distinct from all other decisions.
func TestDataQualityDTO_SkipIsValidDecision(t *testing.T) {
	if DecisionSkip == "" {
		t.Fatal("DecisionSkip must not be empty")
	}
	if DecisionSkip != "SKIP" {
		t.Fatalf("DecisionSkip = %q; want %q", DecisionSkip, "SKIP")
	}

	// Ensure SKIP is distinct from the other three decisions.
	for _, other := range []string{DecisionPass, DecisionRiskyPass, DecisionReject} {
		if DecisionSkip == other {
			t.Errorf("DecisionSkip %q must not equal %q", DecisionSkip, other)
		}
	}

	// Ensure a DataQualityDTO can be constructed with the SKIP decision (zero-value safe).
	dto := DataQualityDTO{Decision: DecisionSkip}
	if dto.Decision != DecisionSkip {
		t.Errorf("DataQualityDTO.Decision = %q; want %q", dto.Decision, DecisionSkip)
	}
}

// TestDataQualityDTO_FlagConstantsAreDefined verifies that the canonical flag
// constants for the serial-launcher SKIP/RISKY_PASS path are defined and non-empty.
func TestDataQualityDTO_FlagConstantsAreDefined(t *testing.T) {
	if FlagSerialLauncherMonitored == "" {
		t.Fatal("FlagSerialLauncherMonitored must not be empty")
	}
	if FlagSerialLauncherSkipped == "" {
		t.Fatal("FlagSerialLauncherSkipped must not be empty")
	}
	if FlagSerialLauncherMonitored == FlagSerialLauncherSkipped {
		t.Errorf("FlagSerialLauncherMonitored and FlagSerialLauncherSkipped must be distinct")
	}
	if FlagSerialLauncherMonitored != "serial_launcher_monitored" {
		t.Errorf("FlagSerialLauncherMonitored = %q; want %q", FlagSerialLauncherMonitored, "serial_launcher_monitored")
	}
	if FlagSerialLauncherSkipped != "serial_launcher_skipped" {
		t.Errorf("FlagSerialLauncherSkipped = %q; want %q", FlagSerialLauncherSkipped, "serial_launcher_skipped")
	}
}

// TestMarketDataDTO_ZeroMarketCapDoesNotPanic verifies that a MarketDataDTO with
// zero MarketCapUsd (the "not yet available" sentinel) can be constructed and read
// without any panic, and that all four market/volume fields default to zero.
func TestMarketDataDTO_ZeroMarketCapDoesNotPanic(t *testing.T) {
	dto := MarketDataDTO{}

	if dto.MarketCapUsd != 0 {
		t.Errorf("MarketCapUsd zero value = %v; want 0", dto.MarketCapUsd)
	}
	if dto.VolumeUsd5m != 0 {
		t.Errorf("VolumeUsd5m zero value = %v; want 0", dto.VolumeUsd5m)
	}
	if dto.VolumeUsd1h != 0 {
		t.Errorf("VolumeUsd1h zero value = %v; want 0", dto.VolumeUsd1h)
	}
	if dto.VolumeUsd24h != 0 {
		t.Errorf("VolumeUsd24h zero value = %v; want 0", dto.VolumeUsd24h)
	}

	// A zero MarketCapUsd must not be treated as a populated value by filters.
	// This guard pattern mirrors what DQ consumers must implement.
	filterWouldApply := dto.MarketCapUsd > 0
	if filterWouldApply {
		t.Error("zero MarketCapUsd should NOT trigger market-cap filter (filter must guard > 0)")
	}
}

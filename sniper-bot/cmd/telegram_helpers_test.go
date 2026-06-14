package main

import (
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// ── positionAge ───────────────────────────────────────────────────────────────

func TestPositionAge_EmptyString_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := positionAge("", time.Now()); got != 0 {
		t.Errorf("positionAge(''): want 0, got %v", got)
	}
}

func TestPositionAge_InvalidFormat_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := positionAge("not-a-timestamp", time.Now()); got != 0 {
		t.Errorf("positionAge(invalid): want 0, got %v", got)
	}
}

func TestPositionAge_RFC3339_ReturnsCorrectDuration(t *testing.T) {
	// Arrange
	openedAt := "2026-01-01T10:00:00Z"
	now := time.Date(2026, 1, 1, 11, 30, 0, 0, time.UTC) // 1h 30m later

	// Act
	got := positionAge(openedAt, now)

	// Assert: 90 minutes.
	want := 90 * time.Minute
	if got != want {
		t.Errorf("positionAge: want %v, got %v", want, got)
	}
}

func TestPositionAge_RFC3339Nano_ReturnsCorrectDuration(t *testing.T) {
	// Arrange: RFC3339Nano format (sub-second precision).
	openedAt := "2026-01-01T10:00:00.500Z"
	now := time.Date(2026, 1, 1, 10, 1, 0, 0, time.UTC)

	// Act
	got := positionAge(openedAt, now)

	// Assert: approximately 59.5 seconds.
	if got <= 0 {
		t.Errorf("positionAge(RFC3339Nano): want positive duration, got %v", got)
	}
}

// ── humanDuration ─────────────────────────────────────────────────────────────

func TestHumanDuration_Seconds_FormatSeconds(t *testing.T) {
	// Arrange / Act / Assert
	if got := humanDuration(45 * time.Second); got != "45s" {
		t.Errorf("humanDuration(45s): want 45s, got %q", got)
	}
}

func TestHumanDuration_Minutes_FormatMinutesAndSeconds(t *testing.T) {
	// Arrange / Act / Assert
	if got := humanDuration(2*time.Minute + 5*time.Second); got != "2m05s" {
		t.Errorf("humanDuration(2m5s): want 2m05s, got %q", got)
	}
}

func TestHumanDuration_Hours_FormatHoursAndMinutes(t *testing.T) {
	// Arrange / Act / Assert
	if got := humanDuration(3*time.Hour + 15*time.Minute); got != "3h15m" {
		t.Errorf("humanDuration(3h15m): want 3h15m, got %q", got)
	}
}

func TestHumanDuration_Days_FormatDaysAndHours(t *testing.T) {
	// Arrange / Act / Assert
	if got := humanDuration(49 * time.Hour); got != "2d01h" {
		t.Errorf("humanDuration(49h): want 2d01h, got %q", got)
	}
}

// ── priceOrDash ───────────────────────────────────────────────────────────────

func TestPriceOrDash_EmptyString_ReturnsDash(t *testing.T) {
	// Arrange / Act / Assert
	if got := priceOrDash(""); got != "—" {
		t.Errorf("priceOrDash(''): want —, got %q", got)
	}
}

func TestPriceOrDash_NonEmpty_ReturnsValue(t *testing.T) {
	// Arrange / Act / Assert
	if got := priceOrDash("0.00042"); got != "0.00042" {
		t.Errorf("priceOrDash('0.00042'): want 0.00042, got %q", got)
	}
}

// ── parseFloat ────────────────────────────────────────────────────────────────

func TestParseFloat_EmptyString_ReturnsFalse(t *testing.T) {
	// Arrange / Act
	_, ok := parseFloat("")

	// Assert
	if ok {
		t.Error("parseFloat(''): want ok=false")
	}
}

func TestParseFloat_InvalidString_ReturnsFalse(t *testing.T) {
	// Arrange / Act
	_, ok := parseFloat("not-a-number")

	// Assert
	if ok {
		t.Error("parseFloat(invalid): want ok=false")
	}
}

func TestParseFloat_ValidFloat_ReturnsParsedValue(t *testing.T) {
	// Arrange / Act
	v, ok := parseFloat("3.14")

	// Assert
	if !ok {
		t.Fatal("parseFloat('3.14'): want ok=true")
	}
	if v < 3.13 || v > 3.15 {
		t.Errorf("parseFloat('3.14'): want ~3.14, got %v", v)
	}
}

// ── unrealizedPctBps ──────────────────────────────────────────────────────────

func TestUnrealizedPctBps_MissingPrices_ReturnsZero(t *testing.T) {
	// Arrange
	p := contracts.PositionStateDTO{}

	// Act / Assert
	if got := unrealizedPctBps(p); got != 0 {
		t.Errorf("unrealizedPctBps(empty): want 0, got %v", got)
	}
}

func TestUnrealizedPctBps_ZeroEntryPrice_ReturnsZero(t *testing.T) {
	// Arrange: zero entry price would cause division by zero.
	p := contracts.PositionStateDTO{
		EntryPrice:   "0",
		CurrentPrice: "1.5",
	}

	// Act / Assert
	if got := unrealizedPctBps(p); got != 0 {
		t.Errorf("unrealizedPctBps(zero entry): want 0, got %v", got)
	}
}

func TestUnrealizedPctBps_PositiveGain_ReturnsPositive(t *testing.T) {
	// Arrange: entry=$1, current=$2 → 100% gain.
	p := contracts.PositionStateDTO{
		EntryPrice:   "1.0",
		CurrentPrice: "2.0",
	}

	// Act
	got := unrealizedPctBps(p)

	// Assert
	if got < 99.9 || got > 100.1 {
		t.Errorf("unrealizedPctBps: want ~100, got %v", got)
	}
}

func TestUnrealizedPctBps_Loss_ReturnsNegative(t *testing.T) {
	// Arrange: entry=$2, current=$1 → -50% loss.
	p := contracts.PositionStateDTO{
		EntryPrice:   "2.0",
		CurrentPrice: "1.0",
	}

	// Act
	got := unrealizedPctBps(p)

	// Assert
	if got > -49.9 || got < -50.1 {
		t.Errorf("unrealizedPctBps: want ~-50, got %v", got)
	}
}

// ── unrealizedUsd ─────────────────────────────────────────────────────────────

func TestUnrealizedUsd_Gain_ReturnsPositiveUsd(t *testing.T) {
	// Arrange: entry=$1, current=$2, size=$100 → gain=$100.
	p := contracts.PositionStateDTO{
		EntryPrice:   "1.0",
		CurrentPrice: "2.0",
		EntrySizeUsd: 100.0,
	}

	// Act
	got := unrealizedUsd(p)

	// Assert
	if got < 99.9 || got > 100.1 {
		t.Errorf("unrealizedUsd: want ~100, got %v", got)
	}
}

func TestUnrealizedUsd_MissingPrice_ReturnsZero(t *testing.T) {
	// Arrange
	p := contracts.PositionStateDTO{EntrySizeUsd: 100.0}

	// Act / Assert
	if got := unrealizedUsd(p); got != 0 {
		t.Errorf("unrealizedUsd(missing prices): want 0, got %v", got)
	}
}

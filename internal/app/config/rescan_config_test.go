package config_test

// rescan_config_test.go — unit tests for validateRescanConfig and
// applyRescanDefaults, exercised through the public Validate method.
// All tests are offline, deterministic, and follow AAA structure.

import (
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

// rescanValidConfig returns a minimal config with a valid, enabled RescanConfig.
func rescanValidConfig() *config.Config {
	cfg := minimalValidConfig()
	cfg.Rescan = config.RescanConfig{
		Enabled:           true,
		IntervalSeconds:   60,
		MaxPerBandPerTick: 10,
		SkipOpenPositions: true,
		Eligibility: config.RescanEligibility{
			MaxHoneypotScore: 0.5,
			MaxRugScore:      0.65,
			MaxBuyTaxBps:     3000,
			IncludePassed:    true,
		},
		Bands: []config.RescanBand{
			{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
			{Name: "30m", MinAgeSeconds: 1800, MaxAgeSeconds: 2700, Priority: 60},
		},
		ModeOverrides: map[string]config.RescanEligibility{
			"STRICT": {
				MaxHoneypotScore: 0.30,
				MaxRugScore:      0.50,
				MaxBuyTaxBps:     1500,
				IncludePassed:    false,
			},
		},
	}
	return cfg
}

// ── validateRescanConfig (via Validate) ──────────────────────────────────────

func TestRescanConfig_ValidEnabled_Passes(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("expected no error for valid enabled rescan config, got: %v", err)
	}
}

func TestRescanConfig_DisabledSkipsValidation(t *testing.T) {
	// Arrange — disabled config with deliberately invalid interval (would fail if enabled).
	cfg := minimalValidConfig()
	cfg.Rescan = config.RescanConfig{
		Enabled:         false,
		IntervalSeconds: 0, // invalid, but must be ignored when disabled
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("disabled rescan must skip validation, got: %v", err)
	}
}

func TestRescanConfig_IntervalTooLow_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.IntervalSeconds = 5 // below minimum of 10

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for interval_seconds < 10")
	}
}

func TestRescanConfig_IntervalAtMinimum_Passes(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.IntervalSeconds = 10

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("interval_seconds=10 is the minimum and must pass, got: %v", err)
	}
}

func TestRescanConfig_BandMinAgeEqualToMaxAge_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Bands = []config.RescanBand{
		{Name: "bad", MinAgeSeconds: 900, MaxAgeSeconds: 900}, // equal is invalid
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error when min_age_seconds == max_age_seconds")
	}
}

func TestRescanConfig_BandMinAgeGreaterThanMaxAge_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Bands = []config.RescanBand{
		{Name: "inverted", MinAgeSeconds: 1800, MaxAgeSeconds: 900}, // inverted
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error when min_age_seconds > max_age_seconds")
	}
}

func TestRescanConfig_BandsNotSortedAscending_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Bands = []config.RescanBand{
		{Name: "30m", MinAgeSeconds: 1800, MaxAgeSeconds: 2700},
		{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800}, // out of order
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error when bands are not sorted by min_age_seconds ascending")
	}
}

func TestRescanConfig_HoneypotScoreAboveOne_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Eligibility.MaxHoneypotScore = 1.1 // out of [0,1]

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for max_honeypot_score > 1.0")
	}
}

func TestRescanConfig_HoneypotScoreNegative_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Eligibility.MaxHoneypotScore = -0.1

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for max_honeypot_score < 0")
	}
}

func TestRescanConfig_BuyTaxBpsAboveMax_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Eligibility.MaxBuyTaxBps = 10001 // above [0, 10000]

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for max_buy_tax_bps > 10000")
	}
}

func TestRescanConfig_BuyTaxBpsNegative_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.Eligibility.MaxBuyTaxBps = -1

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for max_buy_tax_bps < 0")
	}
}

func TestRescanConfig_InvalidModeOverrideKey_Fails(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.ModeOverrides = map[string]config.RescanEligibility{
		"UNKNOWN_MODE": {MaxHoneypotScore: 0.5, MaxRugScore: 0.5, MaxBuyTaxBps: 1000},
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err == nil {
		t.Error("expected error for invalid mode_overrides key")
	}
}

func TestRescanConfig_ValidModeOverrideKeys_Passes(t *testing.T) {
	// Arrange
	cfg := rescanValidConfig()
	cfg.Rescan.ModeOverrides = map[string]config.RescanEligibility{
		"STRICT":      {MaxHoneypotScore: 0.3, MaxRugScore: 0.5, MaxBuyTaxBps: 1500},
		"BALANCED":    {MaxHoneypotScore: 0.5, MaxRugScore: 0.65, MaxBuyTaxBps: 3000},
		"EXPLORATION": {MaxHoneypotScore: 0.6, MaxRugScore: 0.75, MaxBuyTaxBps: 4500},
	}

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("all valid mode override keys must pass, got: %v", err)
	}
}

// ── applyRescanDefaults (observed side-effects via Load from YAML) ────────────

func TestRescanDefaults_EmptyEnabledConfig_GetsDefaults(t *testing.T) {
	// Arrange — YAML that only sets enabled: true; all other fields absent.
	yaml := `
database:
  host: localhost
  port: 5432
  database: sniper
  user: sniper
rescan:
  enabled: true
`
	path := writeTempYAML(t, yaml)

	// Act
	cfg, err := config.Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Rescan.IntervalSeconds == 0 {
		t.Error("applyRescanDefaults must set interval_seconds when zero")
	}
	if cfg.Rescan.MaxPerBandPerTick == 0 {
		t.Error("applyRescanDefaults must set max_per_band_per_tick when zero")
	}
	if len(cfg.Rescan.Bands) == 0 {
		t.Error("applyRescanDefaults must populate default bands when none configured")
	}
	if len(cfg.Rescan.ModeOverrides) == 0 {
		t.Error("applyRescanDefaults must populate default mode_overrides when nil")
	}
	if !cfg.Rescan.Eligibility.IncludePassed {
		t.Error("applyRescanDefaults must set IncludePassed=true by default")
	}
}

func TestRescanDefaults_ExplicitValuesPreserved(t *testing.T) {
	// Arrange — YAML with explicit non-default interval.
	yaml := `
database:
  host: localhost
  port: 5432
  database: sniper
  user: sniper
rescan:
  enabled: true
  interval_seconds: 120
  max_per_band_per_tick: 50
  bands:
    - name: "15m"
      min_age_seconds: 900
      max_age_seconds: 1800
      priority: 80
`
	path := writeTempYAML(t, yaml)

	// Act
	cfg, err := config.Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Rescan.IntervalSeconds != 120 {
		t.Errorf("explicit interval_seconds must be preserved, got %d", cfg.Rescan.IntervalSeconds)
	}
	if cfg.Rescan.MaxPerBandPerTick != 50 {
		t.Errorf("explicit max_per_band_per_tick must be preserved, got %d", cfg.Rescan.MaxPerBandPerTick)
	}
	if len(cfg.Rescan.Bands) != 1 {
		t.Errorf("explicit bands must be preserved, got %d", len(cfg.Rescan.Bands))
	}
}

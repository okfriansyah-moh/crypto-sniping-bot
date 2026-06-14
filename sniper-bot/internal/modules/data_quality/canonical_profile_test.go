// Tests that canonicalProfile fallback values match config/data_quality.yaml
// defaults (Tasks 12 + 14). If this test fails after a YAML rename, update
// both the YAML and this file together.
package data_quality

import "testing"

// TestCanonicalProfile_MatchesYAMLDefaults verifies that every field in the
// hardcoded canonicalProfile map exactly matches the canonical values documented
// in config/data_quality.yaml (Task 12) and §7.9 of the implementation plan.
//
// This is a contract test: if the YAML changes, this test must be updated too.
func TestCanonicalProfile_MatchesYAMLDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode                              string
		wantRejectAbove                   float64
		wantRiskyPassAbove                float64
		wantUnknownFactor                 float64
		wantMinTokenAgeSeconds            int32
		wantMaxCreatorPrevTokenCount      int32
		wantRequiresSocialLinks           bool
		wantSerialLauncherMaxRiskScore    float64
		wantSerialLauncherMinHolderCount  int32
	}{
		{
			mode:                             "STRICT",
			wantRejectAbove:                  0.30,
			wantRiskyPassAbove:               0.15,
			wantUnknownFactor:                0.5,
			wantMinTokenAgeSeconds:           0,
			wantMaxCreatorPrevTokenCount:     0,   // sentinel → use global=1, hard REJECT unchanged
			wantRequiresSocialLinks:          false,
			wantSerialLauncherMaxRiskScore:   0.0,
			wantSerialLauncherMinHolderCount: 0,
		},
		{
			mode:                             "BALANCED",
			wantRejectAbove:                  0.50,
			wantRiskyPassAbove:               0.25,
			wantUnknownFactor:                0.0,
			wantMinTokenAgeSeconds:           0,
			wantMaxCreatorPrevTokenCount:     0,   // sentinel → use global=1, hard REJECT unchanged
			wantRequiresSocialLinks:          false,
			wantSerialLauncherMaxRiskScore:   0.0,
			wantSerialLauncherMinHolderCount: 0,
		},
		{
			mode:                             "EXPLORATION",
			wantRejectAbove:                  0.65,
			wantRiskyPassAbove:               0.35,
			wantUnknownFactor:                0.0,
			wantMinTokenAgeSeconds:           -1,
			wantMaxCreatorPrevTokenCount:     5,
			wantRequiresSocialLinks:          true,
			wantSerialLauncherMaxRiskScore:   0.40,
			wantSerialLauncherMinHolderCount: 50,
		},
		{
			mode:                             "VERY_EXPLORATION",
			wantRejectAbove:                  0.75,
			wantRiskyPassAbove:               0.45,
			wantUnknownFactor:                0.0,
			wantMinTokenAgeSeconds:           -1,
			wantMaxCreatorPrevTokenCount:     10,
			wantRequiresSocialLinks:          true,
			wantSerialLauncherMaxRiskScore:   0.45,
			wantSerialLauncherMinHolderCount: 25,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			p, ok := canonicalProfile[tc.mode]
			if !ok {
				t.Fatalf("mode %q not found in canonicalProfile", tc.mode)
			}

			if p.RejectAbove != tc.wantRejectAbove {
				t.Errorf("RejectAbove: got %v, want %v", p.RejectAbove, tc.wantRejectAbove)
			}
			if p.RiskyPassAbove != tc.wantRiskyPassAbove {
				t.Errorf("RiskyPassAbove: got %v, want %v", p.RiskyPassAbove, tc.wantRiskyPassAbove)
			}
			if p.UnknownFactor != tc.wantUnknownFactor {
				t.Errorf("UnknownFactor: got %v, want %v", p.UnknownFactor, tc.wantUnknownFactor)
			}
			if p.MinTokenAgeSeconds != tc.wantMinTokenAgeSeconds {
				t.Errorf("MinTokenAgeSeconds: got %v, want %v", p.MinTokenAgeSeconds, tc.wantMinTokenAgeSeconds)
			}
			if p.MaxCreatorPrevTokenCount != tc.wantMaxCreatorPrevTokenCount {
				t.Errorf("MaxCreatorPrevTokenCount: got %v, want %v", p.MaxCreatorPrevTokenCount, tc.wantMaxCreatorPrevTokenCount)
			}
			if p.SerialLauncherRequiresSocialLinks != tc.wantRequiresSocialLinks {
				t.Errorf("SerialLauncherRequiresSocialLinks: got %v, want %v", p.SerialLauncherRequiresSocialLinks, tc.wantRequiresSocialLinks)
			}
			if p.SerialLauncherMaxRiskScore != tc.wantSerialLauncherMaxRiskScore {
				t.Errorf("SerialLauncherMaxRiskScore: got %v, want %v", p.SerialLauncherMaxRiskScore, tc.wantSerialLauncherMaxRiskScore)
			}
			if p.SerialLauncherMinHolderCount != tc.wantSerialLauncherMinHolderCount {
				t.Errorf("SerialLauncherMinHolderCount: got %v, want %v", p.SerialLauncherMinHolderCount, tc.wantSerialLauncherMinHolderCount)
			}
		})
	}
}

// TestCanonicalProfile_StrictBalancedSentinelPreserved verifies that the sentinel
// value (0) is preserved for STRICT and BALANCED so the hard-reject path in
// ProcessForMode remains unchanged.
func TestCanonicalProfile_StrictBalancedSentinelPreserved(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"STRICT", "BALANCED"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p := canonicalProfile[mode]
			if p.MaxCreatorPrevTokenCount != 0 {
				t.Errorf("%s: MaxCreatorPrevTokenCount must be 0 (sentinel) to preserve hard-reject; got %d",
					mode, p.MaxCreatorPrevTokenCount)
			}
			if p.SerialLauncherRequiresSocialLinks {
				t.Errorf("%s: SerialLauncherRequiresSocialLinks must be false (unused when sentinel=0); got true", mode)
			}
			if p.SerialLauncherMaxRiskScore != 0.0 {
				t.Errorf("%s: SerialLauncherMaxRiskScore must be 0.0 (unused when sentinel=0); got %v",
					mode, p.SerialLauncherMaxRiskScore)
			}
			if p.SerialLauncherMinHolderCount != 0 {
				t.Errorf("%s: SerialLauncherMinHolderCount must be 0 (unused when sentinel=0); got %d",
					mode, p.SerialLauncherMinHolderCount)
			}
		})
	}
}

// TestCanonicalProfile_ExplorationModesHaveSerialLauncherOverrides verifies that
// EXPLORATION and VERY_EXPLORATION have non-zero serial-launcher overrides so the
// conditional RISKY_PASS path is reachable.
func TestCanonicalProfile_ExplorationModesHaveSerialLauncherOverrides(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode            string
		wantMaxCount    int32
		wantMinHolders  int32
		wantMaxRisk     float64
	}{
		{"EXPLORATION", 5, 50, 0.40},
		{"VERY_EXPLORATION", 10, 25, 0.45},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			p := canonicalProfile[tc.mode]

			if p.MaxCreatorPrevTokenCount != tc.wantMaxCount {
				t.Errorf("MaxCreatorPrevTokenCount: got %d, want %d", p.MaxCreatorPrevTokenCount, tc.wantMaxCount)
			}
			if !p.SerialLauncherRequiresSocialLinks {
				t.Errorf("SerialLauncherRequiresSocialLinks: got false, want true")
			}
			if p.SerialLauncherMaxRiskScore != tc.wantMaxRisk {
				t.Errorf("SerialLauncherMaxRiskScore: got %v, want %v", p.SerialLauncherMaxRiskScore, tc.wantMaxRisk)
			}
			if p.SerialLauncherMinHolderCount != tc.wantMinHolders {
				t.Errorf("SerialLauncherMinHolderCount: got %d, want %d", p.SerialLauncherMinHolderCount, tc.wantMinHolders)
			}
		})
	}
}

// TestCanonicalProfile_VeryExplorationLooserThanExploration verifies that
// VERY_EXPLORATION is strictly more permissive than EXPLORATION on all
// serial-launcher quality gates (higher count allowed, lower holder bar, higher risk cap).
func TestCanonicalProfile_VeryExplorationLooserThanExploration(t *testing.T) {
	t.Parallel()

	exp := canonicalProfile["EXPLORATION"]
	vexp := canonicalProfile["VERY_EXPLORATION"]

	if vexp.MaxCreatorPrevTokenCount <= exp.MaxCreatorPrevTokenCount {
		t.Errorf("VERY_EXPLORATION.MaxCreatorPrevTokenCount (%d) should be > EXPLORATION (%d)",
			vexp.MaxCreatorPrevTokenCount, exp.MaxCreatorPrevTokenCount)
	}
	if vexp.SerialLauncherMinHolderCount >= exp.SerialLauncherMinHolderCount {
		t.Errorf("VERY_EXPLORATION.SerialLauncherMinHolderCount (%d) should be < EXPLORATION (%d)",
			vexp.SerialLauncherMinHolderCount, exp.SerialLauncherMinHolderCount)
	}
	if vexp.SerialLauncherMaxRiskScore <= exp.SerialLauncherMaxRiskScore {
		t.Errorf("VERY_EXPLORATION.SerialLauncherMaxRiskScore (%v) should be > EXPLORATION (%v)",
			vexp.SerialLauncherMaxRiskScore, exp.SerialLauncherMaxRiskScore)
	}
}

// TestResolveProfile_FallsBackToCanonicalProfile verifies that resolveProfile
// returns the canonical fallback when no YAML override is present, and that the
// returned profile carries the correct serial-launcher fields.
func TestResolveProfile_FallsBackToCanonicalProfile(t *testing.T) {
	t.Parallel()

	// nil runtime → no YAML overrides; must fall back to canonicalProfile.
	name, p := resolveProfile("EXPLORATION", nil)

	if name != "EXPLORATION" {
		t.Errorf("canonical name: got %q, want %q", name, "EXPLORATION")
	}
	if p.MaxCreatorPrevTokenCount != 5 {
		t.Errorf("MaxCreatorPrevTokenCount: got %d, want 5", p.MaxCreatorPrevTokenCount)
	}
	if !p.SerialLauncherRequiresSocialLinks {
		t.Errorf("SerialLauncherRequiresSocialLinks: got false, want true")
	}
	if p.SerialLauncherMaxRiskScore != 0.40 {
		t.Errorf("SerialLauncherMaxRiskScore: got %v, want 0.40", p.SerialLauncherMaxRiskScore)
	}
	if p.SerialLauncherMinHolderCount != 50 {
		t.Errorf("SerialLauncherMinHolderCount: got %d, want 50", p.SerialLauncherMinHolderCount)
	}
}

package data_quality

import "testing"

func TestDetectWashTrading(t *testing.T) {
	cases := []struct {
		name           string
		volume         float64
		holders        int32
		ageSeconds     int32
		want           bool
	}{
		{"high_per_holder_few_holders_new_pool", 200_000, 20, 600, true},
		{"too_many_holders", 200_000, 100, 600, false},
		{"old_pool", 200_000, 20, 7200, false},
		{"low_per_holder", 1_000, 20, 600, false},
		{"zero_volume", 0, 20, 600, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectWashTrading(tc.volume, tc.holders, tc.ageSeconds)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestDetectRugRisk(t *testing.T) {
	cfgMin := "1000000000000000" // 0.001 ETH
	if !DetectRugRisk(false, "5000000000000000", cfgMin) {
		t.Fatal("thin pool with no LP lock should flag")
	}
	if DetectRugRisk(true, "5000000000000000", cfgMin) {
		t.Fatal("locked LP should not flag")
	}
	if DetectRugRisk(false, "100000000000000000", cfgMin) {
		t.Fatal("deep pool should not flag")
	}
	if !DetectRugRisk(false, "not-a-number", cfgMin) {
		t.Fatal("unparseable reserve should flag")
	}
}

func TestDetectTaxAnomaly(t *testing.T) {
	if !DetectTaxAnomaly(2000, 0, 1000, 1500) {
		t.Fatal("high buy tax must flag")
	}
	if !DetectTaxAnomaly(0, 2000, 1000, 1500) {
		t.Fatal("high sell tax must flag")
	}
	if DetectTaxAnomaly(500, 500, 1000, 1500) {
		t.Fatal("normal taxes should not flag")
	}
}

func TestAggregateRiskScoreBounds(t *testing.T) {
	score := AggregateRiskScore(4, 4, true, true, true, true, true)
	if score != 1 {
		t.Fatalf("max risk should clamp to 1, got %v", score)
	}
	if AggregateRiskScore(0, 4, false, false, false, false, false) != 0 {
		t.Fatal("clean signals must score 0")
	}
}

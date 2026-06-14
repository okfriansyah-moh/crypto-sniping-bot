package features

import "testing"

func TestComputeTopNConcentrationBps(t *testing.T) {
	cases := []struct {
		name     string
		balances []float64
		supply   float64
		topN     int
		want     int32
	}{
		{"empty", nil, 1000, 10, 0},
		{"zero supply", []float64{100}, 0, 10, 0},
		{"top1 of total", []float64{500, 200, 100}, 1000, 1, 5000},
		{"top3 of total", []float64{500, 200, 100}, 1000, 3, 8000},
		{"saturates at total", []float64{900, 200}, 1000, 2, 10000},
		{"topN larger than holders", []float64{100, 100}, 1000, 10, 2000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeTopNConcentrationBps(c.balances, c.supply, c.topN)
			if got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestHolderConcentrationScore(t *testing.T) {
	if HolderConcentrationScore(0) != 0 {
		t.Fatal("0 bps should be unknown=0")
	}
	if HolderConcentrationScore(10000) != 0 {
		t.Fatal("100% concentration must be 0")
	}
	got := HolderConcentrationScore(5000)
	if got < 0.49 || got > 0.51 {
		t.Fatalf("midpoint score: got %f", got)
	}
}

func TestComputeSocialPresence(t *testing.T) {
	has, n := ComputeSocialPresence(SocialLinkInputs{})
	if has || n != 0 {
		t.Fatalf("empty: got (%v,%d)", has, n)
	}
	has, n = ComputeSocialPresence(SocialLinkInputs{Website: "https://x", Twitter: " "})
	if !has || n != 1 {
		t.Fatalf("whitespace counted: got (%v,%d)", has, n)
	}
	has, n = ComputeSocialPresence(SocialLinkInputs{Website: "a", Twitter: "b", Telegram: "c", Discord: "d", Other: []string{"e"}})
	if !has || n != 5 {
		t.Fatalf("all five: got (%v,%d)", has, n)
	}
}

func TestSocialPresenceScore(t *testing.T) {
	if SocialPresenceScore(0) != 0 {
		t.Fatal("0 must be 0")
	}
	if SocialPresenceScore(3) != 1.0 || SocialPresenceScore(7) != 1.0 {
		t.Fatal("≥3 must saturate to 1.0")
	}
	if SocialPresenceScore(1) < 0.32 || SocialPresenceScore(1) > 0.34 {
		t.Fatalf("1/3 expected ~0.333, got %f", SocialPresenceScore(1))
	}
}

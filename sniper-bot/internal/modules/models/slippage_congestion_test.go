package models

import "testing"

func TestApplyCongestion(t *testing.T) {
	cases := []struct {
		name        string
		base        int32
		latency     int32
		anchor      int32
		maxFactor   float64
		wantBps     int32
		wantFactor  float64
		factorDelta float64
	}{
		{"disabled by anchor", 100, 800, 0, 2.0, 100, 1.0, 0},
		{"disabled by maxFactor", 100, 800, 200, 1.0, 100, 1.0, 0},
		{"no excess", 100, 200, 200, 2.0, 100, 1.0, 0},
		{"latency below anchor stays 1x", 100, 50, 200, 2.0, 100, 1.0, 0},
		{"50% over anchor", 100, 300, 200, 2.0, 150, 1.5, 1e-9},
		{"clamped at max", 100, 1000, 200, 2.0, 200, 2.0, 1e-9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotBps, gotFactor := ApplyCongestion(c.base, c.latency, c.anchor, c.maxFactor)
			if gotBps != c.wantBps {
				t.Fatalf("bps: got %d want %d", gotBps, c.wantBps)
			}
			if c.factorDelta == 0 && gotFactor != c.wantFactor {
				t.Fatalf("factor: got %f want %f", gotFactor, c.wantFactor)
			}
			if c.factorDelta > 0 && (gotFactor < c.wantFactor-c.factorDelta || gotFactor > c.wantFactor+c.factorDelta) {
				t.Fatalf("factor: got %f want ~%f", gotFactor, c.wantFactor)
			}
		})
	}
}

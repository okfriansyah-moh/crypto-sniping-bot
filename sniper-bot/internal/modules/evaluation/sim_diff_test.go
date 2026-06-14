package evaluation

import "testing"

func TestComputeExecutionVariance(t *testing.T) {
	cases := []struct {
		name    string
		sim     float64
		real    float64
		wantBps int32
		wantOk  bool
	}{
		{"sim zero", 0, 100, 0, false},
		{"sim negative", -1, 100, 0, false},
		{"exact match", 100, 100, 0, true},
		{"5% worse", 100, 95, -500, true},
		{"10% better", 100, 110, 1000, true},
		{"tiny diff rounds to bps", 100, 100.05, 5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotBps, gotOk := ComputeExecutionVariance(c.sim, c.real)
			if gotOk != c.wantOk {
				t.Fatalf("ok: got %v want %v", gotOk, c.wantOk)
			}
			if gotBps != c.wantBps {
				t.Fatalf("bps: got %d want %d", gotBps, c.wantBps)
			}
		})
	}
}

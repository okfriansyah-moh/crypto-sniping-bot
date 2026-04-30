package selection

import (
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestFilterByCreatorOpenPositions_Disabled(t *testing.T) {
	in := []contracts.SelectionOutputDTO{
		{Selected: true, CreatorAddress: "A"},
		{Selected: true, CreatorAddress: "A"},
	}
	out := FilterByCreatorOpenPositions(in, nil, 0)
	for i, c := range out {
		if !c.Selected {
			t.Fatalf("idx %d: must remain selected when disabled", i)
		}
	}
}

func TestFilterByCreatorOpenPositions_RespectsExistingOpen(t *testing.T) {
	in := []contracts.SelectionOutputDTO{
		{Selected: true, CreatorAddress: "A"}, // already at cap → reject
		{Selected: true, CreatorAddress: "B"}, // pass
	}
	out := FilterByCreatorOpenPositions(in, map[string]int32{"A": 1}, 1)
	if out[0].Selected || out[0].RejectReason != RejectReasonCreatorAlreadyOpen {
		t.Fatalf("idx 0: want rejected, got %+v", out[0])
	}
	if !out[1].Selected {
		t.Fatalf("idx 1: must pass")
	}
}

func TestFilterByCreatorOpenPositions_BatchCap(t *testing.T) {
	in := []contracts.SelectionOutputDTO{
		{Selected: true, CreatorAddress: "X"}, // pick #1 (cap=2)
		{Selected: true, CreatorAddress: "X"}, // pick #2 (cap=2)
		{Selected: true, CreatorAddress: "X"}, // reject
		{Selected: true, CreatorAddress: "Y"}, // pass
	}
	out := FilterByCreatorOpenPositions(in, nil, 2)
	if !out[0].Selected || !out[1].Selected {
		t.Fatal("first two X must survive")
	}
	if out[2].Selected || out[2].RejectReason != RejectReasonCreatorAlreadyOpen {
		t.Fatalf("third X must be rejected: %+v", out[2])
	}
	if !out[3].Selected {
		t.Fatal("Y must pass")
	}
}

func TestFilterByCreatorOpenPositions_EmptyCreator(t *testing.T) {
	in := []contracts.SelectionOutputDTO{
		{Selected: true, CreatorAddress: ""},
		{Selected: true, CreatorAddress: ""},
	}
	out := FilterByCreatorOpenPositions(in, nil, 1)
	for i, c := range out {
		if !c.Selected {
			t.Fatalf("idx %d: empty creator must not be capped", i)
		}
	}
}

func TestFilterByCreatorOpenPositions_PreservesPriorReject(t *testing.T) {
	in := []contracts.SelectionOutputDTO{
		{Selected: false, CreatorAddress: "A", RejectReason: "low_score"}, // pre-rejected stays
	}
	out := FilterByCreatorOpenPositions(in, map[string]int32{"A": 1}, 1)
	if out[0].RejectReason != "low_score" {
		t.Fatalf("must preserve prior reject reason: %q", out[0].RejectReason)
	}
}

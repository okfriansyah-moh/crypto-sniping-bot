package learning

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

func TestIsConfirmedRug(t *testing.T) {
	cases := []struct {
		name string
		rec  contracts.LearningRecordDTO
		want bool
	}{
		{"rug+real", contracts.LearningRecordDTO{Outcome: "RUG"}, true},
		{"rug+shadow", contracts.LearningRecordDTO{Outcome: "RUG", Shadow: true}, false},
		{"rug+simulated", contracts.LearningRecordDTO{Outcome: "RUG", Simulated: true}, false},
		{"sl is not rug", contracts.LearningRecordDTO{Outcome: "SL"}, false},
		{"tp is not rug", contracts.LearningRecordDTO{Outcome: "TP"}, false},
		{"time is not rug", contracts.LearningRecordDTO{Outcome: "TIME"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsConfirmedRug(c.rec); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestBuildCreatorRugObservation(t *testing.T) {
	rec := contracts.LearningRecordDTO{
		VersionID:    "v1",
		EdgeSnapshot: contracts.EdgeDTO{CreatorAddress: "creator-X", TokenAddress: "token-Y"},
	}
	obs, ok := BuildCreatorRugObservation(rec, "solana-mainnet")
	if !ok {
		t.Fatal("expected ok")
	}
	if obs.CreatorAddress != "creator-X" || obs.Chain != "solana-mainnet" ||
		obs.TokenAddress != "token-Y" || obs.StrategyVersionID != "v1" {
		t.Fatalf("bad obs: %+v", obs)
	}
}

func TestBuildCreatorRugObservation_MissingFields(t *testing.T) {
	if _, ok := BuildCreatorRugObservation(contracts.LearningRecordDTO{}, "solana"); ok {
		t.Fatal("empty creator must return ok=false")
	}
	rec := contracts.LearningRecordDTO{EdgeSnapshot: contracts.EdgeDTO{CreatorAddress: "X"}}
	if _, ok := BuildCreatorRugObservation(rec, ""); ok {
		t.Fatal("empty chain must return ok=false")
	}
}

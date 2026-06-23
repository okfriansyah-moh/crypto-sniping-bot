package operator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"
)

func TestBuildIngestionStatus_FromConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Solana: config.SolanaConfig{
			Transport: config.IngestionTransportConfig{Mode: "grpc"},
			Ingestion: config.SolanaIngestionConfig{
				Delivery: "hybrid",
				Webhook:  config.SolanaWebhookConfig{Enabled: true},
			},
			Programs: []config.SolanaProgramConfig{
				{ProgramID: "pump", Family: "pumpfun", Delivery: "webhook"},
			},
		},
	}
	got := operator.BuildIngestionStatus(cfg)
	if got.GlobalDelivery != "hybrid" || !got.WebhookActive {
		t.Fatalf("unexpected ingestion status: %+v", got)
	}
	if len(got.Programs) != 1 || got.Programs[0].Delivery != "webhook" {
		t.Fatalf("programs: %+v", got.Programs)
	}
}

func TestBuildGateBrief_ReadsLatestFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gate_brief_old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gate_brief_new.txt")
	if err := os.WriteFile(path, []byte("MODE: PIPELINE_PROOF\nBLOCKERS: NONE"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := operator.BuildGateBrief(dir)
	if err != nil {
		t.Fatalf("BuildGateBrief: %v", err)
	}
	if got.Path != "gate_brief_new.txt" {
		t.Errorf("Path = %q", got.Path)
	}
	if got.Content == "" {
		t.Fatal("expected content snippet")
	}
}

func TestBuildRescanStats_BandPhases(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Rescan: config.RescanConfig{
			Enabled: true,
			Bands: []config.RescanBand{
				{Name: "15m"},
				{Name: "24h"},
				{Name: "48h"},
			},
		},
	}
	got, err := operator.BuildRescanStats(context.Background(), &overviewStubDB{}, cfg)
	if err != nil {
		t.Fatalf("BuildRescanStats: %v", err)
	}
	if len(got.Bands) != 3 {
		t.Fatalf("bands = %+v", got.Bands)
	}
	if got.Bands[0].Phase != "A" || got.Bands[1].Phase != "B" || got.Bands[2].Phase != "C" {
		t.Fatalf("phases = %+v", got.Bands)
	}
}

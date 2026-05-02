package main
package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"crypto-sniping-bot/database"
)

// enableTradingStubAdapter implements only the three database.Adapter
// methods that buildEnableTradingFn touches. The embedded interface field
// is left nil — any other call panics, which is exactly what we want as a
// guard against accidental scope creep in the closure.
type enableTradingStubAdapter struct {
	database.Adapter
	halted     bool
	haltErr    error
	stats      *database.PipelineStats
	statsErr   error
	clearedFor string // captures the reason argument so tests can assert audit log
	clearErr   error
}

func (s *enableTradingStubAdapter) IsSystemHalted(_ context.Context) (bool, string, error) {
	return s.halted, "", s.haltErr
}

func (s *enableTradingStubAdapter) GetPipelineStats(_ context.Context, _ int) (*database.PipelineStats, error) {
	return s.stats, s.statsErr
}

func (s *enableTradingStubAdapter) SetSystemHalt(_ context.Context, halted bool, reason, _ string) error {
	if !halted {
		s.clearedFor = reason
		s.halted = false
	} else {
		s.halted = true
	}
	return s.clearErr
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEnableTrading_AlreadyEnabled(t *testing.T) {
	stub := &enableTradingStubAdapter{halted: false}
	fn := buildEnableTradingFn(stub, quietLogger())
	out, err := fn(context.Background(), "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "already enabled") {
		t.Fatalf("expected idempotent response, got: %q", out)
	}
	if stub.clearedFor != "" {
		t.Fatal("must not call SetSystemHalt when already enabled")
	}
}

func TestEnableTrading_BlockedBelowShadowMin(t *testing.T) {
	stub := &enableTradingStubAdapter{
		halted: true,
		stats:  &database.PipelineStats{DQPassed: 100},
	}
	fn := buildEnableTradingFn(stub, quietLogger())
	out, err := fn(context.Background(), "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "shadow run incomplete") {
		t.Fatalf("expected refusal, got: %q", out)
	}
	if stub.clearedFor != "" {
		t.Fatal("halt MUST remain set when shadow data insufficient")
	}
	if !stub.halted {
		t.Fatal("halt flag must remain true after refusal")
	}
}

func TestEnableTrading_AllowedWhenShadowMinReached(t *testing.T) {
	stub := &enableTradingStubAdapter{
		halted: true,
		stats:  &database.PipelineStats{DQPassed: 600},
	}
	fn := buildEnableTradingFn(stub, quietLogger())
	out, err := fn(context.Background(), "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "Trading enabled") {
		t.Fatalf("expected enablement, got: %q", out)
	}
	if !strings.Contains(stub.clearedFor, "Phase 6") {
		t.Fatalf("audit reason must mention Phase 6, got: %q", stub.clearedFor)
	}
	if stub.halted {
		t.Fatal("halt flag must be cleared")
	}
}

func TestEnableTrading_ProceedsOnStatsInfraError(t *testing.T) {
	// Infra failure must NOT block indefinitely — operator must still be
	// able to enable trading. A nil stats with an error is the realistic
	// failure shape.
	stub := &enableTradingStubAdapter{
		halted:   true,
		stats:    nil,
		statsErr: io.ErrUnexpectedEOF,
	}
	fn := buildEnableTradingFn(stub, quietLogger())
	out, err := fn(context.Background(), "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "Trading enabled") {
		t.Fatalf("expected enablement on infra error, got: %q", out)
	}
	if stub.halted {
		t.Fatal("halt must be cleared even when stats query failed")
	}
}

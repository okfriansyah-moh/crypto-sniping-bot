package ingestion_solana

// Phase 11 (Reference-Repo Improvements R2 — INGEST) — hybrid Solana
// streaming transport abstraction.
//
// Reference repos (m8s-lab, chainstacklabs/pumpfun-bonkfun-bot,
// 0xalberto/solana-raydium-pumpfun-sniper-Rust) all stream Pump.fun &
// Raydium events via Yellowstone/Geyser gRPC for sub-100ms latency,
// while preserving WS+RPC as a safety fallback. This file introduces
// the same pattern in Go without replacing the existing RPC pipeline.
//
// Architecture rules:
//   * Pure transport abstraction. NO database access. NO module imports
//     beyond contracts/ and config.
//   * Three modes: "rpc" (legacy), "grpc" (Yellowstone), "hybrid"
//     (gRPC primary with auto-fallback to RPC after N consecutive errors).
//   * Deterministic: same input event stream → identical TokenEvent
//     output sequence regardless of transport mode.
//   * The orchestrator chooses transport via SolanaConfig.Transport.Mode.
//
// The gRPC implementation here is a STUB. Wiring a real Yellowstone
// client (e.g. github.com/rpcpool/yellowstone-grpc) is out of scope for
// the additive Phase 11 contract layer; the stub returns
// ErrTransportNotImplemented so HybridTransport can demonstrate the
// fallback path in tests.

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrTransportNotImplemented is returned by stub transport methods that
// have not been wired to a concrete client (e.g. gRPC mode without a
// Yellowstone dependency installed).
var ErrTransportNotImplemented = errors.New("ingestion_solana: transport not implemented")

// TokenEvent is the transport-level stream output. It is intentionally
// minimal — full normalization (CreateEvent decode, slot mapping) lives
// in normalize.go and is invoked AFTER Transport.Recv returns. Thus the
// transport layer is pluggable without touching feature extraction.
type TokenEvent struct {
	Program    string // "pumpfun" | "raydium-v4" | …
	Signature  string // tx signature (Solana base58)
	Slot       uint64
	LogPayload []byte // raw program log bytes for downstream decoders
}

// Transport is the minimal contract a Solana streaming source must satisfy.
// Implementations: RpcTransport, GrpcTransport, HybridTransport.
type Transport interface {
	// Start initializes the underlying connection. Must be idempotent
	// when called twice on the same instance.
	Start(ctx context.Context) error

	// Recv returns the next event or an error. Blocks until an event is
	// available or ctx is canceled.
	Recv(ctx context.Context) (TokenEvent, error)

	// Mode reports the active transport mode for observability.
	// Possible values: "rpc", "grpc".
	Mode() string

	// Close releases resources. Idempotent.
	Close() error
}

// RpcTransport is a thin marker around the existing WS+RPC subscribe.go
// pipeline. Today Recv returns ErrTransportNotImplemented because the
// existing ingestion loop is wired directly to the publish channel, not
// through this abstraction. This struct exists so the HybridTransport
// fallback target is type-safe; future PRs will adapt subscribe.go to
// satisfy Transport.
type RpcTransport struct {
	mode string // always "rpc"
}

// NewRpcTransport returns a placeholder RPC transport.
func NewRpcTransport() *RpcTransport { return &RpcTransport{mode: "rpc"} }

func (t *RpcTransport) Start(ctx context.Context) error { return nil }
func (t *RpcTransport) Recv(ctx context.Context) (TokenEvent, error) {
	return TokenEvent{}, ErrTransportNotImplemented
}
func (t *RpcTransport) Mode() string { return t.mode }
func (t *RpcTransport) Close() error { return nil }

// GrpcTransport is the Yellowstone/Geyser stream stub. A real
// implementation would wire github.com/rpcpool/yellowstone-grpc and
// SubscribeRequest filters. For now it returns ErrTransportNotImplemented
// so HybridTransport's fallback logic can be exercised end-to-end.
type GrpcTransport struct {
	endpoint  string
	authToken string
}

// NewGrpcTransport constructs a GrpcTransport. endpoint must be
// "host:port"; authToken may be empty.
func NewGrpcTransport(endpoint, authToken string) *GrpcTransport {
	return &GrpcTransport{endpoint: endpoint, authToken: authToken}
}

func (t *GrpcTransport) Start(ctx context.Context) error { return ErrTransportNotImplemented }
func (t *GrpcTransport) Recv(ctx context.Context) (TokenEvent, error) {
	return TokenEvent{}, ErrTransportNotImplemented
}
func (t *GrpcTransport) Mode() string { return "grpc" }
func (t *GrpcTransport) Close() error { return nil }

// HybridTransport prefers Primary; on N consecutive Recv errors it
// permanently switches to Fallback for the rest of the run. Switching
// is one-way and bounded — HybridTransport never re-attempts Primary
// to avoid oscillation. The orchestrator can restart the worker to
// re-test Primary after a cooldown.
type HybridTransport struct {
	Primary       Transport
	Fallback      Transport
	MaxErrors     int32 // consecutive errors before fallback; <=0 means "never fall back"
	consecErrors  atomic.Int32
	usingFallback atomic.Bool
}

// NewHybridTransport constructs a hybrid wrapper. maxErrors must be > 0
// for fallback to ever activate.
func NewHybridTransport(primary, fallback Transport, maxErrors int32) *HybridTransport {
	return &HybridTransport{Primary: primary, Fallback: fallback, MaxErrors: maxErrors}
}

func (h *HybridTransport) Start(ctx context.Context) error {
	if err := h.Primary.Start(ctx); err != nil {
		// Primary refused to start — fall back immediately.
		h.usingFallback.Store(true)
		return h.Fallback.Start(ctx)
	}
	return nil
}

func (h *HybridTransport) Recv(ctx context.Context) (TokenEvent, error) {
	if h.usingFallback.Load() {
		return h.Fallback.Recv(ctx)
	}
	evt, err := h.Primary.Recv(ctx)
	if err == nil {
		h.consecErrors.Store(0)
		return evt, nil
	}
	// Track consecutive errors; switch to fallback when threshold reached.
	if h.MaxErrors > 0 {
		if h.consecErrors.Add(1) >= h.MaxErrors {
			h.usingFallback.Store(true)
			// Best-effort cold-start of fallback.
			_ = h.Fallback.Start(ctx)
		}
	}
	return evt, err
}

func (h *HybridTransport) Mode() string {
	if h.usingFallback.Load() {
		return h.Fallback.Mode()
	}
	return h.Primary.Mode()
}

func (h *HybridTransport) Close() error {
	_ = h.Primary.Close()
	return h.Fallback.Close()
}

// UsingFallback is exposed for tests and observability.
func (h *HybridTransport) UsingFallback() bool { return h.usingFallback.Load() }

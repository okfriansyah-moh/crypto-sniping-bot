// Package ingestion implements Layer 0 DEX event ingestion.
// It subscribes to on-chain factory logs, normalizes them into MarketDataDTO,
// and emits events to the bus via the EventEmitter callback.
//
// Architecture invariants:
//   - This package MUST NOT import database/ — it receives EventEmitter from the worker.
//   - This package MUST NOT import other modules — only contracts/ and internal/rpc/.
//   - All functions are deterministic: same log + same config = same DTO.
package ingestion

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/rpc"
)

// EventEmitter is the callback type through which the ingestion module
// delivers normalized MarketDataDTOs to the orchestrator/worker.
// The worker wraps adapter.InsertEvent + adapter.InsertMarketData inside it.
type EventEmitter func(ctx context.Context, dto contracts.MarketDataDTO) error

// Config holds all ingestion parameters for a single chain.
// All values come from config/chains.yaml — no hardcoded values.
type Config struct {
	Chain             string
	Market            string
	FactoryAddresses  []string
	BaseTokens        []string
	WSEndpoints       []string
	RPCEndpoints      []string
	ConfirmationDepth uint32
	Backoff           BackoffConfig
	PollIntervalMs    int
	HeartbeatInterval int // ms
	HeartbeatTimeout  int // ms
	// LastProcessedBlock seeds gap recovery on restart (from the ingestion watermark).
	// Used to initialise PollConfig.LastProcessedBlock when the poll fallback is active.
	LastProcessedBlock uint64
}

// Module manages the ingestion lifecycle for a single chain.
// It is a pure component — it holds no database handles.
type Module struct {
	cfg           Config
	versionID     string
	emit          EventEmitter
	logger        *slog.Logger
	client        rpc.Client        // injected via WithClient; nil = use clientFactory
	clientFactory rpc.ClientFactory // creates a fresh client per reconnect attempt

	mu         sync.Mutex
	pairTokens map[string][2]string // pool address → [token0, token1]
	cancelFn   context.CancelFunc
}

// New creates a new Module ready to Start.
// versionID is the active StrategyVersionID pinned at worker startup.
// emit is the EventEmitter callback provided by the worker.
func New(cfg Config, versionID string, emit EventEmitter, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	// Sort collections for deterministic processing.
	sort.Strings(cfg.FactoryAddresses)
	sort.Strings(cfg.BaseTokens)
	sort.Strings(cfg.WSEndpoints)
	sort.Strings(cfg.RPCEndpoints)

	return &Module{
		cfg:        cfg,
		versionID:  versionID,
		emit:       emit,
		logger:     logger,
		pairTokens: make(map[string][2]string),
	}
}

// WithClient injects a fixed RPC client. Primarily used in tests.
// WithClientFactory takes precedence when both are set.
func (m *Module) WithClient(c rpc.Client) *Module {
	m.client = c
	return m
}

// WithClientFactory injects a factory that creates a fresh RPC client per
// reconnect attempt, enabling true endpoint failover. Takes precedence over
// the fixed client set via WithClient.
func (m *Module) WithClientFactory(f rpc.ClientFactory) *Module {
	m.clientFactory = f
	return m
}

// Start begins the ingestion loop.
// If a WebSocket client is configured, it runs a subscription loop.
// Falls back to polling if no WebSocket client or on subscription failure.
// Blocks until ctx is cancelled.
func (m *Module) Start(ctx context.Context) error {
	if m.client == nil && m.clientFactory == nil {
		// No client source configured — noop until context cancels (useful in tests).
		m.logger.Info("ingestion_no_client_noop", "chain", m.cfg.Chain)
		<-ctx.Done()
		return ctx.Err()
	}

	m.logger.Info("ingestion_starting",
		"chain", m.cfg.Chain,
		"market", m.cfg.Market,
		"factories", len(m.cfg.FactoryAddresses),
	)

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		endpoint := SelectEndpoint(m.cfg.WSEndpoints, attempt)
		if endpoint == "" {
			endpoint = SelectEndpoint(m.cfg.RPCEndpoints, attempt)
		}

		// Resolve client for this attempt.
		// Factory creates a fresh client per endpoint for true failover;
		// fixed client (WithClient) is the fallback used in tests.
		client := m.client
		if m.clientFactory != nil {
			var factoryErr error
			client, factoryErr = m.clientFactory(ctx, endpoint)
			if factoryErr != nil {
				m.logger.Warn("ingestion_client_create_failed",
					"chain", m.cfg.Chain, "endpoint", endpoint, "error", factoryErr)
				attempt++
				delay := NextDelay(m.cfg.Backoff, attempt)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
		}

		subCtx, cancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.cancelFn = cancel
		m.mu.Unlock()

		err := m.runSubscribeLoop(subCtx, endpoint, client)
		cancel()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			m.logger.Warn("ingestion_subscribe_failed",
				"chain", m.cfg.Chain, "attempt", attempt, "error", err)
		}

		delay := NextDelay(m.cfg.Backoff, attempt)
		m.logger.Info("ingestion_reconnecting",
			"chain", m.cfg.Chain, "delay_ms", delay.Milliseconds(), "attempt", attempt+1)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		attempt++
	}
}

// Stop cancels the running ingestion loops.
func (m *Module) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelFn != nil {
		m.cancelFn()
	}
	m.logger.Info("ingestion_stopped", "chain", m.cfg.Chain)
	return nil
}

// runSubscribeLoop runs the WebSocket subscription loop for one connection attempt.
func (m *Module) runSubscribeLoop(ctx context.Context, endpoint string, client rpc.Client) error {
	m.mu.Lock()
	pairTokens := m.pairTokens
	m.mu.Unlock()

	cfg := SubscribeConfig{
		Chain:             m.cfg.Chain,
		Market:            m.cfg.Market,
		FactoryAddresses:  m.cfg.FactoryAddresses,
		BaseTokens:        m.cfg.BaseTokens,
		ConfirmationDepth: m.cfg.ConfirmationDepth,
		VersionID:         m.versionID,
		Endpoint:          endpoint,
		PairTokens:        pairTokens,
	}

	if err := SubscribeLoop(ctx, cfg, client, m.emit, m.logger); err != nil {
		return fmt.Errorf("subscribe_loop: %w", err)
	}
	return nil
}

// EmitWithTransport is an internal helper for tests — wraps emit with a fixed transport label.
func (m *Module) emitWithTransport(ctx context.Context, dto contracts.MarketDataDTO, transport string) error {
	// transport is already baked into the DTO by normalize functions.
	return m.emit(ctx, dto)
}

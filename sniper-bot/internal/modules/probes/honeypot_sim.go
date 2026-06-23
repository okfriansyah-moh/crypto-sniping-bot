package probes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// HoneypotSimRPCClient is the minimal RPC surface the honeypot_sim probe
// requires. It is intentionally narrow — we only need eth_call against
// the simulation contract — so the probe is trivially fakeable in tests
// and does NOT depend on internal/rpc.
type HoneypotSimRPCClient interface {
	// EthCall executes an eth_call against the configured simulation
	// contract at the given block tag ("latest" in production). Returns
	// the raw ABI-encoded return bytes.
	EthCall(ctx context.Context, callData []byte, block string) ([]byte, error)
}

// HoneypotSimConfig configures the honeypot_sim probe.
type HoneypotSimConfig struct {
	// Enabled toggles the probe at the worker layer. Even when the
	// worker invokes a disabled probe, the probe itself returns
	// (in, nil) with HoneypotSimKnown=false so the contract is uniform.
	Enabled bool `yaml:"enabled"`

	// SimulationContract is the EIP-55 address of the deployed
	// simulation contract. Empty value disables the probe at runtime
	// (returns the input unchanged with HoneypotSimKnown=false).
	SimulationContract string `yaml:"simulation_contract"`

	// TimeoutMs bounds the eth_call. Bounded by validate_ranges to
	// [100, 30000].
	TimeoutMs int `yaml:"timeout_ms"`
}

// HoneypotSimProbe simulates a buy → sell round-trip on the simulation
// contract via eth_call. It populates HoneypotSimKnown and the two
// boolean simulation outcomes on the returned DTO.
//
// IMPORTANT: This file deliberately does NOT contain the simulation
// contract bytecode or selector encoding logic — that is an external
// deployment concern. We only encode the ABI selector + token argument
// here so the probe is testable. A production deployment must point
// SimulationContract at an audited contract that exposes
// `simulateBuySell(address token) returns (bool buyOk, bool sellOk)`.
type HoneypotSimProbe struct {
	rpc    HoneypotSimRPCClient
	cfg    HoneypotSimConfig
	logger *slog.Logger

	// Encoder is overridable so tests can substitute a deterministic
	// callData builder without depending on a real ABI library.
	Encoder func(token string) []byte

	// Decoder maps raw return bytes to (buyOk, sellOk). Defaulted to
	// decodeTwoBools and overridable in tests.
	Decoder func(out []byte) (bool, bool, error)
}

// NewHoneypotSimProbe constructs a probe with default ABI encode/decode.
// Returns a usable probe even when cfg.SimulationContract is empty — in
// that case Probe() returns the input unchanged with HoneypotSimKnown
// left false and a single warn log on first invocation.
func NewHoneypotSimProbe(rpc HoneypotSimRPCClient, cfg HoneypotSimConfig, logger *slog.Logger) *HoneypotSimProbe {
	if logger == nil {
		logger = slog.Default()
	}
	return &HoneypotSimProbe{
		rpc:     rpc,
		cfg:     cfg,
		logger:  logger,
		Encoder: encodeSimulateBuySell,
		Decoder: decodeTwoBools,
	}
}

// Name returns the canonical probe identifier.
func (p *HoneypotSimProbe) Name() string { return "honeypot_sim" }

// Probe runs the buy/sell simulation. See package doc for the contract:
//
//   - empty SimulationContract → (in, nil), HoneypotSimKnown=false
//   - rpc/decode error          → (in, err), HoneypotSimKnown=false
//   - success                   → (out, nil), HoneypotSimKnown=true,
//     BuySimSuccess + SellSimSuccess populated.
//
// The input DTO is never mutated.
func (p *HoneypotSimProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if p.cfg.SimulationContract == "" {
		p.logger.Warn("honeypot_sim_disabled",
			"reason", "no_simulation_contract_configured",
			"token", in.TokenAddress,
		)
		return in, nil
	}
	if p.rpc == nil {
		return in, errors.New("probes/honeypot_sim: nil rpc client")
	}
	if in.TokenAddress == "" {
		return in, errors.New("probes/honeypot_sim: empty token address")
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callData := p.Encoder(in.TokenAddress)
	raw, err := p.rpc.EthCall(cctx, callData, "latest")
	if err != nil {
		return in, fmt.Errorf("probes/honeypot_sim: eth_call: %w", err)
	}
	buyOk, sellOk, err := p.Decoder(raw)
	if err != nil {
		return in, fmt.Errorf("probes/honeypot_sim: decode: %w", err)
	}

	out := in
	out.HoneypotSimKnown = true
	out.BuySimSuccess = buyOk
	out.SellSimSuccess = sellOk
	return out, nil
}

// encodeSimulateBuySell is a placeholder ABI encoder. It builds a
// deterministic byte sequence (selector || zero-padded token addr) so
// the production contract — once deployed — can use a stable selector.
// This is INTENTIONALLY simplified: the real contract address encoding
// must be supplied by the caller's RPC client / ABI library when the
// simulation contract is deployed. Until then, the encoded bytes are
// only used as opaque input by the fake RPC client in tests.
func encodeSimulateBuySell(token string) []byte {
	// 4-byte synthetic selector + 32-byte right-padded token bytes.
	// We do NOT compute keccak256("simulateBuySell(address)") here to
	// keep this file dependency-free — production deployments must
	// override Encoder with the real ABI selector.
	out := make([]byte, 0, 4+32)
	out = append(out, 0x10, 0x20, 0x30, 0x40) // placeholder selector
	tokBytes := []byte(token)
	if len(tokBytes) > 32 {
		tokBytes = tokBytes[:32]
	}
	pad := make([]byte, 32-len(tokBytes))
	out = append(out, pad...)
	out = append(out, tokBytes...)
	return out
}

// decodeTwoBools decodes two ABI-encoded bool return values. Each bool
// occupies 32 bytes; non-zero LSB → true. Returns an error if the input
// is shorter than 64 bytes.
func decodeTwoBools(out []byte) (bool, bool, error) {
	if len(out) < 64 {
		return false, false, fmt.Errorf("expected >= 64 bytes, got %d", len(out))
	}
	buy := out[31] != 0
	sell := out[63] != 0
	return buy, sell, nil
}

package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crypto-sniping-bot/contracts"
)

// SPL Mint Account layout (82 bytes, programs/token/program/src/state.rs):
//
//	[ 0..  4) mint_authority_option   (u32 LE: 0 = None, 1 = Some)
//	[ 4.. 36) mint_authority pubkey   (32 bytes; valid iff option==1)
//	[36.. 44) supply                  (u64 LE)
//	[44.. 45) decimals                (u8)
//	[45.. 46) is_initialized          (u8: 0 or 1)
//	[46.. 50) freeze_authority_option (u32 LE)
//	[50.. 82) freeze_authority pubkey (32 bytes)
//
// "Renounced" means the COption is None (option==0).
const (
	splMintAccountSize = 82

	offsetMintAuthorityOption   = 0
	offsetSupply                = 36
	offsetDecimals              = 44
	offsetIsInitialized         = 45
	offsetFreezeAuthorityOption = 46
)

// SPLTokenProgram and Token-2022 program IDs. Both layouts share the
// first 82 bytes used here, so we accept either as the account owner.
var splTokenProgramOwners = map[string]struct{}{
	"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA": {}, // SPL Token
	"TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb": {}, // Token-2022
}

// SPLMintState is the decoded SPL mint authority info this probe needs.
type SPLMintState struct {
	MintAuthorityRenounced   bool
	FreezeAuthorityRenounced bool
	Supply                   uint64
	Decimals                 uint8
	IsInitialized            bool
}

// DecodeSPLMint decodes the canonical SPL Mint account layout. Returns
// an error when the buffer is shorter than splMintAccountSize.
func DecodeSPLMint(b []byte) (*SPLMintState, error) {
	if len(b) < splMintAccountSize {
		return nil, fmt.Errorf("probes/solana_authorities: mint account too short: %d bytes", len(b))
	}
	mintOpt := binary.LittleEndian.Uint32(b[offsetMintAuthorityOption:])
	freezeOpt := binary.LittleEndian.Uint32(b[offsetFreezeAuthorityOption:])
	return &SPLMintState{
		MintAuthorityRenounced:   mintOpt == 0,
		FreezeAuthorityRenounced: freezeOpt == 0,
		Supply:                   binary.LittleEndian.Uint64(b[offsetSupply:]),
		Decimals:                 b[offsetDecimals],
		IsInitialized:            b[offsetIsInitialized] == 1,
	}, nil
}

// SolanaAuthoritiesConfig configures the solana_authorities probe.
type SolanaAuthoritiesConfig struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
}

// authorityResult is the cached authority data for a single SPL token.
type authorityResult struct {
	mintRenounced   bool
	freezeRenounced bool
}

// SolanaAuthoritiesProbe populates MintAuthorityRenounced,
// FreezeAuthorityRenounced and SolanaAuthoritiesKnown from the SPL mint
// account on chain. Skips EVM tokens (Chain != "solana") and tokens with
// no MintAddress.
//
// Results are cached in memory indefinitely: SPL mint/freeze authority is
// immutable once renounced, so a single RPC call per token address is enough.
// The cache avoids redundant getAccountInfo calls for rescan events and any
// other repeated probes of the same token within the process lifetime.
type SolanaAuthoritiesProbe struct {
	rpc    SolanaProbeRPCClient
	cfg    SolanaAuthoritiesConfig
	logger *slog.Logger
	// cache maps tokenAddress (string) → authorityResult.
	// Written once on first successful probe; never evicted (immutable data).
	cache sync.Map
}

func NewSolanaAuthoritiesProbe(rpc SolanaProbeRPCClient, cfg SolanaAuthoritiesConfig, logger *slog.Logger) *SolanaAuthoritiesProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	return &SolanaAuthoritiesProbe{rpc: rpc, cfg: cfg, logger: logger}
}

func (p *SolanaAuthoritiesProbe) Name() string { return "solana_authorities" }

// Probe fetches the SPL mint account and populates the authority fields
// on a copy of the input DTO. Non-Solana inputs pass through unchanged.
// On RPC failure or decode error the input is returned with the error;
// the *Known flag stays false so DQ degrades gracefully.
//
// Cache hit: if this token was probed before in the current process lifetime,
// returns the cached result immediately without any Helius RPC call.
func (p *SolanaAuthoritiesProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}
	mint := strings.TrimSpace(in.TokenAddress)
	if mint == "" {
		return in, errors.New("probes/solana_authorities: empty token address")
	}

	// Fast path: return cached result without RPC call.
	if cached, ok := p.cache.Load(mint); ok {
		r := cached.(authorityResult)
		out := in
		out.MintAuthorityRenounced = r.mintRenounced
		out.FreezeAuthorityRenounced = r.freezeRenounced
		out.SolanaAuthoritiesKnown = true
		return out, nil
	}

	if p.rpc == nil {
		return in, errors.New("probes/solana_authorities: nil rpc client")
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	acc, err := p.rpc.GetAccountInfo(cctx, mint, p.cfg.Commitment)
	if err != nil {
		return in, fmt.Errorf("probes/solana_authorities: get_account_info: %w", err)
	}
	if acc == nil {
		return in, fmt.Errorf("probes/solana_authorities: mint account not found: %s", mint)
	}
	if acc.Owner != "" {
		if _, ok := splTokenProgramOwners[acc.Owner]; !ok {
			return in, fmt.Errorf("probes/solana_authorities: unexpected owner %q for mint %s", acc.Owner, mint)
		}
	}

	raw, err := base64.StdEncoding.DecodeString(acc.DataB64)
	if err != nil {
		return in, fmt.Errorf("probes/solana_authorities: base64 decode: %w", err)
	}
	state, err := DecodeSPLMint(raw)
	if err != nil {
		return in, err
	}

	out := in
	out.MintAuthorityRenounced = state.MintAuthorityRenounced
	out.FreezeAuthorityRenounced = state.FreezeAuthorityRenounced
	out.SolanaAuthoritiesKnown = true
	// Store in cache so future probes of the same token skip the RPC call.
	// Mint/freeze authority is immutable — safe to cache indefinitely.
	p.cache.Store(mint, authorityResult{
		mintRenounced:   state.MintAuthorityRenounced,
		freezeRenounced: state.FreezeAuthorityRenounced,
	})
	return out, nil
}

package execution

// router.go — thin execution router for multi-chain dispatch.
// Routes AllocationDTO to the correct execution module (EVM or Solana)
// based on alloc.Chain. The Router is owned by the execution worker.
//
// Architecture invariants:
//   - This file MUST NOT import database/.
//   - The Router is a pure dispatcher — it calls the appropriate executor.
//   - EVM execution module is untouched — the Router wraps it via EVMExecutor interface.

import (
	"context"
	"fmt"

	"crypto-sniping-bot/contracts"
)

// EVMExecutor is the interface the EVM execution module must satisfy.
// Implemented by *Module from execution.go.
type EVMExecutor interface {
	Process(ctx context.Context, in contracts.AllocationDTO, nonce uint64, routerAddress string) (contracts.ExecutionResultDTO, error)
}

// SolanaExecutor is the interface the Solana execution module must satisfy.
// Implemented by *execution_solana.Module.
// market and poolAddress may be empty strings; the module uses its configured defaults.
type SolanaExecutor interface {
	Execute(ctx context.Context, alloc contracts.AllocationDTO, market, poolAddress string) (contracts.ExecutionResultDTO, error)
}

// Router dispatches execution to the correct chain-specific executor.
type Router struct {
	evm    EVMExecutor
	solana SolanaExecutor
}

// NewRouter creates a Router. solana may be nil when Solana support is disabled.
func NewRouter(evm EVMExecutor, solana SolanaExecutor) *Router {
	return &Router{evm: evm, solana: solana}
}

// Route dispatches the allocation to the appropriate executor.
// For EVM chains ("eth", "bsc", "polygon", etc.): calls evm.Process.
// For "solana": calls solana.Execute.
func (r *Router) Route(ctx context.Context, alloc contracts.AllocationDTO, nonce uint64, routerAddress string) (contracts.ExecutionResultDTO, error) {
	switch alloc.Chain {
	case "solana":
		if r.solana == nil {
			return contracts.ExecutionResultDTO{}, fmt.Errorf("router: solana executor not configured")
		}
		return r.solana.Execute(ctx, alloc, "", "")
	default:
		if r.evm == nil {
			return contracts.ExecutionResultDTO{}, fmt.Errorf("router: evm executor not configured")
		}
		return r.evm.Process(ctx, alloc, nonce, routerAddress)
	}
}

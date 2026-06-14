// Package execution — fee-bump helpers for transaction replacement.
// BumpGasPrice computes the replacement gas price for a stuck transaction.
// The retry/polling loop is handled in the execution worker layer.
package execution

import (
	"math/big"
)

// BumpGasPrice computes the replacement gas price from original and bump multiplier.
// newGas = originalGas × (100 + bumpPct) / 100
// bumpPct = 15 means a 15% increase (per Phase 3 spec: δ ≈ 10–20%).
func BumpGasPrice(originalWei *big.Int, bumpPct int) *big.Int {
	if originalWei == nil || originalWei.Sign() == 0 {
		return big.NewInt(0)
	}
	multiplier := big.NewInt(int64(100 + bumpPct))
	result := new(big.Int).Mul(originalWei, multiplier)
	result.Div(result, big.NewInt(100))
	return result
}

// TxStatus represents on-chain transaction status after polling.
type TxStatus int

const (
	TxStatusPending   TxStatus = iota // not yet mined
	TxStatusConfirmed                 // mined with status=1
	TxStatusReverted                  // mined with status=0
	TxStatusStuck                     // pending past StuckAfter deadline
	TxStatusDropped                   // no receipt after max retries
)

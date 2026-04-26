package data_quality

import "math/big"

// DetectRugRisk returns true when LP-locked status + reserves indicate the
// pool is likely a rug.
//
// Heuristic (Phase 4):
//   - LP NOT locked, AND
//   - Reserves below 10× the configured minimum (i.e., very thin pool)
func DetectRugRisk(lpLocked bool, reserveBaseRaw, minReserveBaseWei string) bool {
if lpLocked {
return false
}
reserve, ok := new(big.Int).SetString(reserveBaseRaw, 10)
if !ok {
return true
}
minReserve, ok := new(big.Int).SetString(minReserveBaseWei, 10)
if !ok || minReserve.Sign() == 0 {
return false
}
threshold := new(big.Int).Mul(minReserve, big.NewInt(10))
return reserve.Cmp(threshold) < 0
}

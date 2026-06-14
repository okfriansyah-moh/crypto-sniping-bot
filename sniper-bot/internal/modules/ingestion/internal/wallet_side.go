// Package ingestioninternal provides helpers used exclusively by the ingestion module.
// It is an internal package — only code under internal/modules/ingestion/ may import it.
package ingestioninternal

import (
	"fmt"
	"sort"
	"strings"
)

// WalletSide determines which token in a pair is the sniping target ("token")
// and which is the base ("base"). baseTokens are normalised to lowercase internally.
//
// Returns (tokenAddr, baseAddr) or an error if neither side is a recognised base.
// Iteration over baseTokens is sorted for deterministic results.
func WalletSide(token0, token1 string, baseTokens []string) (tokenAddr, baseAddr string, err error) {
	t0 := strings.ToLower(token0)
	t1 := strings.ToLower(token1)

	// Sort baseTokens for deterministic iteration.
	sorted := make([]string, len(baseTokens))
	copy(sorted, baseTokens)
	sort.Strings(sorted)

	for _, base := range sorted {
		b := strings.ToLower(base)
		if t0 == b {
			return token1, token0, nil
		}
		if t1 == b {
			return token0, token1, nil
		}
	}
	return "", "", fmt.Errorf("wallet_side: neither %s nor %s is a known base token", token0, token1)
}

package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

// Pump.fun AMM pool state — vault pubkeys after 8-byte discriminator +
// bump(1) + index(2) + creator(32) + base_mint(32) + quote_mint(32) + lp_mint(32).
const (
	pumpfunAMMBaseVaultOffset  = 139
	pumpfunAMMQuoteVaultOffset = 171
	splTokenAmountOffset       = 64
)

var errInvalidSPLAccount = errors.New("probes: invalid spl token account")

// liquidityUsdFromSolLamports converts on-chain SOL lamports to USD liquidity.
func liquidityUsdFromSolLamports(lamports uint64, solPriceUsd float64) float64 {
	if lamports == 0 || solPriceUsd <= 0 {
		return 0
	}
	return (float64(lamports) / lamportsPerSol) * solPriceUsd
}

// solPriceOrFallback returns the live SOL/USD quote when available, otherwise
// the configured static estimate. ok is false only when both sources are absent.
func solPriceOrFallback(ctx context.Context, solUsd SolUsdSource, fallbackUsd float64) (float64, bool) {
	if solUsd != nil {
		if px, ok := solUsd.SolUsd(ctx); ok && px > 0 {
			return px, true
		}
	}
	if fallbackUsd > 0 {
		return fallbackUsd, true
	}
	return 0, false
}

func decodeSPLTokenAmount(dataB64 string) (uint64, error) {
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return 0, err
	}
	if len(raw) < splTokenAmountOffset+8 {
		return 0, errInvalidSPLAccount
	}
	return binary.LittleEndian.Uint64(raw[splTokenAmountOffset : splTokenAmountOffset+8]), nil
}

func pubkeyFromBytes(b []byte) string {
	if len(b) != 32 {
		return ""
	}
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ""
	}
	return base58Encode(b)
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}
	num := make([]byte, len(input))
	copy(num, input)
	var encoded []byte
	for len(num) > 0 {
		carry := 0
		for i := len(num) - 1; i >= 0; i-- {
			val := int(num[i]) + carry*256
			num[i] = byte(val / 58)
			carry = val % 58
		}
		encoded = append(encoded, base58Alphabet[carry])
		for len(num) > 0 && num[0] == 0 {
			num = num[1:]
		}
	}
	for i := 0; i < zeros; i++ {
		encoded = append(encoded, '1')
	}
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	return string(encoded)
}
